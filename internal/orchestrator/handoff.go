package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
)

// SetHandoffStore connects the handoff store for session continuity.
func (orch *Orchestrator) SetHandoffStore(store *pipeline.HandoffStore) {
	orch.handoffStore = store
}

// WriteHandoffForTest exports writeHandoff for testing.
func (orch *Orchestrator) WriteHandoffForTest(ctx context.Context, ticket string, role agent.Role, status, summary, branch string) {
	orch.writeHandoff(ctx, ticket, role, status, summary, branch)
}

// writeHandoff persists a session handoff so the next session for
// this ticket can pick up where the predecessor left off.
func (orch *Orchestrator) writeHandoff(ctx context.Context, ticket string, role agent.Role, status, summary, branch string) {
	if orch.handoffStore == nil {
		return
	}

	gitState := "clean"
	if status == "failed" {
		gitState = "dirty"
	}

	handoff := pipeline.Handoff{
		Ticket:    ticket,
		Agent:     string(role),
		Status:    status,
		Summary:   agent.TruncateSummary(summary, 500),
		GitBranch: branch,
		GitState:  gitState,
	}

	_, err := orch.handoffStore.Create(ctx, handoff)
	if err != nil {
		log.Printf("failed to write handoff for %s: %v", ticket, err)
	}
}

// BuildHandoffContext loads the latest handoff for a ticket and
// formats it as a prompt section. Returns empty string if no handoff
// exists.
func BuildHandoffContext(ctx context.Context, store *pipeline.HandoffStore, ticket string) string {
	if store == nil {
		return ""
	}

	handoff, err := store.LatestForTicket(ctx, ticket)
	if isNoRowsError(err) {
		return ""
	}
	if err != nil {
		log.Printf("failed to read handoff for %s: %v", ticket, err)
		return ""
	}

	var builder strings.Builder
	builder.WriteString("## Previous Session Handoff\n")
	fmt.Fprintf(&builder, "Agent: %s | Status: %s\n", handoff.Agent, handoff.Status)
	fmt.Fprintf(&builder, "%s\n", handoff.Summary)

	if handoff.Remaining != "" {
		fmt.Fprintf(&builder, "Remaining: %s\n", handoff.Remaining)
	}

	if handoff.Blockers != "" {
		fmt.Fprintf(&builder, "Blockers: %s\n", handoff.Blockers)
	}

	if handoff.GitBranch != "" {
		fmt.Fprintf(&builder, "Branch: %s (state: %s)\n", handoff.GitBranch, handoff.GitState)
	}

	builder.WriteString("\n")

	return builder.String()
}

// isNoRowsError returns true if the error is or wraps sql.ErrNoRows.
func isNoRowsError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	return strings.Contains(err.Error(), "no rows")
}
