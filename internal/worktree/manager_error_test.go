package worktree_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/worktree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_Create_MkdirFails_ReturnsError(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()

	// Use a path under a file (not a directory) so MkdirAll fails.
	blockingFile := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blockingFile, []byte("x"), 0o644))

	baseDir := filepath.Join(blockingFile, "deep", "nested")
	mgr := worktree.NewManager(git, baseDir)

	_, err := mgr.Create(context.Background(), agent.RoleEngineer1, "feat/sq-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating worktree parent dir")
}

func TestManager_List_GitError_ReturnsError(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, "/base")

	git.On("worktree list --porcelain",
		[]byte("fatal: not a git repository\n"),
		fmt.Errorf("exit status 128"),
	)

	_, err := mgr.List(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing worktrees")
}

func TestManager_Prune_GitError_ReturnsError(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, "/base")

	git.On("worktree prune",
		[]byte("error: failed to prune\n"),
		fmt.Errorf("exit status 1"),
	)

	err := mgr.Prune(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pruning worktrees")
}

func TestManager_List_EmptyOutput_ReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, "/base")

	git.On("worktree list --porcelain", []byte(""), nil)

	paths, err := mgr.List(context.Background())

	require.NoError(t, err)
	assert.Empty(t, paths)
}

func TestManager_List_SingleWorktree_ReturnsSinglePath(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, "/base")

	git.On("worktree list --porcelain", []byte(
		"worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n",
	), nil)

	paths, err := mgr.List(context.Background())

	require.NoError(t, err)
	require.Len(t, paths, 1)
	assert.Equal(t, "/repo", paths[0])
}

func TestManager_Create_Success_RecordsGitCall(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, t.TempDir())
	expectedPath := mgr.PathForRole(agent.RoleEngineer3)

	git.On(
		fmt.Sprintf("worktree add -b fix/sq-99 %s", expectedPath),
		[]byte("Preparing worktree\n"), nil,
	)

	path, err := mgr.Create(context.Background(), agent.RoleEngineer3, "fix/sq-99")

	require.NoError(t, err)
	assert.Equal(t, expectedPath, path)
	require.Len(t, git.calls, 1)
	assert.Contains(t, git.calls[0], "worktree add")
}

func TestManager_Remove_GitError_IncludesOutput(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, t.TempDir())
	expectedPath := mgr.PathForRole(agent.RoleEngineer1)

	git.On(
		fmt.Sprintf("worktree remove --force %s", expectedPath),
		[]byte("fatal: is not a valid directory\n"),
		fmt.Errorf("exit status 128"),
	)

	err := mgr.Remove(context.Background(), agent.RoleEngineer1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing worktree")
	assert.Contains(t, err.Error(), "exit status 128")
}

func TestNewExecGitRunner_CreatesRunner(t *testing.T) {
	t.Parallel()

	runner := worktree.NewExecGitRunner("/tmp/test-repo")

	assert.NotNil(t, runner)
}

func TestExecGitRunner_Run_InvalidCommand_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := worktree.NewExecGitRunner(t.TempDir())

	_, err := runner.Run(context.Background(), "this-is-not-a-real-subcommand")

	require.Error(t, err)
}

func TestExecGitRunner_Run_VersionSucceeds(t *testing.T) {
	t.Parallel()

	runner := worktree.NewExecGitRunner(t.TempDir())

	output, err := runner.Run(context.Background(), "version")

	require.NoError(t, err)
	assert.Contains(t, string(output), "git version")
}

func TestManager_PathForRole_AllEngineers(t *testing.T) {
	t.Parallel()

	mgr := worktree.NewManager(nil, "/worktrees")

	tests := []struct {
		role     agent.Role
		expected string
	}{
		{agent.RoleEngineer1, "/worktrees/engineer-1"},
		{agent.RoleEngineer2, "/worktrees/engineer-2"},
		{agent.RoleEngineer3, "/worktrees/engineer-3"},
		{agent.RoleTechLead, "/worktrees/tech-lead"},
		{agent.RoleReviewer, "/worktrees/reviewer"},
		{agent.RoleDesigner, "/worktrees/designer"},
	}

	for _, testCase := range tests {
		assert.Equal(t, testCase.expected, mgr.PathForRole(testCase.role))
	}
}
