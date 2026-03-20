package agent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersonalityLoader_LoadBase_ReturnsContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := "# Engineer 1\n\nYou are thorough and defensive."
	err := os.WriteFile(filepath.Join(dir, "engineer-1.md"), []byte(content), 0o644)
	require.NoError(t, err)

	loader := agent.NewPersonalityLoader(dir)
	result, err := loader.LoadBase(agent.RoleEngineer1)

	require.NoError(t, err)
	assert.Equal(t, content, result)
}

func TestPersonalityLoader_LoadBase_MissingFile_ReturnsError(t *testing.T) {
	t.Parallel()

	loader := agent.NewPersonalityLoader(t.TempDir())
	_, err := loader.LoadBase(agent.RolePM)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading personality file")
}

func TestAssemblePrompt_IncludesAllSections(t *testing.T) {
	t.Parallel()

	personality := "You are a thorough engineer."
	memCtx := memory.RetrievalContext{
		Facts: []memory.Fact{
			{Content: "WAL mode is fast", Type: memory.FactObservation, Confidence: 0.8},
		},
		Beliefs: []memory.Belief{
			{Content: "tests prevent regressions", Confidence: 0.9, Confirmations: 3},
		},
		Episodes: []memory.Episode{
			{Outcome: memory.OutcomeSuccess, Ticket: "SQ-42", Summary: "Fixed auth bug"},
		},
	}
	task := "Implement payment retry logic"

	result := agent.AssemblePrompt(personality, memCtx, task)

	assert.Contains(t, result, "# Personality")
	assert.Contains(t, result, "You are a thorough engineer.")
	assert.Contains(t, result, "# Relevant Memory")
	assert.Contains(t, result, "WAL mode is fast")
	assert.Contains(t, result, "tests prevent regressions")
	assert.Contains(t, result, "Fixed auth bug")
	assert.Contains(t, result, "# Current Task")
	assert.Contains(t, result, "Implement payment retry logic")
}

func TestAssemblePrompt_EmptyMemory_OmitsMemorySection(t *testing.T) {
	t.Parallel()

	personality := "You are a PM."
	memCtx := memory.RetrievalContext{}
	task := "Run standup"

	result := agent.AssemblePrompt(personality, memCtx, task)

	assert.Contains(t, result, "# Personality")
	assert.NotContains(t, result, "# Relevant Memory")
	assert.Contains(t, result, "# Current Task")
}

func TestAssemblePrompt_FactsOnly_ShowsFactsSection(t *testing.T) {
	t.Parallel()

	memCtx := memory.RetrievalContext{
		Facts: []memory.Fact{
			{Content: "important fact", Type: memory.FactWarning, Confidence: 0.7},
		},
	}

	result := agent.AssemblePrompt("personality", memCtx, "task")

	assert.Contains(t, result, "## Known Facts")
	assert.NotContains(t, result, "## Beliefs")
	assert.NotContains(t, result, "## Recent Sessions")
}
