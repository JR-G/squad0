package orchestrator

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/JR-G/squad0/internal/agent"
)

const moveTicketPromptTemplate = `Move Linear ticket %s to "%s" status.

Prefer the squad0-linear MCP tools (mcp__squad0-linear__*). If those
aren't exposed, use the managed connector equivalents (mcp__claude_ai_Linear__*).
If the tool schema isn't pre-loaded, load it first via ToolSearch with
{"query": "select:<tool-name>", "max_results": 5}.

1. get_issue — arguments: {"id": "%s"}
2. list_issue_statuses — arguments: {"teamId": "<teamId from step 1>"}
3. Find the state id whose name matches "%s"
4. save_issue — arguments: {"id": "%s", "stateId": "<id from step 3>"}

Respond with just "done" when complete, or "failed: <short reason>" if you could not update it.
`

// linearStateSetter is the API-based transition path. When set, it is
// tried first and only falls back to the PM-via-MCP path on error.
// The setter is nil until start.go wires it up with the configured
// LINEAR_API_KEY and team ID, which lets MoveTicketState stay a
// package-level function with no struct plumbing.
var (
	linearStateSetterMu sync.RWMutex
	linearStateSetter   func(ctx context.Context, ticket, targetState string) error
)

// SetLinearStateSetter installs a transition function that bypasses
// MCP (typically MoveLinearTicketStateAPI bound to apiKey + teamID).
// Passing nil clears it.
func SetLinearStateSetter(fn func(ctx context.Context, ticket, targetState string) error) {
	linearStateSetterMu.Lock()
	defer linearStateSetterMu.Unlock()
	linearStateSetter = fn
}

func currentLinearStateSetter() func(ctx context.Context, ticket, targetState string) error {
	linearStateSetterMu.RLock()
	defer linearStateSetterMu.RUnlock()
	return linearStateSetter
}

// MoveTicketState transitions a Linear ticket to the given state.
// Prefers the direct GraphQL API path when a setter has been
// installed; falls back to a PM DirectSession that calls the Linear
// MCP tools. The MCP path exists for environments that haven't
// configured LINEAR_API_KEY — it is slower and less reliable because
// MCP tools are deferred and the model has to ToolSearch them first.
func MoveTicketState(ctx context.Context, pmAgent *agent.Agent, ticket, targetState string) {
	if tryMoveViaAPI(ctx, ticket, targetState) {
		return
	}
	moveViaPMSession(ctx, pmAgent, ticket, targetState)
}

// tryMoveViaAPI returns true if the API path handled the transition
// (success), false if no setter is installed or the call failed and
// the caller should fall back to the PM path.
func tryMoveViaAPI(ctx context.Context, ticket, targetState string) bool {
	setter := currentLinearStateSetter()
	if setter == nil {
		return false
	}
	if err := setter(ctx, ticket, targetState); err != nil {
		log.Printf("Linear API transition failed for %s → %s: %v (falling back to PM session)", ticket, targetState, err)
		return false
	}
	log.Printf("ticket %s → %s via Linear API", ticket, targetState)
	return true
}

func moveViaPMSession(ctx context.Context, pmAgent *agent.Agent, ticket, targetState string) {
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
