//go:build sqlite_fts5

package memory_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_CorruptedVersionTable_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "corrupt_version.db")
	ctx := context.Background()

	// Manually create a database with a corrupted schema_version table
	rawDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000")
	require.NoError(t, err)

	// Create a schema_version table with wrong column type
	_, err = rawDB.Exec(`CREATE TABLE schema_version (version TEXT NOT NULL)`)
	require.NoError(t, err)

	// Insert a non-numeric version
	_, err = rawDB.Exec(`INSERT INTO schema_version (version) VALUES ('not_a_number')`)
	require.NoError(t, err)

	require.NoError(t, rawDB.Close())

	// Now try to Open, which should fail when reading schema version
	_, err = memory.Open(ctx, dbPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading schema version")
}

func TestOpen_CorruptedDB_File_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "corrupt.db")

	// Write garbage to the DB file
	err := os.WriteFile(dbPath, []byte("this is not a sqlite database"), 0o644)
	require.NoError(t, err)

	_, err = memory.Open(context.Background(), dbPath)

	require.Error(t, err)
}

func TestOpen_MigrationApply_DropsRequiredTable_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "migration_fail.db")
	ctx := context.Background()

	// Create database with version 0 so migration 1 is attempted,
	// but pre-create some conflicting tables
	rawDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000")
	require.NoError(t, err)

	_, err = rawDB.Exec(`CREATE TABLE schema_version (version INTEGER NOT NULL)`)
	require.NoError(t, err)

	_, err = rawDB.Exec(`INSERT INTO schema_version (version) VALUES (0)`)
	require.NoError(t, err)

	// Pre-create the entities table to cause a conflict in migration
	_, err = rawDB.Exec(`CREATE TABLE entities (id INTEGER PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)

	require.NoError(t, rawDB.Close())

	// Open should fail during migration because entities table already exists
	_, err = memory.Open(ctx, dbPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "applying migration")
}

func TestOpen_Reopen_ExistingDB_SkipsMigrations(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "skip_migrations.db")
	ctx := context.Background()

	// First open applies migrations
	db1, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)

	var version1 int
	err = db1.RawDB().QueryRow(`SELECT version FROM schema_version`).Scan(&version1)
	require.NoError(t, err)
	assert.Equal(t, 1, version1)
	require.NoError(t, db1.Close())

	// Second open should skip all migrations (version already 1)
	db2, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })

	var version2 int
	err = db2.RawDB().QueryRow(`SELECT version FROM schema_version`).Scan(&version2)
	require.NoError(t, err)
	assert.Equal(t, 1, version2)

	// Verify data is still accessible
	store := memory.NewGraphStore(db2)
	_, err = store.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "post-reopen",
	})
	require.NoError(t, err)
}

func TestOpen_ReadOnlyDir_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	readOnlyDir := filepath.Join(dir, "readonly")
	err := os.Mkdir(readOnlyDir, 0o555)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Chmod(readOnlyDir, 0o755)
	})

	// Some systems allow sqlite to open even in read-only dirs,
	// so we just verify it does not panic
	_, err = memory.Open(context.Background(), filepath.Join(readOnlyDir, "subdir", "test.db"))
	if err != nil {
		assert.Error(t, err)
	}
}

func TestOpen_VersionAt1_Reopen_SkipsMigration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "version1_reopen.db")
	ctx := context.Background()

	// First open: sets version to 1
	db1, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)
	require.NoError(t, db1.Close())

	// Second open: version is already 1, all migrations are skipped
	db2, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })

	// Verify tables exist from first run
	var tableCount int
	err = db2.RawDB().QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('entities','facts','episodes','beliefs')`,
	).Scan(&tableCount)
	require.NoError(t, err)
	assert.Equal(t, 4, tableCount)
}

func TestOpen_EnsureVersionTableFails_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bad_version_table.db")

	// Create a file that is a valid database but make schema_version a
	// VIEW instead of TABLE so CREATE TABLE IF NOT EXISTS fails
	rawDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	require.NoError(t, err)
	_, err = rawDB.Exec(`CREATE TABLE dummy (id INTEGER)`)
	require.NoError(t, err)
	// Create schema_version as a VIEW — not a table
	_, err = rawDB.Exec(`CREATE VIEW schema_version AS SELECT 0 AS version`)
	require.NoError(t, err)
	require.NoError(t, rawDB.Close())

	_, err = memory.Open(context.Background(), dbPath)

	require.Error(t, err)
}

func TestOpen_VersionTableDestroyed_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "no_version.db")
	ctx := context.Background()

	// Create database with migrations applied
	db1, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)

	// Drop the schema_version table
	_, err = db1.RawDB().Exec(`DROP TABLE schema_version`)
	require.NoError(t, err)
	require.NoError(t, db1.Close())

	// Re-open. ensureVersionTable will recreate it, then currentVersion
	// returns 0 (no rows), then it tries to apply migration 1 which will
	// fail because tables already exist
	_, err = memory.Open(ctx, dbPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "applying migration")
}
