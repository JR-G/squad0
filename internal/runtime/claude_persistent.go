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
	seq     int64
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
// send-keys and waits for the response in the outbox. The prompt is
// sent as a user message which triggers the UserPromptSubmit hook
// (injecting any queued context). The prompt itself instructs Claude
// to write the response to the outbox file.
//
// If the session is dead, Start is called automatically (self-heal).
func (rt *ClaudePersistentRuntime) Send(ctx context.Context, prompt string) (string, error) {
	rt.mu.Lock()
	timeout := rt.timeout
	rt.mu.Unlock()

	if healErr := rt.selfHeal(ctx); healErr != nil {
		return "", healErr
	}

	id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), rt.sendSeq())
	outboxPath := rt.inbox.OutboxDir() + "/" + id + "-response.json"

	// Build the message: the actual prompt + an instruction to write
	// the response to the outbox file so we can collect it.
	wrappedPrompt := prompt + fmt.Sprintf(
		"\n\nAfter responding, write ONLY your response text to this file (no explanation, just the response): %s",
		outboxPath,
	)

	// Send via tmux — this triggers the UserPromptSubmit hook and
	// delivers the prompt as a user message to Claude Code.
	if err := rt.tmux.SendKeys(rt.session, wrappedPrompt); err != nil {
		return "", fmt.Errorf("sending to persistent session %s: %w", rt.role, err)
	}

	response, waitErr := rt.inbox.WaitForResponse(id, timeout)
	if waitErr != nil {
		return "", fmt.Errorf("waiting for response from %s: %w", rt.role, waitErr)
	}

	return response, nil
}

func (rt *ClaudePersistentRuntime) sendSeq() int64 {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.seq++
	return rt.seq
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
