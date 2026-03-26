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
	_, err := factStore.CreateBelief(ctx, memory.Belief{
		Content:       text,
		Confidence:    0.4,
		Confirmations: 1,
		SourceOutcome: "conversation",
	})
	if err != nil {
		log.Printf("failed to store conversation belief for %s: %v", role, err)
	}
}
