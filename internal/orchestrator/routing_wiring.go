package orchestrator

import (
	"context"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
	"github.com/JR-G/squad0/internal/routing"
)

// AdvancePipelineForTest exports advancePipeline for testing.
func (orch *Orchestrator) AdvancePipelineForTest(ctx context.Context, itemID int64, stage pipeline.Stage) {
	orch.advancePipeline(ctx, itemID, stage)
}

// IsRoleIdleForTest exports isRoleIdle for testing.
func (orch *Orchestrator) IsRoleIdleForTest(ctx context.Context, role agent.Role) bool {
	return orch.isRoleIdle(ctx, role)
}

// FilterHealthyEngineersForTest exports filterHealthyEngineers for testing.
func (orch *Orchestrator) FilterHealthyEngineersForTest(roles []agent.Role) []agent.Role {
	return orch.filterHealthyEngineers(roles)
}

// ShouldEscalateForTest exports shouldEscalate for testing.
func (orch *Orchestrator) ShouldEscalateForTest(ctx context.Context, workItemID int64, ticket string) bool {
	return orch.shouldEscalate(ctx, workItemID, ticket)
}

// CancelSessionForTest exports cancelSession for testing.
func (orch *Orchestrator) CancelSessionForTest(role agent.Role) {
	orch.cancelSession(role)
}

// NameForRole returns the agent's chosen name, falling back to the
// role ID if no name is known. Thin forward to ConversationCoordinator.
func (orch *Orchestrator) NameForRole(role agent.Role) string {
	return orch.chat.NameForRole(role)
}

// SetSpecialisationStore connects the specialisation tracker for
// intelligent assignment based on agent success rates.
func (orch *Orchestrator) SetSpecialisationStore(store *routing.SpecialisationStore) {
	orch.specStore = store
}

// SetOpinionStore connects the inter-agent opinion tracker for
// review scrutiny adjustment.
func (orch *Orchestrator) SetOpinionStore(store *routing.OpinionStore) {
	orch.opinionStore = store
}

// SetTokenLedger connects the token budget tracker for cost control.
func (orch *Orchestrator) SetTokenLedger(ledger *routing.TokenLedger) {
	orch.tokenLedger = ledger
}

// SetComplexityClassifier connects the task complexity classifier
// for semantic model routing.
func (orch *Orchestrator) SetComplexityClassifier(classifier *routing.ComplexityClassifier) {
	orch.complexityClassifier = classifier
}
