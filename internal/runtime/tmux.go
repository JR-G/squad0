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
	return runTmux("new-session", "-d", "-s", name, "-c", workDir, cmd+" "+strings.Join(args, " "))
}

// HasSession checks if a tmux session exists.
func (ExecTmuxExecutor) HasSession(name string) bool {
	return runTmux("has-session", "-t", name) == nil
}

// KillSession destroys a tmux session.
func (ExecTmuxExecutor) KillSession(name string) error {
	_ = runTmux("kill-session", "-t", name)
	return nil
}

func runTmux(args ...string) error {
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %s: %w", args[0], out, err)
	}
	return nil
}

// SessionName returns the canonical tmux session name for a role.
func SessionName(role string) string {
	return "squad0-" + role
}
