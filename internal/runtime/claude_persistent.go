package runtime

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	// defaultResponseTimeout is how long Send waits for a response
	// before falling back to a fresh process.
	defaultResponseTimeout = 60 * time.Second
)

// ClaudePersistentRuntime keeps a Claude Code session alive in tmux.
// The personality is loaded once at session start via the SessionStart
// hook. Messages are injected via the UserPromptSubmit hook which
// drains the filesystem inbox. Responses are collected from the outbox.
type ClaudePersistentRuntime struct {
	mu      sync.Mutex
	tmux    TmuxExecutor
	inbox   *Inbox
	role    string
	model   string
	workDir string
	session string // tmux session name
	started bool
	timeout time.Duration
}

// NewClaudePersistentRuntime creates a runtime that maintains a
// persistent tmux session for the given role.
func NewClaudePersistentRuntime(
	tmux TmuxExecutor,
	inbox *Inbox,
	role, model, workDir string,
) *ClaudePersistentRuntime {
	return &ClaudePersistentRuntime{
		tmux:    tmux,
		inbox:   inbox,
		role:    role,
		model:   model,
		workDir: workDir,
		session: SessionName(role),
		timeout: defaultResponseTimeout,
	}
}

// SetTimeout overrides the response timeout for testing.
func (rt *ClaudePersistentRuntime) SetTimeout(timeout time.Duration) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.timeout = timeout
}

// Start creates a tmux session and launches Claude Code in interactive
// mode. The SessionStart hook injects the personality via stdout.
func (rt *ClaudePersistentRuntime) Start(_ context.Context, cfg StartConfig) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	// Kill any stale session from a previous run.
	if rt.tmux.HasSession(rt.session) {
		_ = rt.tmux.KillSession(rt.session)
	}

	model := rt.model
	if cfg.Model != "" {
		model = cfg.Model
	}

	workDir := rt.workDir
	if cfg.WorkDir != "" {
		workDir = cfg.WorkDir
	}

	// Write hook configuration so Claude Code runs prime on startup
	// and drains the inbox on every turn boundary.
	if hookErr := WriteHookSettings(workDir, rt.role); hookErr != nil {
		log.Printf("persistent session %s: failed to write hooks: %v", rt.session, hookErr)
	}

	err := rt.tmux.NewSession(
		rt.session,
		workDir,
		"claude",
		"--model", model,
		"--dangerously-skip-permissions",
	)
	if err != nil {
		return fmt.Errorf("starting persistent session for %s: %w", rt.role, err)
	}

	rt.started = true
	log.Printf("persistent session started: %s (model=%s)", rt.session, model)
	return nil
}

// Send delivers the prompt to the persistent Claude session via tmux
// send-keys. The Stop hook captures Claude's response from the
// transcript and writes it to the outbox signal file. Send watches
// for the signal and returns immediately — no polling timeouts.
//
// Flow:
//  1. send-keys delivers prompt → triggers UserPromptSubmit hook
//  2. Claude processes and responds
//  3. Stop hook fires → reads transcript → writes latest-response.json
//  4. WaitForSignal detects the file → returns response
//
// If the session is dead, Start is called automatically (self-heal).
func (rt *ClaudePersistentRuntime) Send(ctx context.Context, prompt string) (string, error) {
	if healErr := rt.selfHeal(ctx); healErr != nil {
		return "", healErr
	}

	// Create a deadline context so we don't wait forever if something
	// goes wrong. The Stop hook should fire within seconds of Claude
	// finishing — this deadline is a safety net, not a design choice.
	rt.mu.Lock()
	deadline := rt.timeout
	rt.mu.Unlock()

	sendCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	// Send the prompt via tmux send-keys. This triggers a new turn
	// in the Claude Code session, firing the UserPromptSubmit hook.
	if err := rt.tmux.SendKeys(rt.session, prompt); err != nil {
		return "", fmt.Errorf("sending to persistent session %s: %w", rt.role, err)
	}

	// Wait for the Stop hook to write the response signal.
	response, waitErr := rt.inbox.WaitForSignal(sendCtx)
	if waitErr != nil {
		return "", fmt.Errorf("waiting for response from %s: %w", rt.role, waitErr)
	}

	return response, nil
}

func (rt *ClaudePersistentRuntime) selfHeal(ctx context.Context) error {
	if rt.IsAlive() {
		return nil
	}
	log.Printf("persistent session %s is dead — restarting", rt.session)
	if err := rt.Start(ctx, StartConfig{}); err != nil {
		return fmt.Errorf("self-heal failed for %s: %w", rt.role, err)
	}
	return nil
}

// IsAlive checks if the tmux session exists.
func (rt *ClaudePersistentRuntime) IsAlive() bool {
	return rt.tmux.HasSession(rt.session)
}

// Stop kills the tmux session.
func (rt *ClaudePersistentRuntime) Stop() error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.started = false
	if rt.tmux.HasSession(rt.session) {
		return rt.tmux.KillSession(rt.session)
	}
	return nil
}

// Name returns "claude-persistent".
func (rt *ClaudePersistentRuntime) Name() string {
	return "claude-persistent"
}

// SupportsHooks returns true — persistent Claude sessions use hooks
// for message injection and personality loading.
func (rt *ClaudePersistentRuntime) SupportsHooks() bool {
	return true
}

// SessionNameForTest returns the tmux session name for testing.
func (rt *ClaudePersistentRuntime) SessionNameForTest() string {
	return rt.session
}
