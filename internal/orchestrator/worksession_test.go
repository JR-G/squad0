package orchestrator_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildImplementationPrompt_IncludesTicketAndDescription(t *testing.T) {
	t.Parallel()

	prompt := orchestrator.BuildImplementationPrompt("JAM-42", "Add user authentication")

	assert.Contains(t, prompt, "JAM-42")
	assert.Contains(t, prompt, "Add user authentication")
	assert.Contains(t, prompt, "gh pr create")
	assert.Contains(t, prompt, "recall")
	assert.Contains(t, prompt, "remember_fact")
	assert.Contains(t, prompt, "Linear MCP")
}

func TestNewWorkSession_CreatesWorktree(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	initTestRepo(t, repoDir)

	ws, err := orchestrator.NewWorkSession(context.Background(), repoDir, agent.RoleEngineer1, "JAM-42")

	require.NoError(t, err)
	assert.DirExists(t, ws.Dir())

	ws.Cleanup(context.Background())
	assert.NoDirExists(t, ws.Dir())
}

func TestNewWorkSession_InvalidRepo_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := orchestrator.NewWorkSession(context.Background(), "/nonexistent/repo", agent.RoleEngineer1, "JAM-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating worktree")
}

func TestWorkSession_Cleanup_NonexistentWorktree_DoesNotPanic(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	initTestRepo(t, repoDir)

	ws, err := orchestrator.NewWorkSession(context.Background(), repoDir, agent.RoleEngineer1, "JAM-99")
	require.NoError(t, err)

	ws.Cleanup(context.Background())
	ws.Cleanup(context.Background())
}

func initTestRepo(t *testing.T, dir string) {
	t.Helper()

	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}

	for _, cmd := range commands {
		c := execCommand(dir, cmd[0], cmd[1:]...)
		output, err := c.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", cmd, string(output))
	}

	testFile := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(testFile, []byte("# test"), 0o644))

	c := execCommand(dir, "git", "add", ".")
	_, err := c.CombinedOutput()
	require.NoError(t, err)

	c = execCommand(dir, "git", "commit", "-m", "initial")
	_, err = c.CombinedOutput()
	require.NoError(t, err)
}

func execCommand(dir, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd
}
