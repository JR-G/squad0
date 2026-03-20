package slack

import (
	"context"
	"sync"
	"time"
)

// RateLimiter enforces a minimum spacing between Slack API calls to stay
// within rate limits. Messages are queued and drained with the configured
// delay between each send.
type RateLimiter struct {
	minSpacing time.Duration
	mu         sync.Mutex
	lastSend   time.Time
}

// NewRateLimiter creates a RateLimiter with the given minimum spacing
// between messages.
func NewRateLimiter(minSpacing time.Duration) *RateLimiter {
	return &RateLimiter{
		minSpacing: minSpacing,
	}
}

// Wait blocks until enough time has elapsed since the last send. Returns
// an error if the context is cancelled while waiting.
func (limiter *RateLimiter) Wait(ctx context.Context) error {
	limiter.mu.Lock()
	elapsed := time.Since(limiter.lastSend)
	remaining := limiter.minSpacing - elapsed
	limiter.mu.Unlock()

	if remaining <= 0 {
		limiter.markSent()
		return nil
	}

	select {
	case <-time.After(remaining):
		limiter.markSent()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (limiter *RateLimiter) markSent() {
	limiter.mu.Lock()
	limiter.lastSend = time.Now()
	limiter.mu.Unlock()
}
