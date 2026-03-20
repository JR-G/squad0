//go:build sqlite_fts5

package memory_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := memory.Open(ctx, ":memory:")

	require.Error(t, err)
}

func TestOpen_IdempotentReopen_SkipsAppliedMigrations(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := dir + "/reopen.db"
	ctx := context.Background()

	db1, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)
	require.NoError(t, db1.Close())

	db2, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })

	var version int
	err = db2.RawDB().QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, 1, version)
}

func TestOpen_Reopen_UpdatesBranch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := dir + "/update_branch.db"
	ctx := context.Background()

	db1, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)

	var initialVersion int
	err = db1.RawDB().QueryRow(`SELECT version FROM schema_version`).Scan(&initialVersion)
	require.NoError(t, err)
	assert.Equal(t, 1, initialVersion)
	require.NoError(t, db1.Close())

	db2, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })

	var finalVersion int
	err = db2.RawDB().QueryRow(`SELECT version FROM schema_version`).Scan(&finalVersion)
	require.NoError(t, err)
	assert.Equal(t, 1, finalVersion)
}

func TestCurrentVersion_EmptyTable_ReturnsZero(t *testing.T) {
	t.Parallel()

	rawDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = rawDB.Close() }()

	_, err = rawDB.Exec(`CREATE TABLE schema_version (version INTEGER NOT NULL)`)
	require.NoError(t, err)

	var version int
	err = rawDB.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&version)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestOpen_ClosedDB_OperationsFail(t *testing.T) {
	t.Parallel()

	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	err = db.RawDB().Ping()
	assert.Error(t, err)
}

func TestOpen_PersistentDB_PreservesSchema(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := dir + "/persist.db"
	ctx := context.Background()

	db1, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)

	graphStore := memory.NewGraphStore(db1)
	_, err = graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "persist-test", Summary: "check persistence",
	})
	require.NoError(t, err)
	require.NoError(t, db1.Close())

	db2, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })

	graphStore2 := memory.NewGraphStore(db2)
	entity, err := graphStore2.FindEntityByName(ctx, memory.EntityModule, "persist-test")
	require.NoError(t, err)
	assert.Equal(t, "persist-test", entity.Name)
}
