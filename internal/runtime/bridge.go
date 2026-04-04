package runtime

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/JR-G/squad0/internal/agent"
)

// SessionBridge wraps the active runtime for a single agent. Handles
// transparent fallback on rate limits. The orchestrator calls Chat()
// for conversations and Execute() for implementation sessions.
type SessionBridge struct {
	mu       sync.Mutex
	role     agent.Role
	active   Runtime
	fallback Runtime
	swapped  bool
}

// NewSessionBridge creates a bridge with the given active and fallback
// runtimes. Both runtimes are peers — either can be primary.
func NewSessionBridge(role agent.Role, active, fallback Runtime) *SessionBridge {
	return &SessionBridge{
		role:     role,
		active:   active,
		fallback: fallback,
	}
}

// Chat sends a prompt via the active runtime and returns the response.
// If the active runtime hits a rate limit, swaps to the fallback
// transparently. If fallback is nil, returns the error.
func (bridge *SessionBridge) Chat(ctx context.Context, prompt string) (string, error) {
	bridge.mu.Lock()
	active := bridge.active
	fallback := bridge.fallback
	bridge.mu.Unlock()

	response, err := active.Send(ctx, prompt)
	if err == nil {
		return response, nil
	}

	// Check if rate limited — swap to fallback if available.
	if !agent.IsRateLimited(response, err) {
		return response, fmt.Errorf("chat via %s: %w", active.Name(), err)
	}

	if fallback == nil {
		return response, fmt.Errorf("rate limited on %s with no fallback: %w", active.Name(), err)
	}

	log.Printf("bridge: %s rate limited on %s, swapping to %s", bridge.role, active.Name(), fallback.Name())
	bridge.markSwapped()

	fallbackResponse, fallbackErr := fallback.Send(ctx, prompt)
	if fallbackErr != nil {
		return fallbackResponse, fmt.Errorf("fallback %s also failed: %w", fallback.Name(), fallbackErr)
	}

	return fallbackResponse, nil
}

// IsSwapped returns true if the bridge has swapped to the fallback
// runtime due to rate limiting.
func (bridge *SessionBridge) IsSwapped() bool {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	return bridge.swapped
}

// ResetSwap switches back to the primary runtime. Called when rate
// limits have cleared.
func (bridge *SessionBridge) ResetSwap() {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.swapped = false
}

func (bridge *SessionBridge) markSwapped() {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.swapped = true
}

// Active returns the currently active runtime.
func (bridge *SessionBridge) Active() Runtime {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	return bridge.active
}

// Stop stops both runtimes.
func (bridge *SessionBridge) Stop() error {
	activeErr := bridge.active.Stop()
	fallbackErr := bridge.stopFallback()
	if activeErr != nil {
		return activeErr
	}
	return fallbackErr
}

func (bridge *SessionBridge) stopFallback() error {
	if bridge.fallback == nil {
		return nil
	}
	return bridge.fallback.Stop()
}

// Role returns the bridge's agent role.
func (bridge *SessionBridge) Role() agent.Role {
	return bridge.role
}
