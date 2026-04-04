package runtime

import (
	"fmt"
	"os/exec"
	"strings"
)

// TmuxExecutor provides an interface for tmux operations. Tests use
// a stub. Production uses ExecTmuxExecutor.
type TmuxExecutor interface {
	NewSession(name, workDir, cmd string, args ...string) error
	HasSession(name string) bool
	KillSession(name string) error
}

// ExecTmuxExecutor implements TmuxExecutor via real tmux commands.
type ExecTmuxExecutor struct{}

// NewSession creates a detached tmux session.
func (ExecTmuxExecutor) NewSession(name, workDir, cmd string, args ...string) error {
	full := cmd + " " + strings.Join(args, " ")
	out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", workDir, full).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-session %s: %s: %w", name, out, err)
	}
	return nil
}

// HasSession checks if a tmux session exists.
func (ExecTmuxExecutor) HasSession(name string) bool {
	return exec.Command("tmux", "has-session", "-t", name).Run() == nil //nolint:gosec // name is internal
}

// KillSession destroys a tmux session.
func (ExecTmuxExecutor) KillSession(name string) error {
	_ = exec.Command("tmux", "kill-session", "-t", name).Run() //nolint:gosec // name is internal
	return nil
}

// SessionName returns the canonical tmux session name for a role.
func SessionName(role string) string {
	return "squad0-" + role
}
