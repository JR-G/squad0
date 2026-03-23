package orchestrator

import (
	"context"
	"fmt"
	"log"

	"github.com/JR-G/squad0/internal/agent"
)

const moveTicketPromptTemplate = `Move Linear ticket %s to "%s" status.

Use the Linear MCP tools to update this ticket's state. Find the correct workflow state for "%s" and update the issue.

Steps:
1. Get the issue details: use the get_issue tool with identifier "%s"
2. List the available workflow states: use the list_issue_statuses tool
3. Find the state ID that matches "%s"
4. Update the issue state: use the save_issue tool

Respond with just "done" when complete, or "failed" if you couldn't update it.
`

// MoveTicketState uses the PM agent to transition a Linear ticket to
// the given state. This is a safety net — agents also move tickets
// themselves via MCP tools during sessions.
func MoveTicketState(ctx context.Context, pmAgent *agent.Agent, ticket, targetState string) {
	if pmAgent == nil {
		return
	}

	prompt := fmt.Sprintf(moveTicketPromptTemplate, ticket, targetState, targetState, ticket, targetState)

	result, err := pmAgent.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("failed to move %s to %q: %v", ticket, targetState, err)
		return
	}

	log.Printf("ticket %s → %s: %s", ticket, targetState, agent.TruncateSummary(result.Transcript, 100))
}
