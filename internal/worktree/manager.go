package worktree

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

// GitRunner executes git commands. This interface exists so tests can
// avoid real git operations.
type GitRunner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// ExecGitRunner implements GitRunner using os/exec.
type ExecGitRunner struct {
	repoDir string
}

// NewExecGitRunner creates a GitRunner that runs git in the given repo.
func NewExecGitRunner(repoDir string) *ExecGitRunner {
	return &ExecGitRunner{repoDir: repoDir}
}

// Run executes a git command in the repo directory.
func (runner *ExecGitRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = runner.repoDir
	return cmd.CombinedOutput()
}

// Manager creates and cleans up git worktrees for agent sessions.
type Manager struct {
	git     GitRunner
	baseDir string
}

// NewManager creates a worktree Manager that stores worktrees under the
// given base directory relative to the repo root.
func NewManager(git GitRunner, baseDir string) *Manager {
	return &Manager{
		git:     git,
		baseDir: baseDir,
	}
}

// Create creates a new git worktree for the given agent and branch name.
// Returns the absolute path to the worktree directory.
func (mgr *Manager) Create(ctx context.Context, role agent.Role, branchName string) (string, error) {
	worktreeDir := filepath.Join(mgr.baseDir, string(role))

	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		return "", fmt.Errorf("creating worktree parent dir: %w", err)
	}

	// Try creating with existing branch first. If the branch doesn't
	// exist, fall back to creating a new one with -b.
	output, err := mgr.git.Run(ctx, "worktree", "add", worktreeDir, branchName)
	if err != nil {
		output, err = mgr.git.Run(ctx, "worktree", "add", "-b", branchName, worktreeDir)
	}
	if err != nil {
		return "", fmt.Errorf("creating worktree for %s: %s: %w", role, string(output), err)
	}

	return worktreeDir, nil
}

// Remove cleans up a worktree for the given agent.
func (mgr *Manager) Remove(ctx context.Context, role agent.Role) error {
	worktreeDir := filepath.Join(mgr.baseDir, string(role))

	output, err := mgr.git.Run(ctx, "worktree", "remove", "--force", worktreeDir)
	if err != nil {
		return fmt.Errorf("removing worktree for %s: %s: %w", role, string(output), err)
	}

	return nil
}

// List returns the paths of all active worktrees.
func (mgr *Manager) List(ctx context.Context) ([]string, error) {
	output, err := mgr.git.Run(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("listing worktrees: %w", err)
	}

	var paths []string
	for _, line := range strings.Split(string(output), "\n") {
		trimmed := strings.TrimPrefix(line, "worktree ")
		if trimmed != line && trimmed != "" {
			paths = append(paths, trimmed)
		}
	}

	return paths, nil
}

// Prune removes stale worktree references.
func (mgr *Manager) Prune(ctx context.Context) error {
	output, err := mgr.git.Run(ctx, "worktree", "prune")
	if err != nil {
		return fmt.Errorf("pruning worktrees: %s: %w", string(output), err)
	}
	return nil
}

// PathForRole returns the worktree path for a given agent role.
func (mgr *Manager) PathForRole(role agent.Role) string {
	return filepath.Join(mgr.baseDir, string(role))
}
