package orchestrator_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestPreSubmitCheckForTest_ContainsGitStatus(t *testing.T) {
	t.Parallel()

	prompt := orchestrator.PreSubmitCheckForTest()

	assert.Contains(t, prompt, "git status")
	assert.Contains(t, prompt, "git diff origin/main..HEAD --stat")
	assert.Contains(t, prompt, "CLEAN")
}

func TestRunPreSubmitCheck_Clean_ReturnsTrue(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Everything is CLEAN — no uncommitted changes"}` + "\n"),
	}
	engAgent := setupAgentWithRole(t, runner, agent.RoleEngineer1)

	result := orchestrator.RunPreSubmitCheck(context.Background(), engAgent, t.TempDir())

	assert.True(t, result)
}

func TestRunPreSubmitCheck_Dirty_ReturnsFalse(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"There are uncommitted changes in main.go"}` + "\n"),
	}
	engAgent := setupAgentWithRole(t, runner, agent.RoleEngineer1)

	result := orchestrator.RunPreSubmitCheck(context.Background(), engAgent, t.TempDir())

	assert.False(t, result)
}

func TestRunPreSubmitCheck_SessionError_ReturnsFalse(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"session crashed"}` + "\n"),
		err:    assert.AnError,
	}
	engAgent := setupAgentWithRole(t, runner, agent.RoleEngineer1)

	result := orchestrator.RunPreSubmitCheck(context.Background(), engAgent, t.TempDir())

	assert.False(t, result)
}

func TestRunPreSubmitCheck_CleanLowercase_ReturnsTrue(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"all clean, pushed everything"}` + "\n"),
	}
	engAgent := setupAgentWithRole(t, runner, agent.RoleEngineer1)

	result := orchestrator.RunPreSubmitCheck(context.Background(), engAgent, t.TempDir())

	assert.True(t, result)
}
