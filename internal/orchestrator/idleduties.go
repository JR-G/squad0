package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
)

// RunIdleDuties engages idle agents with real work: engineers check
// their own PRs first, then all idle agents review others' PRs.
// Also investigates any unresolved concerns agents have noted.
func (orch *Orchestrator) RunIdleDuties(ctx context.Context, idleRoles []agent.Role) {
	orch.InvestigateConcerns(ctx, idleRoles)

	if orch.pipelineStore == nil {
		return
	}

	openPRs, err := orch.pipelineStore.OpenWithPR(ctx)
	if err != nil || len(openPRs) == 0 {
		return
	}

	// Engineers check their own PRs first — ownership over their work.
	for _, role := range idleRoles {
		orch.checkOwnPR(ctx, role, openPRs)
	}

	for _, role := range idleRoles {
		orch.tryIdleDuty(ctx, role, openPRs)
	}
}

// checkOwnPR looks for the engineer's own open PR and follows up:
// reads comments, checks status, merges if approved.
func (orch *Orchestrator) checkOwnPR(ctx context.Context, role agent.Role, openPRs []pipeline.WorkItem) {
	if !isEngineerRole(role) {
		return
	}

	item := orch.findOwnPR(role, openPRs)
	if item == nil {
		return
	}

	if orch.hasCommented(role, item.ID) {
		return
	}
	orch.markCommented(role, item.ID)

	agentInstance, ok := orch.agents[role]
	if !ok {
		return
	}

	name := orch.NameForRole(role)
	prompt := fmt.Sprintf(
		"You are %s. You have an open PR for %s: %s\n\n"+
			"Check its status:\n"+
			"1. Run: gh pr view %s --json state,reviewDecision,statusCheckRollup\n"+
			"2. If APPROVED → merge it: gh pr merge %s --squash\n"+
			"3. If CHANGES_REQUESTED → read comments: gh pr view %s --comments, then address them\n"+
			"4. If no review yet → just respond PASS\n\n"+
			"Respond with a 1-sentence Slack update about what you did, or PASS if nothing to do.",
		name, item.Ticket, item.PRURL,
		item.PRURL, item.PRURL, item.PRURL)

	result, err := agentInstance.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("own PR check: %s session failed: %v", role, err)
		return
	}

	response := filterPassResponse(result.Transcript)
	if response == "" {
		return
	}

	log.Printf("own PR check: %s followed up on %s", role, item.Ticket)
	clean := cleanIdleResponse(response)
	if clean != "" {
		orch.postAsRole(ctx, "engineering", clean, role)
	}
}

func (orch *Orchestrator) findOwnPR(role agent.Role, openPRs []pipeline.WorkItem) *pipeline.WorkItem {
	for idx := range openPRs {
		if openPRs[idx].Engineer == role {
			return &openPRs[idx]
		}
	}
	return nil
}

func (orch *Orchestrator) tryIdleDuty(ctx context.Context, role agent.Role, openPRs []pipeline.WorkItem) {
	item := orch.pickUncommentedPR(role, openPRs)
	if item == nil {
		return
	}

	orch.markCommented(role, item.ID)

	agentInstance, ok := orch.agents[role]
	if !ok {
		return
	}

	prompt := buildIdleReviewPrompt(role, item, orch.NameForRole(item.Engineer))
	if prompt == "" {
		return
	}

	// DirectSession — the agent can run gh commands to read the actual diff.
	result, err := agentInstance.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("idle duty: %s session failed: %v", role, err)
		return
	}

	response := filterPassResponse(result.Transcript)
	if response == "" {
		return
	}

	log.Printf("idle duty: %s reviewed %s's PR for %s", role, item.Engineer, item.Ticket)

	// Strip narration and post to #reviews — the detailed feedback
	// is on the PR itself via gh pr comment.
	clean := cleanIdleResponse(response)
	if clean == "" {
		return
	}
	orch.announceAsRole(ctx, "reviews", clean, role)
}

