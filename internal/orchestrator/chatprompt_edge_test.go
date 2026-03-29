package orchestrator_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestChannelInstruction_Chitchat_ReturnsCasualMessage(t *testing.T) {
	t.Parallel()

	result := orchestrator.ChannelInstructionForTest("chitchat")
	assert.Contains(t, result, "casual")
}

func TestChannelInstruction_Engineering_ReturnsWork(t *testing.T) {
	t.Parallel()

	result := orchestrator.ChannelInstructionForTest("engineering")
	assert.Equal(t, "work", result)
}

func TestRoleDescription_AllRoles_NonEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role agent.Role
		want string
	}{
		{"PM", agent.RolePM, "PM"},
		{"TechLead", agent.RoleTechLead, "Tech Lead"},
		{"Engineer1", agent.RoleEngineer1, "Engineer"},
		{"Engineer2", agent.RoleEngineer2, "Engineer"},
		{"Engineer3", agent.RoleEngineer3, "Engineer"},
		{"Reviewer", agent.RoleReviewer, "Reviewer"},
		{"Designer", agent.RoleDesigner, "Designer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := orchestrator.RoleDescriptionForTest(tt.role)
			assert.NotEmpty(t, result)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestRoleDescription_UnknownRole_ReturnsFallback(t *testing.T) {
	t.Parallel()

	result := orchestrator.RoleDescriptionForTest(agent.Role("unknown-agent"))
	assert.Equal(t, "unknown-agent", result)
}

func TestRoleTitle_AllRoles_NonEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role agent.Role
		want string
	}{
		{"PM", agent.RolePM, "PM"},
		{"TechLead", agent.RoleTechLead, "Tech Lead"},
		{"Engineer1", agent.RoleEngineer1, "Engineer"},
		{"Engineer2", agent.RoleEngineer2, "Engineer"},
		{"Engineer3", agent.RoleEngineer3, "Engineer"},
		{"Reviewer", agent.RoleReviewer, "Reviewer"},
		{"Designer", agent.RoleDesigner, "Designer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := orchestrator.RoleTitleForTest(tt.role)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestRoleTitle_UnknownRole_ReturnsFallback(t *testing.T) {
	t.Parallel()

	result := orchestrator.RoleTitleForTest(agent.Role("mystery"))
	assert.Equal(t, "mystery", result)
}
