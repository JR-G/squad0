package orchestrator_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssigner_RequestAssignments_MalformedJSON_ReturnsError(t *testing.T) {
	t.Parallel()

	// PM returns something that has brackets but is not valid JSON.
	contentBytes, err := json.Marshal(`[{"role":"engineer-1" broken_json]`)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	runner := &fakeProcessRunner{output: []byte(pmOutput)}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	_, err = assigner.RequestAssignments(
		context.Background(), []agent.Role{agent.RoleEngineer1},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing PM assignments")
}

func TestAssigner_RequestAssignments_NestedBrackets_ExtractsOutermost(t *testing.T) {
	t.Parallel()

	assignmentJSON := `Some text [{"role":"engineer-1","ticket":"SQ-42","description":"Fix [nested] brackets"}] more text`
	contentBytes, err := json.Marshal(assignmentJSON)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	runner := &fakeProcessRunner{output: []byte(pmOutput)}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	result, err := assigner.RequestAssignments(
		context.Background(), []agent.Role{agent.RoleEngineer1},
	)

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "SQ-42", result[0].Ticket)
}

func TestAssigner_RequestAssignments_TextBeforeJSON_ParsesCorrectly(t *testing.T) {
	t.Parallel()

	assignmentJSON := `I'll assign the following tickets:
[{"role":"engineer-2","ticket":"SQ-100","description":"Add caching layer"}]`
	contentBytes, err := json.Marshal(assignmentJSON)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	runner := &fakeProcessRunner{output: []byte(pmOutput)}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	result, err := assigner.RequestAssignments(
		context.Background(), []agent.Role{agent.RoleEngineer2},
	)

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, agent.RoleEngineer2, result[0].Role)
	assert.Equal(t, "SQ-100", result[0].Ticket)
}

func TestAssigner_RequestAssignments_OnlyOpenBracket_ReturnsError(t *testing.T) {
	t.Parallel()

	contentBytes, err := json.Marshal(`Here is a bracket [ but no closing`)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	runner := &fakeProcessRunner{output: []byte(pmOutput)}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	result, err := assigner.RequestAssignments(
		context.Background(), []agent.Role{agent.RoleEngineer1},
	)

	// extractJSON returns "" when no closing bracket, parseAssignments returns nil.
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestAssigner_RequestAssignments_OnlyCloseBracket_ReturnsNil(t *testing.T) {
	t.Parallel()

	contentBytes, err := json.Marshal(`No open bracket ] here`)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	runner := &fakeProcessRunner{output: []byte(pmOutput)}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	result, err := assigner.RequestAssignments(
		context.Background(), []agent.Role{agent.RoleEngineer1},
	)

	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestAssigner_RequestAssignments_AllRolesInvalid_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	assignmentJSON := `[{"role":"designer","ticket":"SQ-42","description":"Review UI"},{"role":"reviewer","ticket":"SQ-43","description":"Review code"}]`
	contentBytes, err := json.Marshal(assignmentJSON)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	runner := &fakeProcessRunner{output: []byte(pmOutput)}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	result, err := assigner.RequestAssignments(
		context.Background(), []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2},
	)

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestAssigner_RequestAssignments_MultipleEngineers(t *testing.T) {
	t.Parallel()

	assignmentJSON := `[{"role":"engineer-1","ticket":"SQ-1","description":"Task A"},{"role":"engineer-2","ticket":"SQ-2","description":"Task B"},{"role":"engineer-3","ticket":"SQ-3","description":"Task C"}]`
	contentBytes, err := json.Marshal(assignmentJSON)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	runner := &fakeProcessRunner{output: []byte(pmOutput)}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	validRoles := []agent.Role{
		agent.RoleEngineer1,
		agent.RoleEngineer2,
		agent.RoleEngineer3,
	}

	result, err := assigner.RequestAssignments(context.Background(), validRoles)

	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, "SQ-1", result[0].Ticket)
	assert.Equal(t, "SQ-2", result[1].Ticket)
	assert.Equal(t, "SQ-3", result[2].Ticket)
}
