package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupWorkingStore(t *testing.T) (*memory.WorkingStore, context.Context) {
	t.Helper()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return memory.NewWorkingStore(db), ctx
}

func TestWorkingStore_SetThenGet_RoundTrips(t *testing.T) {
	t.Parallel()
	store, ctx := setupWorkingStore(t)

	require.NoError(t, store.Set(ctx, "sess-1", "current_plan", "fix the auth bug"))

	got, err := store.Get(ctx, "sess-1", "current_plan")
	require.NoError(t, err)
	assert.Equal(t, "fix the auth bug", got)
}

func TestWorkingStore_Set_OverwritesExisting(t *testing.T) {
	t.Parallel()
	store, ctx := setupWorkingStore(t)

	require.NoError(t, store.Set(ctx, "sess-1", "step", "1: explore"))
	require.NoError(t, store.Set(ctx, "sess-1", "step", "2: implement"))

	got, err := store.Get(ctx, "sess-1", "step")
	require.NoError(t, err)
	assert.Equal(t, "2: implement", got)
}

func TestWorkingStore_Get_MissingKey_ReturnsErrNoEntry(t *testing.T) {
	t.Parallel()
	store, ctx := setupWorkingStore(t)

	_, err := store.Get(ctx, "sess-1", "never_set")
	assert.True(t, errors.Is(err, memory.ErrNoEntry))
}

func TestWorkingStore_Set_EmptyValue_AllowedAndDistinctFromMissing(t *testing.T) {
	t.Parallel()
	store, ctx := setupWorkingStore(t)

	require.NoError(t, store.Set(ctx, "sess-1", "scratch", ""))

	got, err := store.Get(ctx, "sess-1", "scratch")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestWorkingStore_Set_EmptySessionRejected(t *testing.T) {
	t.Parallel()
	store, ctx := setupWorkingStore(t)

	err := store.Set(ctx, "", "key", "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session")
}

func TestWorkingStore_Set_EmptyKeyRejected(t *testing.T) {
	t.Parallel()
	store, ctx := setupWorkingStore(t)

	err := store.Set(ctx, "sess-1", "", "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key")
}

func TestWorkingStore_Keys_ListsInsertionOrder(t *testing.T) {
	t.Parallel()
	store, ctx := setupWorkingStore(t)

	require.NoError(t, store.Set(ctx, "sess-1", "first", "1"))
	require.NoError(t, store.Set(ctx, "sess-1", "second", "2"))
	require.NoError(t, store.Set(ctx, "sess-1", "third", "3"))

	keys, err := store.Keys(ctx, "sess-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "second", "third"}, keys)
}

func TestWorkingStore_Keys_OtherSession_NotIncluded(t *testing.T) {
	t.Parallel()
	store, ctx := setupWorkingStore(t)

	require.NoError(t, store.Set(ctx, "sess-1", "mine", "x"))
	require.NoError(t, store.Set(ctx, "sess-2", "theirs", "y"))

	keys, err := store.Keys(ctx, "sess-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"mine"}, keys)
}

func TestWorkingStore_AllOps_ClosedDB_ReturnError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)

	store := memory.NewWorkingStore(db)
	require.NoError(t, store.Set(ctx, "sess", "k", "v"))
	require.NoError(t, db.Close())

	assert.Error(t, store.Set(ctx, "sess", "k2", "v"))
	_, getErr := store.Get(ctx, "sess", "k")
	assert.Error(t, getErr)
	_, keysErr := store.Keys(ctx, "sess")
	assert.Error(t, keysErr)
	assert.Error(t, store.Clear(ctx, "sess"))
}

func TestWorkingStore_Clear_RemovesOnlyTargetSession(t *testing.T) {
	t.Parallel()
	store, ctx := setupWorkingStore(t)

	require.NoError(t, store.Set(ctx, "sess-1", "a", "1"))
	require.NoError(t, store.Set(ctx, "sess-2", "b", "2"))

	require.NoError(t, store.Clear(ctx, "sess-1"))

	_, err := store.Get(ctx, "sess-1", "a")
	assert.True(t, errors.Is(err, memory.ErrNoEntry))

	got, err := store.Get(ctx, "sess-2", "b")
	require.NoError(t, err)
	assert.Equal(t, "2", got)
}
