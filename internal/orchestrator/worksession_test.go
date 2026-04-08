package orchestrator_test

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
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
	assert.Contains(t, prompt, "git push -u origin HEAD")
	assert.Contains(t, prompt, "Step 1:")
	assert.Contains(t, prompt, "SELF-REVIEW")
	assert.Contains(t, prompt, "recall")
	assert.Contains(t, prompt, "Linear")
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

func TestNewWorkSession_ExistingBranch_ChecksOutInstead(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	initTestRepo(t, repoDir)

	// Create the branch ahead of time (simulates an open PR).
	c := execCommand(repoDir, "git", "branch", "feat/jam-50")
	output, err := c.CombinedOutput()
	require.NoError(t, err, "branch creation failed: %s", string(output))

	// NewWorkSession should detect the existing branch and check it
	// out instead of failing with "branch already exists".
	ws, err := orchestrator.NewWorkSession(context.Background(), repoDir, agent.RoleEngineer1, "JAM-50")
	require.NoError(t, err)
	assert.DirExists(t, ws.Dir())

	branchCmd := execCommand(ws.Dir(), "git", "rev-parse", "--abbrev-ref", "HEAD")
	branchOutput, branchErr := branchCmd.Output()
	require.NoError(t, branchErr)
	assert.Equal(t, "feat/jam-50", strings.TrimSpace(string(branchOutput)))

	ws.Cleanup(context.Background())
}

func TestNewFixUpSession_ChecksOutExistingBranch(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	initTestRepo(t, repoDir)

	// Create the branch that a PR would be on.
	c := execCommand(repoDir, "git", "branch", "feat/jam-42")
	output, err := c.CombinedOutput()
	require.NoError(t, err, "branch creation failed: %s", string(output))

	// NewFixUpSession should check out that existing branch.
	// prURL is empty so extractPRBranch will fail, falling back to the
	// constructed name feat/jam-42.
	ws, err := orchestrator.NewFixUpSession(context.Background(), repoDir, "", agent.RoleEngineer1, "JAM-42")
	require.NoError(t, err)
	assert.DirExists(t, ws.Dir())

	// Verify the worktree is on the right branch.
	branchCmd := execCommand(ws.Dir(), "git", "rev-parse", "--abbrev-ref", "HEAD")
	branchOutput, branchErr := branchCmd.Output()
	require.NoError(t, branchErr)
	assert.Equal(t, "feat/jam-42", strings.TrimSpace(string(branchOutput)))

	ws.Cleanup(context.Background())
}

func TestNewFixUpSession_BranchNotExist_FallsBackToConstructed(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	initTestRepo(t, repoDir)

	// No branch exists yet — NewFixUpSession constructs feat/jam-99
	// which also doesn't exist, so this should fail.
	_, err := orchestrator.NewFixUpSession(context.Background(), repoDir, "", agent.RoleEngineer2, "JAM-99")
	// The branch doesn't exist locally, so worktree add fails.
	require.Error(t, err)
}

func TestNewFixUpSession_InvalidRepo_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := orchestrator.NewFixUpSession(context.Background(), "/nonexistent/repo", "", agent.RoleEngineer1, "JAM-1")
	require.Error(t, err)
}

func TestRescuePR_ExtractsPRURL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"https://github.com/JR-G/project/pull/42"}` + "\n"),
	}
	agentInstance := buildAgent(t, runner, agent.RoleEngineer1, memDB)

	checkIns := coordination.NewCheckInStore(sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: agentInstance},
		checkIns, nil, nil,
	)

	prURL := orch.RescuePRForTest(ctx, agentInstance, "/tmp/work", "JAM-1", "feat/JAM-1")
	assert.Equal(t, "https://github.com/JR-G/project/pull/42", prURL)
}

func TestRescuePR_NoPR_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"FAILED"}` + "\n"),
	}
	agentInstance := buildAgent(t, runner, agent.RoleEngineer1, memDB)

	checkIns := coordination.NewCheckInStore(sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: agentInstance},
		checkIns, nil, nil,
	)

	prURL := orch.RescuePRForTest(ctx, agentInstance, "/tmp/work", "JAM-1", "feat/JAM-1")
	assert.Empty(t, prURL)
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
