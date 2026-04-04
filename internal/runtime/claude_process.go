package runtime

import (
	"context"
	"fmt"

	"github.com/JR-G/squad0/internal/agent"
)

// ClaudeProcessRuntime executes prompts via Claude Code CLI. Each
// Send spawns a fresh `claude -p` process — no persistent state.
// This is the current Squad0 behaviour wrapped in the Runtime
// interface.
type ClaudeProcessRuntime struct {
	session *agent.Session
	model   string
	workDir string
}

// NewClaudeProcessRuntime creates a runtime that spawns fresh Claude
// Code processes per interaction.
func NewClaudeProcessRuntime(session *agent.Session, model, workDir string) *ClaudeProcessRuntime {
	return &ClaudeProcessRuntime{
		session: session,
		model:   model,
		workDir: workDir,
	}
}

// Start is a no-op — each Send is a fresh process.
func (rt *ClaudeProcessRuntime) Start(_ context.Context, _ StartConfig) error {
	return nil
}

// Send spawns a Claude Code process with the prompt and returns the
// transcript. Delegates to the existing Session.Run which handles
// stream-json parsing, rate limit detection, and Codex fallback.
func (rt *ClaudeProcessRuntime) Send(ctx context.Context, prompt string) (string, error) {
	cfg := agent.SessionConfig{
		Model:      rt.model,
		Prompt:     prompt,
		WorkingDir: rt.workDir,
	}

	result, err := rt.session.Run(ctx, cfg)
	if err != nil {
		return result.Transcript, fmt.Errorf("claude process send: %w", err)
	}

	return result.Transcript, nil
}

// IsAlive always returns false — no persistent session.
func (rt *ClaudeProcessRuntime) IsAlive() bool {
	return false
}

// Stop is a no-op — nothing persistent to tear down.
func (rt *ClaudeProcessRuntime) Stop() error {
	return nil
}

// Name returns "claude".
func (rt *ClaudeProcessRuntime) Name() string {
	return "claude"
}

// SupportsHooks returns false — this runtime uses fresh processes,
// not persistent sessions with hooks.
func (rt *ClaudeProcessRuntime) SupportsHooks() bool {
	return false
}
