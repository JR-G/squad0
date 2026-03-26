package orchestrator

import (
	"context"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
)

// opinionSignals are phrases that suggest a strong opinion worth storing.
var opinionSignals = []string{
	"i think", "i believe", "we should", "we need to",
	"the right approach", "the wrong approach",
	"always", "never", "learned that",
	"important to", "dangerous to", "risky",
	"better to", "worse to", "prefer",
	"decision:", "recommend",
}

// projectSignals are phrases that indicate a belief applies broadly
// across the project, not just to one agent's personal experience.
var projectSignals = []string{
	"module", "architecture", "pattern", "boundary",
	"dependency", "convention", "interface", "api",
	"schema", "migration", "deploy", "pipeline",
	"auth", "middleware", "handler", "store",
	"config", "test", "lint", "format",
}

// SetProjectFactStore connects a project-level fact store for
// cross-pollination of high-confidence beliefs.
func (engine *ConversationEngine) SetProjectFactStore(store *memory.FactStore) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.projectFactStore = store
}

// maybeStoreConversationBelief checks if an agent's chat response
// contains a strong opinion and stores it as a belief. This is how
// agents build memory from conversations — not just implementation.
func (engine *ConversationEngine) maybeStoreConversationBelief(ctx context.Context, role agent.Role, text string) {
	lower := strings.ToLower(text)

	hasOpinion := false
	for _, signal := range opinionSignals {
		if strings.Contains(lower, signal) {
			hasOpinion = true
			break
		}
	}

	if !hasOpinion {
		return
	}

	factStore, ok := engine.factStores[role]
	if !ok {
		return
	}

	// Store with moderate confidence — conversation opinions are weaker
	// than implementation learnings. They'll strengthen if confirmed.
	beliefID, err := factStore.CreateBelief(ctx, memory.Belief{
		Content:       text,
		Confidence:    0.4,
		Confirmations: 1,
		SourceOutcome: "conversation",
	})
	if err != nil {
		log.Printf("failed to store conversation belief for %s: %v", role, err)
		return
	}

	// Check the stored belief — if it already has high confidence or
	// multiple confirmations, propagate to the project knowledge graph.
	belief, err := factStore.GetBelief(ctx, beliefID)
	if err != nil {
		return
	}

	engine.propagateIfSignificant(ctx, role, belief)
}

// propagateIfSignificant checks whether a belief is significant enough
// to share across all agents via the project knowledge graph.
// A belief is significant when it has high confidence (>= 0.6) or
// multiple confirmations (>= 2), AND mentions project-level concepts.
func (engine *ConversationEngine) propagateIfSignificant(ctx context.Context, role agent.Role, belief memory.Belief) {
	if belief.Confidence < 0.6 && belief.Confirmations < 2 {
		return
	}

	if !containsProjectSignal(belief.Content) {
		return
	}

	engine.propagateToProjectGraph(ctx, role, belief.Content)
}

// propagateToProjectGraph writes a belief to the shared project fact
// store using confirm-or-create semantics. If a similar belief already
// exists, it gets confirmed instead of duplicated.
func (engine *ConversationEngine) propagateToProjectGraph(ctx context.Context, role agent.Role, text string) {
	engine.mu.Lock()
	store := engine.projectFactStore
	engine.mu.Unlock()

	if store == nil {
		return
	}

	err := store.ConfirmOrCreate(ctx, text, "cross-pollination")
	if err != nil {
		log.Printf("cross-pollination from %s failed: %v", role, err)
	}
}

// containsProjectSignal returns true if the text mentions modules,
// patterns, or architecture — things worth sharing across agents.
func containsProjectSignal(text string) bool {
	lower := strings.ToLower(text)
	for _, signal := range projectSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

// ContainsProjectSignalForTest exports containsProjectSignal for testing.
func ContainsProjectSignalForTest(text string) bool {
	return containsProjectSignal(text)
}
