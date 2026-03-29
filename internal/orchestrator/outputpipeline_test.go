package orchestrator_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestOutputPipeline_Process_Valid_Passes(t *testing.T) {
	t.Parallel()

	pipe := orchestrator.NewOutputPipeline()
	text := "Looks good."

	out, result := pipe.Process(text, agent.RoleEngineer1, nil)

	assert.True(t, result.OK)
	assert.Equal(t, text, out)
}

func TestOutputPipeline_Process_BannedPhrase_Rejects(t *testing.T) {
	t.Parallel()

	pipe := orchestrator.NewOutputPipeline()
	text := "As an AI, I think this is fine."

	out, result := pipe.Process(text, agent.RoleEngineer1, nil)

	assert.False(t, result.OK)
	assert.Empty(t, out)
	assert.Contains(t, result.Reason, "banned phrase")
}

func TestOutputPipeline_Process_TooLong_Rejects(t *testing.T) {
	t.Parallel()

	pipe := orchestrator.NewOutputPipeline()
	rules := pipe.RulesForRole(agent.RoleEngineer3)

	// Build a string that exceeds engineer-3's limit.
	long := make([]byte, rules.MaxChars+50)
	for idx := range long {
		long[idx] = 'a'
	}

	out, result := pipe.Process(string(long), agent.RoleEngineer3, nil)

	assert.False(t, result.OK)
	assert.Empty(t, out)
	assert.Equal(t, "too long", result.Reason)
}

func TestOutputPipeline_RulesForRole_KnownRole(t *testing.T) {
	t.Parallel()

	pipe := orchestrator.NewOutputPipeline()

	for _, role := range agent.AllRoles() {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			rules := pipe.RulesForRole(role)

			assert.Greater(t, rules.MaxChars, 0)
			assert.Greater(t, rules.MaxSentences, 0)
			assert.NotEmpty(t, rules.BannedPhrases)
		})
	}
}

func TestOutputPipeline_RulesForRole_UnknownRole(t *testing.T) {
	t.Parallel()

	pipe := orchestrator.NewOutputPipeline()
	rules := pipe.RulesForRole(agent.Role("nonexistent"))

	// Unknown roles fall back to DefaultVoiceRules which returns
	// the base defaults (300 chars, 3 sentences).
	assert.Greater(t, rules.MaxChars, 0)
	assert.Greater(t, rules.MaxSentences, 0)
	assert.NotEmpty(t, rules.BannedPhrases)
}
