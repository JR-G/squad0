package agent_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
)

func TestAllRoles_ReturnsSevenRoles(t *testing.T) {
	t.Parallel()

	roles := agent.AllRoles()

	assert.Len(t, roles, 7)
}

func TestRole_PersonalityFile_ReturnsCorrectFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role     agent.Role
		expected string
	}{
		{agent.RolePM, "pm.md"},
		{agent.RoleTechLead, "tech-lead.md"},
		{agent.RoleEngineer1, "engineer-1.md"},
		{agent.RoleEngineer2, "engineer-2.md"},
		{agent.RoleEngineer3, "engineer-3.md"},
		{agent.RoleReviewer, "reviewer.md"},
		{agent.RoleDesigner, "designer.md"},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.role.PersonalityFile())
		})
	}
}

func TestModelForRole_ReturnsConfiguredModel(t *testing.T) {
	t.Parallel()

	models := map[agent.Role]string{
		agent.RolePM:       "claude-haiku-4-5-20251001",
		agent.RoleTechLead: "claude-opus-4-6",
	}

	assert.Equal(t, "claude-haiku-4-5-20251001", agent.ModelForRole(agent.RolePM, models))
	assert.Equal(t, "claude-opus-4-6", agent.ModelForRole(agent.RoleTechLead, models))
}

func TestModelForRole_UnknownRole_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	models := map[agent.Role]string{}

	assert.Empty(t, agent.ModelForRole(agent.RolePM, models))
}
