package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
)

// TechLeadDiscussionReview posts the Tech Lead's architecture guidance
// in response to an engineer's plan. The Tech Lead weighs in on every
// discussion — not randomly, but deliberately.
func (orch *Orchestrator) TechLeadDiscussionReview(ctx context.Context, channel, plan, ticket string) {
	techLead, ok := orch.agents[agent.RoleTechLead]
	if !ok {
		return
	}

	lines := make([]string, 0)
	if orch.conversation != nil {
		lines = orch.conversation.RecentMessages(channel)
	}

	prompt := fmt.Sprintf("An engineer just posted their plan for ticket %s. "+
		"Review it from an architectural perspective. Consider:\n"+
		"- Does this approach fit the system's design?\n"+
		"- Are there better patterns or approaches?\n"+
		"- Any risks or concerns?\n\n", ticket)

	if len(lines) > 0 {
		prompt += "Discussion so far:\n"
		for _, line := range lines {
			prompt += "> " + line + "\n"
		}
	}

	prompt += "\nRespond with your architectural take in 2-4 sentences. " +
		"Be specific about what you agree with and what concerns you. " +
		"If you'd suggest changes, say what and why. " +
		"End with a clear DECISION: statement summarising what the engineer should do."

	response, err := techLead.QuickChat(ctx, prompt)
	if err != nil {
		log.Printf("tech lead discussion review failed: %v", err)
		return
	}

	response = filterPassResponse(response)
	if response == "" {
		return
	}

	orch.postAsRole(ctx, channel, response, agent.RoleTechLead)

	// Extract and store the DECISION line so it's prominent in context.
	decision := ExtractDecisionLine(response)
	if decision != "" {
		orch.StoreArchitectureDecision(ctx, decision, ticket)
	}
}

// ExtractDecisionLine finds a line starting with "DECISION:" (case-insensitive)
// in the given text and returns the content after the prefix.
func ExtractDecisionLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if !strings.HasPrefix(lower, "decision:") {
			continue
		}
		return strings.TrimSpace(trimmed[len("decision:"):])
	}
	return ""
}

// StoreArchitectureDecision saves a Tech Lead's architectural decision
// as a belief in the project knowledge graph for future recall.
func (orch *Orchestrator) StoreArchitectureDecision(ctx context.Context, decision, ticket string) {
	techLead, ok := orch.agents[agent.RoleTechLead]
	if !ok {
		return
	}

	factStore := techLead.FactStore()
	if factStore == nil {
		return
	}

	content := fmt.Sprintf("[%s] %s", ticket, decision)

	_, err := factStore.CreateBelief(ctx, memory.Belief{
		Content:       content,
		Confidence:    0.7,
		Confirmations: 1,
	})
	if err != nil {
		log.Printf("failed to store architecture decision: %v", err)
	}
}

// RunConversationalArchReview runs the architecture review as a
// back-and-forth conversation between Tech Lead and engineer, rather
// than a single-shot DirectSession.
func (orch *Orchestrator) RunConversationalArchReview(ctx context.Context, prURL, ticket string, engineerRole agent.Role) ReviewOutcome {
	techLead, ok := orch.agents[agent.RoleTechLead]
	if !ok {
		return ReviewApproved
	}

	prNum := ExtractPRNumber(prURL)

	// First pass: Tech Lead reviews the diff.
	reviewPrompt := fmt.Sprintf(
		"Review the architecture of PR #%s for ticket %s.\n\n"+
			"1. Read the diff: gh pr diff %s\n"+
			"2. Focus on design, boundaries, and dependencies\n"+
			"3. Post your findings as a PR comment: "+
			"gh pr comment %s --body \"your feedback\"\n"+
			"4. If there are architectural concerns, run: "+
			"gh pr review %s --request-changes --body \"architectural feedback\"\n"+
			"5. If the architecture is sound, run: "+
			"gh pr review %s --approve --body \"Architecture looks good\"\n\n"+
			"End with APPROVED or CHANGES_REQUESTED.",
		prNum, ticket, prURL, prURL, prURL, prURL)

	result, err := techLead.DirectSession(ctx, reviewPrompt)
	if err != nil {
		log.Printf("arch review failed for %s: %v", ticket, err)
		return ReviewApproved
	}

	outcome := ClassifyReviewOutcome(result.Transcript)
	prLink := orch.cfg.Links.PRLink(prURL)

	orch.announceAsRole(ctx, "reviews",
		fmt.Sprintf("Architecture review for %s — %s. Details on the PR. %s", ticket, outcome, prLink),
		agent.RoleTechLead)

	// Store any architectural decisions from the review.
	orch.extractAndStoreDecisions(ctx, result.Transcript, ticket)

	return outcome
}

func (orch *Orchestrator) extractAndStoreDecisions(ctx context.Context, transcript, ticket string) {
	// Look for decision-like statements in the transcript.
	decisionSignals := []string{"should", "must", "recommend", "approach", "pattern", "design"}

	lines := strings.Split(transcript, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lower := strings.ToLower(trimmed)
		for _, signal := range decisionSignals {
			if strings.Contains(lower, signal) {
				orch.StoreArchitectureDecision(ctx, trimmed, ticket)
				return // Store at most one decision per review.
			}
		}
	}
}
