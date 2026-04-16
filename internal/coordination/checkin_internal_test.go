package coordination

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openInMemoryNoSchema(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestIsSQLiteBusy_NilError_ReturnsFalse(t *testing.T) {
	t.Parallel()

	assert.False(t, isSQLiteBusy(nil))
}

func TestIsSQLiteBusy_ContextCanceled_ReturnsFalse(t *testing.T) {
	t.Parallel()

	assert.False(t, isSQLiteBusy(context.Canceled))
	assert.False(t, isSQLiteBusy(context.DeadlineExceeded))
}

func TestIsSQLiteBusy_DatabaseLocked_ReturnsTrue(t *testing.T) {
	t.Parallel()

	assert.True(t, isSQLiteBusy(errors.New("database is locked")))
	assert.True(t, isSQLiteBusy(errors.New("DATABASE TABLE IS LOCKED here")))
	assert.True(t, isSQLiteBusy(errors.New("SQLITE_BUSY: cannot proceed")))
}

func TestIsSQLiteBusy_OtherError_ReturnsFalse(t *testing.T) {
	t.Parallel()

	assert.False(t, isSQLiteBusy(errors.New("disk full")))
	assert.False(t, isSQLiteBusy(errors.New("no such table")))
}

func TestRetryOnBusy_SucceedsFirstTry_NoRetries(t *testing.T) {
	t.Parallel()

	calls := 0
	err := retryOnBusy(context.Background(), func(_ context.Context) error {
		calls++
		return nil
	}, 3, func(int) time.Duration { return 0 })

	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestRetryOnBusy_BusyThenSucceed_RetriesUntilOK(t *testing.T) {
	t.Parallel()

	calls := 0
	err := retryOnBusy(context.Background(), func(_ context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("database is locked")
		}
		return nil
	}, 5, func(int) time.Duration { return 0 })

	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestRetryOnBusy_ExhaustedRetries_ReturnsWrappedError(t *testing.T) {
	t.Parallel()

	calls := 0
	err := retryOnBusy(context.Background(), func(_ context.Context) error {
		calls++
		return errors.New("database is locked")
	}, 3, func(int) time.Duration { return 0 })

	require.Error(t, err)
	assert.Contains(t, err.Error(), "after 3 attempts")
	assert.Equal(t, 3, calls)
}

func TestRetryOnBusy_NonRetryableError_ReturnsImmediately(t *testing.T) {
	t.Parallel()

	calls := 0
	err := retryOnBusy(context.Background(), func(_ context.Context) error {
		calls++
		return errors.New("disk full")
	}, 3, func(int) time.Duration { return 0 })

	require.Error(t, err)
	assert.Equal(t, 1, calls)
	assert.NotContains(t, err.Error(), "after")
}

func TestRetryOnBusy_CancelledContext_ReturnsCtxErr(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := retryOnBusy(ctx, func(_ context.Context) error {
		return errors.New("database is locked")
	}, 3, func(int) time.Duration { return 0 })

	require.ErrorIs(t, err, context.Canceled)
}

func TestRetryOnBusy_CtxCancelledDuringBackoff_ReturnsCtxErr(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	err := retryOnBusy(ctx, func(_ context.Context) error {
		calls++
		if calls == 1 {
			cancel() // Cancel after first attempt so the backoff select hits ctx.Done.
		}
		return errors.New("database is locked")
	}, 5, func(int) time.Duration { return 100 * time.Millisecond })

	require.ErrorIs(t, err, context.Canceled)
}

func TestUpsert_NonRetryableError_ReturnsImmediately(t *testing.T) {
	t.Parallel()

	// Open a DB without InitSchema so the INSERT fails with
	// "no such table" — a non-retryable error, exercising the
	// !isSQLiteBusy short-circuit path inside Upsert.
	db := openInMemoryNoSchema(t)
	store := NewCheckInStore(db)

	err := store.Upsert(context.Background(), CheckIn{
		Agent: "engineer-1", Status: "idle", FilesTouching: []string{},
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upserting checkin")
}
