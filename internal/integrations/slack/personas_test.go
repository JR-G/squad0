package slack_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/stretchr/testify/assert"
)

func TestDefaultPersonas_HasAllRoles(t *testing.T) {
	t.Parallel()

	personas := slack.DefaultPersonas()

	for _, role := range agent.AllRoles() {
		_, ok := personas[role]
		assert.True(t, ok, "missing persona for role %s", role)
	}
}

func TestDefaultPersonas_AllUsesBunxNotNpx(t *testing.T) {
	t.Parallel()

	personas := slack.DefaultPersonas()

	for role, persona := range personas {
		assert.NotEmpty(t, persona.Username, "persona for %s has empty username", role)
	}
}

func TestPersonaForRole_ReturnsConfiguredPersona(t *testing.T) {
	t.Parallel()

	personas := map[agent.Role]slack.Persona{
		agent.RolePM: {Username: "PM Bot", IconURL: "https://example.com/pm.png"},
	}

	result := slack.PersonaForRole(agent.RolePM, personas)

	assert.Equal(t, "PM Bot", result.Username)
	assert.Equal(t, "https://example.com/pm.png", result.IconURL)
}

func TestPersonaForRole_UnknownRole_FallsBackToRoleName(t *testing.T) {
	t.Parallel()

	personas := map[agent.Role]slack.Persona{}

	result := slack.PersonaForRole(agent.RoleDesigner, personas)

	assert.Equal(t, "designer", result.Username)
}