func buildIdleReviewPrompt(role agent.Role, item *pipeline.WorkItem, engineerName string) string {
	base := fmt.Sprintf(
		"%s has an open PR for %s.\n\n"+
			"1. Read the diff: gh pr diff %s\n"+
			"2. Read the description: gh pr view %s\n\n",
		engineerName, item.Ticket, item.PRURL, item.PRURL)

	action := "gh pr comment " + item.PRURL + " --body 'your observation'"
	rules := "After posting the comment, respond with ONLY what you'd say in Slack about it. " +
		"1-2 sentences. Use " + engineerName + "'s name. " +
		"Use Slack formatting (*bold* not **bold**). No headers. No separators. " +
		"Do NOT say 'Comment posted' or describe what you did — just write the Slack message."

	switch {
	case isEngineerRole(role):
		return base +
			"Post ONE specific code observation as a PR comment:\n" + action + "\n\n" +
			"Reference a file, function, or pattern you noticed. " + rules

	case role == agent.RoleDesigner:
		return base +
			"If there are frontend/UI changes, post a UX observation:\n" + action + "\n" +
			"If purely backend, respond with PASS.\n" + rules

	case role == agent.RoleTechLead:
		return base +
			"Post an architectural observation about module boundaries, " +
			"dependency direction, or patterns:\n" + action + "\n\n" + rules

	default:
		return ""
	}
}

func (orch *Orchestrator) pickUncommentedPR(role agent.Role, openPRs []pipeline.WorkItem) *pipeline.WorkItem {
	for idx := range openPRs {
		item := &openPRs[idx]
		if item.Engineer == role {
			continue
		}
		if orch.hasCommented(role, item.ID) {
			continue
		}
		return item
	}
	return nil
}

func (orch *Orchestrator) hasCommented(role agent.Role, itemID int64) bool {
	return orch.followedUp[idleCommentKey(role, itemID)]
}

func (orch *Orchestrator) markCommented(role agent.Role, itemID int64) {
	orch.followedUp[idleCommentKey(role, itemID)] = true
}

func idleCommentKey(role agent.Role, itemID int64) int64 {
	roleHash := int64(0)
	for _, ch := range string(role) {
		roleHash = roleHash*31 + int64(ch)
	}
	return roleHash*1_000_000 + itemID
}

// cleanIdleResponse strips narration, meta-commentary, markdown headers,
// and separators from an idle review response. Returns just the Slack message.
func cleanIdleResponse(text string) string {
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines, separators, and narration.
		if trimmed == "" || trimmed == "---" || trimmed == "***" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "comment posted") ||
			strings.HasPrefix(lower, "here's the slack") ||
			strings.HasPrefix(lower, "slack summary") ||
			strings.HasPrefix(lower, "**slack summary") ||
			strings.HasPrefix(lower, "#") {
			continue
		}

		// Convert **bold** to *bold* for Slack.
		trimmed = strings.ReplaceAll(trimmed, "**", "*")
		cleaned = append(cleaned, trimmed)
	}

	result := strings.Join(cleaned, " ")
	if len(result) > 500 {
		result = result[:500]
	}
	return result
}

func isEngineerRole(role agent.Role) bool {
	return strings.HasPrefix(string(role), "engineer-")
}

// FilterIdleDutyRolesForTest exports filterIdleDutyRoles for testing.
func FilterIdleDutyRolesForTest(roles []agent.Role) []agent.Role {
	return filterIdleDutyRoles(roles)
}

func filterIdleDutyRoles(roles []agent.Role) []agent.Role {
	eligible := make([]agent.Role, 0, len(roles))
	for _, role := range roles {
		switch role { //nolint:exhaustive // only excluding specific roles
		case agent.RolePM, agent.RoleReviewer:
			continue
		default:
			eligible = append(eligible, role)
		}
	}
	return eligible
}
