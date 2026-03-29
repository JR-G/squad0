package orchestrator_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestDefaultVoiceRules_AllRoles_HavePositiveLimits(t *testing.T) {
	t.Parallel()

	roles := agent.AllRoles()
	assert.Len(t, roles, 7, "expected 7 roles")

	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			rules := orchestrator.DefaultVoiceRules(role)

			assert.Greater(t, rules.MaxChars, 0, "MaxChars must be positive for %s", role)
			assert.Greater(t, rules.MaxSentences, 0, "MaxSentences must be positive for %s", role)
		})
	}
}

func TestDefaultVoiceRules_Engineer3_IsLaconic(t *testing.T) {
	t.Parallel()

	eng3Rules := orchestrator.DefaultVoiceRules(agent.RoleEngineer3)
	tlRules := orchestrator.DefaultVoiceRules(agent.RoleTechLead)

	// Engineer-3 should have tighter limits than the tech lead.
	assert.Less(t, eng3Rules.MaxChars, tlRules.MaxChars,
		"engineer-3 MaxChars (%d) should be less than tech-lead MaxChars (%d)",
		eng3Rules.MaxChars, tlRules.MaxChars)
}

func TestDefaultVoiceRules_AllRoles_HaveBannedPhrases(t *testing.T) {
	t.Parallel()

	for _, role := range agent.AllRoles() {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			rules := orchestrator.DefaultVoiceRules(role)
			assert.NotEmpty(t, rules.BannedPhrases, "BannedPhrases must not be empty for %s", role)
		})
	}
}
