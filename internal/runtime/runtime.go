package runtime

import (
	"context"

	"github.com/JR-G/squad0/internal/agent"
)

// Runtime is the core abstraction for agent session execution.
// Both Claude Code and Codex implement this interface as equal peers.
// The orchestrator interacts with runtimes through the SessionBridge,
// never directly.
type Runtime interface {
	// Start initialises the runtime. For persistent runtimes (tmux),
	// this creates a session. For fresh-process runtimes, this is a
	// no-op.
	Start(ctx context.Context, cfg StartConfig) error

	// Send delivers a prompt and returns the response text. For
	// persistent runtimes, this injects via the inbox queue. For
	// fresh-process runtimes, this spawns a process.
	Send(ctx context.Context, prompt string) (string, error)

	// IsAlive reports whether a persistent session exists and is
	// healthy. Fresh-process runtimes always return false.
	IsAlive() bool

	// Stop tears down any persistent state (tmux sessions, temp
	// directories). Safe to call multiple times.
	Stop() error

	// Name returns the runtime identifier ("claude", "codex", etc.).
	Name() string

	// SupportsHooks reports whether this runtime supports Claude Code
	// hooks (SessionStart, UserPromptSubmit). Determines whether the
	// bridge can use the persistent-session optimisation.
	SupportsHooks() bool
}

// StartConfig holds the parameters needed to initialise a runtime.
type StartConfig struct {
	Role            agent.Role
	Model           string
	WorkDir         string
	PersonalityPath string
	ExtraEnv        map[string]string
}
