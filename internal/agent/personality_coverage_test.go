package agent_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
)

func TestAssemblePrompt_BeliefsOnly_SkipsFactsSection(t *testing.T) {
	t.Parallel()

	memCtx := memory.RetrievalContext{
		Beliefs: []memory.Belief{
			{Content: "explicit errors are better", Confidence: 0.8, Confirmations: 2},
		},
	}

	result := agent.AssemblePrompt("personality", memCtx, "task")

	assert.Contains(t, result, "# Relevant Memory")
	assert.Contains(t, result, "## Beliefs")
	assert.Contains(t, result, "explicit errors are better")
	assert.NotContains(t, result, "## Known Facts")
	assert.NotContains(t, result, "## Recent Sessions")
}

func TestAssemblePrompt_EpisodesOnly_SkipsFactsAndBeliefs(t *testing.T) {
	t.Parallel()

	memCtx := memory.RetrievalContext{
		Episodes: []memory.Episode{
			{Outcome: memory.OutcomeFailure, Ticket: "SQ-10", Summary: "context deadline"},
		},
	}

	result := agent.AssemblePrompt("personality", memCtx, "task")

	assert.Contains(t, result, "# Relevant Memory")
	assert.Contains(t, result, "## Recent Sessions")
	assert.Contains(t, result, "context deadline")
	assert.NotContains(t, result, "## Known Facts")
	assert.NotContains(t, result, "## Beliefs")
}

func TestAssemblePrompt_MultipleFacts_AllRendered(t *testing.T) {
	t.Parallel()

	memCtx := memory.RetrievalContext{
		Facts: []memory.Fact{
			{Content: "first fact", Type: memory.FactObservation, Confidence: 0.9},
			{Content: "second fact", Type: memory.FactWarning, Confidence: 0.6},
			{Content: "third fact", Type: memory.FactTechnique, Confidence: 0.4},
		},
	}

	result := agent.AssemblePrompt("personality", memCtx, "task")

	assert.Contains(t, result, "first fact")
	assert.Contains(t, result, "second fact")
	assert.Contains(t, result, "third fact")
}
