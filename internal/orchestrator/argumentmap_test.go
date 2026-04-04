package orchestrator_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestArgumentMap_Empty_IsEmpty(t *testing.T) {
	t.Parallel()

	am := orchestrator.NewArgumentMap()
	assert.True(t, am.IsEmpty())
	assert.Equal(t, "", am.Format(nil))
}

func TestArgumentMap_AddPosition_NotEmpty(t *testing.T) {
	t.Parallel()

	am := orchestrator.NewArgumentMap()
	am.AddPosition(agent.RoleEngineer1, "use a queue for async processing")

	assert.False(t, am.IsEmpty())
}

func TestArgumentMap_Format_IncludesPositions(t *testing.T) {
	t.Parallel()

	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Callum",
		agent.RoleTechLead:  "Sable",
	}

	am := orchestrator.NewArgumentMap()
	am.AddPosition(agent.RoleEngineer1, "use a queue")
	am.AddPosition(agent.RoleTechLead, "use a channel instead")

	formatted := am.Format(roster)
	assert.Contains(t, formatted, "Callum: use a queue")
	assert.Contains(t, formatted, "Sable: use a channel instead")
}

func TestArgumentMap_Format_IncludesConcerns(t *testing.T) {
	t.Parallel()

	am := orchestrator.NewArgumentMap()
	am.AddConcern("what about backpressure?")

	formatted := am.Format(nil)
	assert.Contains(t, formatted, "Unresolved Concerns")
	assert.Contains(t, formatted, "what about backpressure?")
}

func TestArgumentMap_Format_IncludesEvidence(t *testing.T) {
	t.Parallel()

	am := orchestrator.NewArgumentMap()
	am.AddPosition(agent.RoleEngineer1, "some approach")
	am.AddEvidence("benchmarks show 2x throughput")

	formatted := am.Format(nil)
	assert.Contains(t, formatted, "Evidence")
	assert.Contains(t, formatted, "benchmarks show 2x throughput")
}

func TestArgumentMap_Format_IncludesDecision(t *testing.T) {
	t.Parallel()

	am := orchestrator.NewArgumentMap()
	am.AddPosition(agent.RolePM, "ship the queue approach")
	am.SetDecision("use the queue approach with backpressure handling")

	formatted := am.Format(nil)
	assert.Contains(t, formatted, "Decision")
	assert.Contains(t, formatted, "use the queue approach")
}

func TestArgumentMap_Format_FallbackName(t *testing.T) {
	t.Parallel()

	am := orchestrator.NewArgumentMap()
	am.AddPosition(agent.RoleEngineer1, "some thought")

	// No roster entry — should fall back to role ID.
	formatted := am.Format(nil)
	assert.Contains(t, formatted, string(agent.RoleEngineer1))
}

func TestClassifyMessage_Position(t *testing.T) {
	t.Parallel()

	category, content := orchestrator.ClassifyMessage("I think we should use a worker pool", agent.RoleEngineer1)
	assert.Equal(t, "position", category)
	assert.NotEmpty(t, content)
}

func TestClassifyMessage_Concern(t *testing.T) {
	t.Parallel()

	category, content := orchestrator.ClassifyMessage("I'm concerned about the memory usage here", agent.RoleEngineer2)
	assert.Equal(t, "concern", category)
	assert.NotEmpty(t, content)
}

func TestClassifyMessage_Evidence(t *testing.T) {
	t.Parallel()

	category, content := orchestrator.ClassifyMessage("Because the last time we tried this it caused a deadlock", agent.RoleTechLead)
	assert.Equal(t, "evidence", category)
	assert.NotEmpty(t, content)
}

func TestClassifyMessage_Short_Empty(t *testing.T) {
	t.Parallel()

	category, _ := orchestrator.ClassifyMessage("ok", agent.RoleEngineer3)
	assert.Equal(t, "", category)
}

func TestClassifyMessage_LongNoSignal_IsPosition(t *testing.T) {
	t.Parallel()

	category, _ := orchestrator.ClassifyMessage("The authentication module needs a complete overhaul to support OAuth2 tokens", agent.RoleEngineer1)
	assert.Equal(t, "position", category)
}
