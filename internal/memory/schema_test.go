package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestDB(t *testing.T) *memory.DB {
	t.Helper()
	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpen_InMemory_Succeeds(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	assert.NotNil(t, db.RawDB())
}

func TestOpen_InvalidPath_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := memory.Open(context.Background(), "/nonexistent/dir/test.db")
	require.Error(t, err)
}

func TestOpen_CreatesAllTables(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	expectedTables := []string{"entities", "relationships", "facts", "episodes", "beliefs", "schema_version"}

	for _, table := range expectedTables {
		var name string
		err := db.RawDB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		require.NoError(t, err, "table %s should exist", table)
		assert.Equal(t, table, name)
	}
}

func TestOpen_CreatesFTSTables(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	expectedTables := []string{"facts_fts", "episodes_fts", "beliefs_fts"}

	for _, table := range expectedTables {
		var name string
		err := db.RawDB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		require.NoError(t, err, "FTS table %s should exist", table)
	}
}

func TestOpen_SetsSchemaVersion(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	var version int
	err := db.RawDB().QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, 1, version)
}

func TestOpen_IdempotentMigrations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	_ = db.Close()
}

func TestClose_ClosesConnection(t *testing.T) {
	t.Parallel()

	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)

	err = db.Close()
	require.NoError(t, err)

	err = db.RawDB().Ping()
	assert.Error(t, err)
}
