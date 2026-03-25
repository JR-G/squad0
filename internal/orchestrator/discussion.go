package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

const defaultDiscussionWait = 20 * time.Second

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

Before you start coding, post your planned approach to #engineering. Be specific:
- What files will you create or modify?
- What's your high-level approach?
- Any concerns or questions for the team?

Keep it to 3-5 sentences. Be concrete, not vague. Respond with ONLY your plan — no code, no markdown headers.
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

	// Wait for teammates to respond.
	wait := orch.cfg.DiscussionWait
	if wait == 0 {
		wait = defaultDiscussionWait
	}
	log.Printf("discussion: waiting %s for team feedback on %s", wait, assignment.Ticket)
	select {
	case <-time.After(wait):
	case <-ctx.Done():
		return ""
	}

	// Collect the discussion for the implementation prompt.
	discussion := orch.collectDiscussion(ctx, "engineering")

	return discussion
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
