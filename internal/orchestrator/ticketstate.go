package orchestrator

import (
	"context"
	"fmt"
	"log"

	"github.com/JR-G/squad0/internal/agent"
)

const moveTicketPromptTemplate = `Move Linear ticket %s to "%s" status.

Use the Linear MCP tools (these are the exact tool names — do not guess
shorter variants, they will not exist):

1. mcp__claude_ai_Linear__get_issue — arguments: {"id": "%s"}
2. mcp__claude_ai_Linear__list_issue_statuses — arguments: {"teamId": "<teamId from step 1>"}
3. Find the state id whose name matches "%s"
4. mcp__claude_ai_Linear__save_issue — arguments: {"id": "%s", "stateId": "<id from step 3>"}

Respond with just "done" when complete, or "failed: <short reason>" if you could not update it.
`

// The fmt.Sprintf call in MoveTicketState fills these placeholders in
// order:
//  1. ticket          — "Move Linear ticket %s ..."
//  2. targetState     — "to \"%s\" status"
//  3. ticket          — get_issue id
//  4. targetState     — "whose name matches \"%s\""
//  5. ticket          — save_issue id

// MoveTicketState uses the PM agent to transition a Linear ticket to
// the given state. This is a safety net — agents also move tickets
// themselves via MCP tools during sessions.
func MoveTicketState(ctx context.Context, pmAgent *agent.Agent, ticket, targetState string) {
	if pmAgent == nil {
		return
	}

	prompt := fmt.Sprintf(moveTicketPromptTemplate, ticket, targetState, ticket, targetState, ticket)

	result, err := pmAgent.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("failed to move %s to %q: %v", ticket, targetState, err)
		return
	}

	log.Printf("ticket %s → %s: %s", ticket, targetState, agent.TruncateSummary(result.Transcript, 100))
}
