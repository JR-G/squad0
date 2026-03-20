package slack_test

import (
	"context"
	"testing"
	"time"

	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimiter_Wait_FirstCall_ReturnsImmediately(t *testing.T) {
	t.Parallel()

	limiter := slack.NewRateLimiter(2 * time.Second)

	start := time.Now()
	err := limiter.Wait(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 100*time.Millisecond)
}

func TestRateLimiter_Wait_EnforcesSpacing(t *testing.T) {
	t.Parallel()

	spacing := 100 * time.Millisecond
	limiter := slack.NewRateLimiter(spacing)

	err := limiter.Wait(context.Background())
	require.NoError(t, err)

	start := time.Now()
	err = limiter.Wait(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
}

func TestRateLimiter_Wait_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	limiter := slack.NewRateLimiter(10 * time.Second)

	err := limiter.Wait(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = limiter.Wait(ctx)

	assert.Error(t, err)
}

func TestRateLimiter_Wait_AfterSpacingElapsed_ReturnsImmediately(t *testing.T) {
	t.Parallel()

	spacing := 50 * time.Millisecond
	limiter := slack.NewRateLimiter(spacing)

	err := limiter.Wait(context.Background())
	require.NoError(t, err)

	time.Sleep(spacing + 10*time.Millisecond)

	start := time.Now()
	err = limiter.Wait(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 50*time.Millisecond)
}
