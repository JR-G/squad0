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

// Send writes a prompt to the inbox and waits for a response in the
// outbox. The UserPromptSubmit hook drains the inbox and injects
// messages into the running Claude session.
//
// If the session is dead, Start is called automatically (self-heal).
// If the response times out, returns an error — the bridge should
// fall back to a fresh process.
func (rt *ClaudePersistentRuntime) Send(ctx context.Context, prompt string) (string, error) {
	rt.mu.Lock()
	timeout := rt.timeout
	rt.mu.Unlock()

	// Self-heal: restart if the session died.
	if healErr := rt.selfHeal(ctx); healErr != nil {
		return "", healErr
	}

	id, err := rt.inbox.Enqueue(prompt)
	if err != nil {
		return "", fmt.Errorf("enqueueing prompt for %s: %w", rt.role, err)
	}

	response, waitErr := rt.inbox.WaitForResponse(id, timeout)
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
