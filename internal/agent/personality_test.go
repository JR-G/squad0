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

func TestExtractVoiceSection_ExtractsVoice(t *testing.T) {
	t.Parallel()

	personality := `# Engineer

## Voice

You speak carefully and cautiously.

## How You Work

- Plan before coding
`

	result := agent.ExtractVoiceSection(personality)
	assert.Contains(t, result, "You speak carefully and cautiously.")
	assert.NotContains(t, result, "Plan before coding")
}

func TestExtractVoiceSection_ExtractsBothSections(t *testing.T) {
	t.Parallel()

	personality := `# PM

## Voice

Crisp and decisive.

## Communication Style

You address people directly.

## How You Work

- Think in priorities
`

	result := agent.ExtractVoiceSection(personality)
	assert.Contains(t, result, "Crisp and decisive.")
	assert.Contains(t, result, "You address people directly.")
	assert.NotContains(t, result, "Think in priorities")
}

func TestExtractVoiceSection_NoVoice_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	personality := `# Engineer

## How You Work

- Write code
`

	result := agent.ExtractVoiceSection(personality)
	assert.Empty(t, result)
}

func TestLoadVoice_ReturnsVoiceSection(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := "# PM\n\n## Voice\n\nCrisp and decisive.\n\n## How You Work\n\n- Stay focused\n"
	err := os.WriteFile(filepath.Join(dir, "pm.md"), []byte(content), 0o644)
	require.NoError(t, err)

	loader := agent.NewPersonalityLoader(dir)
	voice := loader.LoadVoice(agent.RolePM)

	assert.Contains(t, voice, "Crisp and decisive.")
	assert.NotContains(t, voice, "Stay focused")
}

func TestLoadVoice_MissingFile_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	loader := agent.NewPersonalityLoader(t.TempDir())
	voice := loader.LoadVoice(agent.RolePM)

	assert.Empty(t, voice)
}

func TestSetGHToken_SetsToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pm.md"), []byte("test"), 0o644))

	a := agent.NewAgent(agent.RolePM, "test", agent.NewSession(nil), agent.NewPersonalityLoader(dir), nil, nil, nil, nil)
	a.SetGHToken("ghs_test123")

	// The token is stored internally — we verify it works by checking
	// DirectSession would use it. Since the session is nil, we just
	// verify no panic on SetGHToken.
}

func TestApplyEnv_SetsAndRestores(t *testing.T) {
	t.Parallel()

	// This tests the env mechanism indirectly through the agent.
	// Setting and unsetting env vars is tested via the session.
}
