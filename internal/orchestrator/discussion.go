package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

const (
	defaultDiscussionWait    = 20 * time.Second
	defaultQuietThreshold    = 5 * time.Second
	defaultQuietPollInterval = 2 * time.Second
	maxDiscussionWait        = 3 * time.Minute
)

// RunDiscussionForTest exports runDiscussionPhase for testing.
func (orch *Orchestrator) RunDiscussionForTest(ctx context.Context, agentInstance *agent.Agent, assignment Assignment) string {
	return orch.runDiscussionPhase(ctx, agentInstance, assignment)
}

// FilterPassResponseForTest exports filterPassResponse for testing.
func FilterPassResponseForTest(text string) string {
	return filterPassResponse(text)
}

func filterPassResponse(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || containsPass(trimmed) {
		return ""
	}
	return trimmed
}

const planPromptTemplate = `You're about to work on ticket %s: %s

Post your planned approach to #engineering. Be specific:
- What files will you create or modify?
- What's your high-level approach?
- Any concerns or questions for the team?

Keep it to 3-5 sentences. Be concrete, not vague.
Use Slack formatting: *bold* not **bold**. No markdown headers (no # or ##). No numbered lists with bold items.
Respond with ONLY your plan.
`

// runDiscussionPhase posts the engineer's plan and waits for team
// feedback before implementation begins. Returns the discussion
// messages so they can be included in the implementation prompt.
func (orch *Orchestrator) runDiscussionPhase(ctx context.Context, agentInstance *agent.Agent, assignment Assignment) string {
	role := agentInstance.Role()
	planPrompt := fmt.Sprintf(planPromptTemplate, assignment.Ticket, assignment.Description)

	plan, err := agentInstance.QuickChat(ctx, planPrompt)
	if err != nil {
		log.Printf("discussion: %s failed to generate plan: %v", role, err)
		return ""
	}

	plan = filterPassResponse(plan)
	if plan == "" {
		return ""
	}

	// Post the plan — this triggers the conversation engine so
	// teammates can respond in the thread.
	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("Here's my plan for %s:\n\n%s",
			orch.cfg.Links.TicketLink(assignment.Ticket), plan),
		role)

	// Tech Lead weighs in on every discussion.
	orch.TechLeadDiscussionReview(ctx, "engineering", plan, assignment.Ticket)

	// Wait until the thread goes quiet instead of a fixed timer.
	orch.waitForQuiet(ctx, "engineering")

	// PM makes the call if the discussion didn't converge.
	orch.BreakDiscussionTie(ctx, "engineering")

	// Collect the discussion for the implementation prompt.
	return orch.collectDiscussion(ctx, "engineering")
}

// waitForQuiet polls the conversation engine until the channel has been
// quiet for quietThreshold, or maxDiscussionWait is reached. Falls back
// to the configured DiscussionWait if no conversation engine is available.
func (orch *Orchestrator) waitForQuiet(ctx context.Context, channel string) {
	if orch.conversation == nil {
		orch.waitFixed(ctx)
		return
	}

	maxWait := maxDiscussionWait
	if orch.cfg.DiscussionWait > 0 && orch.cfg.DiscussionWait < maxWait {
		maxWait = orch.cfg.DiscussionWait
	}

	threshold := orch.cfg.QuietThreshold
	if threshold == 0 {
		threshold = defaultQuietThreshold
	}

	pollInterval := orch.cfg.QuietPollInterval
	if pollInterval == 0 {
		pollInterval = defaultQuietPollInterval
	}

	deadline := time.After(maxWait)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	log.Printf("discussion: waiting for quiet (threshold=%s, max=%s)", threshold, maxWait)

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			log.Printf("discussion: max wait reached, proceeding")
			return
		case <-ticker.C:
			if orch.conversation.IsQuiet(channel, threshold) {
				log.Printf("discussion: channel quiet, proceeding")
				return
			}
		}
	}
}

func (orch *Orchestrator) waitFixed(ctx context.Context) {
	wait := orch.cfg.DiscussionWait
	if wait == 0 {
		wait = defaultDiscussionWait
	}
	log.Printf("discussion: no conversation engine, waiting %s", wait)
	select {
	case <-time.After(wait):
	case <-ctx.Done():
	}
}

func (orch *Orchestrator) collectDiscussion(_ context.Context, channel string) string {
	if orch.conversation == nil {
		return ""
	}

	lines := orch.conversation.RecentMessages(channel)
	if len(lines) == 0 {
		return ""
	}

	result := "## Team Discussion\n\n"
	for _, line := range lines {
		result += "> " + line + "\n"
	}
	result += "\nIncorporate relevant feedback from this discussion into your implementation.\n\n"

	return result
}
