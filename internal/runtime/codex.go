package runtime

import (
	"context"
	"fmt"

	"github.com/JR-G/squad0/internal/agent"
)

// CodexRuntime executes prompts via the Codex CLI. Each Send spawns
// a fresh process — no persistent state, no hooks. First-class
// runtime, not a fallback.
type CodexRuntime struct {
	runner  agent.ProcessRunner
	model   string
	workDir string
}

// NewCodexRuntime creates a CodexRuntime with the given model.
// The model maps to Codex's -m flag (e.g. "gpt-5-codex", "auto").
func NewCodexRuntime(runner agent.ProcessRunner, model, workDir string) *CodexRuntime {
	return &CodexRuntime{
		runner:  runner,
		model:   model,
		workDir: workDir,
	}
}

// Start is a no-op for Codex — each Send is a fresh process.
func (rt *CodexRuntime) Start(_ context.Context, _ StartConfig) error {
	return nil
}

// Send spawns a Codex CLI process with the prompt and returns the
// parsed response. Uses the existing BuildCodexArgs/ParseCodexOutput
// from the agent package.
func (rt *CodexRuntime) Send(ctx context.Context, prompt string) (string, error) {
	args := agent.BuildCodexArgs(prompt, rt.workDir, rt.model)
	output, err := rt.runner.Run(ctx, "", rt.workDir, "codex", args...)
	if err != nil {
		return agent.ParseCodexOutput(string(output)), fmt.Errorf("codex send: %w", err)
	}

	transcript := agent.ParseCodexOutput(string(output))
	if transcript == "" {
		return "", fmt.Errorf("codex returned empty response")
	}

	return transcript, nil
}

// IsAlive always returns false — Codex has no persistent session.
func (rt *CodexRuntime) IsAlive() bool {
	return false
}

// Stop is a no-op for Codex — nothing persistent to tear down.
func (rt *CodexRuntime) Stop() error {
	return nil
}

// Name returns "codex".
func (rt *CodexRuntime) Name() string {
	return "codex"
}

// SupportsHooks returns false — Codex CLI does not support Claude
// Code hooks.
func (rt *CodexRuntime) SupportsHooks() bool {
	return false
}
