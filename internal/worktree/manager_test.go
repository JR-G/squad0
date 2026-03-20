package worktree_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/worktree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeGitRunner struct {
	responses map[string]fakeGitResponse
	calls     []string
}

type fakeGitResponse struct {
	output []byte
	err    error
}

func newFakeGitRunner() *fakeGitRunner {
	return &fakeGitRunner{responses: make(map[string]fakeGitResponse)}
}

func (runner *fakeGitRunner) On(args string, output []byte, err error) {
	runner.responses[args] = fakeGitResponse{output: output, err: err}
}

func (runner *fakeGitRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	runner.calls = append(runner.calls, key)

	resp, ok := runner.responses[key]
	if !ok {
		return nil, fmt.Errorf("unexpected git command: %s", key)
	}

	return resp.output, resp.err
}

func TestManager_Create_CallsGitWorktreeAdd(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, t.TempDir())
	expectedPath := mgr.PathForRole(agent.RoleEngineer1)

	git.On(
		fmt.Sprintf("worktree add -b feat/sq-42 %s", expectedPath),
		[]byte("Preparing worktree\n"), nil,
	)

	path, err := mgr.Create(context.Background(), agent.RoleEngineer1, "feat/sq-42")

	require.NoError(t, err)
	assert.Equal(t, expectedPath, path)
}

func TestManager_Create_GitError_ReturnsError(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, t.TempDir())
	expectedPath := mgr.PathForRole(agent.RoleEngineer1)

	git.On(
		fmt.Sprintf("worktree add -b feat/sq-42 %s", expectedPath),
		[]byte("fatal: branch already exists\n"),
		fmt.Errorf("exit status 128"),
	)

	_, err := mgr.Create(context.Background(), agent.RoleEngineer1, "feat/sq-42")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating worktree")
}

func TestManager_Remove_CallsGitWorktreeRemove(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, t.TempDir())
	expectedPath := mgr.PathForRole(agent.RoleEngineer2)

	git.On(
		fmt.Sprintf("worktree remove --force %s", expectedPath),
		nil, nil,
	)

	err := mgr.Remove(context.Background(), agent.RoleEngineer2)

	require.NoError(t, err)
}

func TestManager_Remove_GitError_ReturnsError(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, t.TempDir())
	expectedPath := mgr.PathForRole(agent.RoleEngineer2)

	git.On(
		fmt.Sprintf("worktree remove --force %s", expectedPath),
		[]byte("error\n"), fmt.Errorf("exit status 1"),
	)

	err := mgr.Remove(context.Background(), agent.RoleEngineer2)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing worktree")
}

func TestManager_List_ParsesWorktreePaths(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, "/base")

	git.On("worktree list --porcelain", []byte(
		"worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n"+
			"worktree /base/engineer-1\nHEAD def456\nbranch refs/heads/feat/sq-42\n\n",
	), nil)

	paths, err := mgr.List(context.Background())

	require.NoError(t, err)
	assert.Len(t, paths, 2)
	assert.Contains(t, paths, "/repo")
	assert.Contains(t, paths, "/base/engineer-1")
}

func TestManager_Prune_CallsGitWorktreePrune(t *testing.T) {
	t.Parallel()

	git := newFakeGitRunner()
	mgr := worktree.NewManager(git, "/base")

	git.On("worktree prune", nil, nil)

	err := mgr.Prune(context.Background())

	require.NoError(t, err)
}

func TestManager_PathForRole_ReturnsCorrectPath(t *testing.T) {
	t.Parallel()

	mgr := worktree.NewManager(nil, "/worktrees")

	assert.Equal(t, "/worktrees/engineer-1", mgr.PathForRole(agent.RoleEngineer1))
	assert.Equal(t, "/worktrees/pm", mgr.PathForRole(agent.RolePM))
}
