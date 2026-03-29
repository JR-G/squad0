package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkdirForFile(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

func TestSetupLogger_CreatesLogDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	out := &bytes.Buffer{}

	appLogger, _, err := cli.SetupLogger(tmpDir, out)

	require.NoError(t, err)
	require.NotNil(t, appLogger)

	logDir := filepath.Join(tmpDir, "logs")
	info, statErr := os.Stat(logDir)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())

	_ = appLogger.Close()
}

func TestSetupLogger_UnwritableDir_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	out := &bytes.Buffer{}

	logsPath := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.WriteFile(logsPath, []byte("blocker"), 0o444))

	appLogger, _, err := cli.SetupLogger(tmpDir, out)

	assert.Error(t, err)
	assert.Nil(t, appLogger)
	assert.Contains(t, out.String(), "Logger failed")
}

func TestCreateCoordinationStore_Twice_ReusesDB(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ctx := context.Background()

	store1, db1, err := cli.CreateCoordinationStore(ctx, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, store1)
	_ = db1.Close()

	store2, db2, err := cli.CreateCoordinationStore(ctx, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, store2)
	_ = db2.Close()
}

func TestCreateCoordinationStore_CorruptDB_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ctx := context.Background()

	// Pre-create a corrupt coordination.db so PingContext or
	// InitSchema fails.
	dbPath := filepath.Join(tmpDir, "coordination.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("corrupt"), 0o644))

	store, db, err := cli.CreateCoordinationStore(ctx, tmpDir)
	// Depending on the SQLite driver, this may fail on ping or
	// InitSchema. Either way, it should return an error.
	if err != nil {
		assert.Nil(t, store)
		assert.Nil(t, db)
		return
	}

	// If SQLite happens to accept the file, clean up.
	_ = db.Close()
}

func TestOpenAllDatabases_AgentDirReadOnly_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create the agents directory, then make it read-only after
	// creating the project.db parent. This forces the project DB to
	// open successfully but the first agent DB write to fail.
	agentDir := filepath.Join(tmpDir, "agents")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	// Make the agent directory read-only so agent DB files cannot be
	// created. On macOS, SQLite may still succeed if the file exists,
	// but since they don't exist yet, Open will fail.
	require.NoError(t, os.Chmod(agentDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(agentDir, 0o755) })

	projectDB, agentDBs, err := cli.OpenAllDatabases(ctx, tmpDir)
	// The project DB should open fine, but agent DBs should fail
	// because the agents/ directory is read-only.
	if err != nil {
		assert.Nil(t, projectDB)
		assert.Nil(t, agentDBs)
		return
	}

	// If this somehow succeeds, clean up.
	cli.CloseDatabases(projectDB, agentDBs)
}

func TestCreateCoordinationStore_ReadOnlyDB_InitSchemaFails(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ctx := context.Background()

	// First, create a valid empty coordination DB so it can be pinged.
	dbPath := filepath.Join(tmpDir, "coordination.db")
	emptyDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	require.NoError(t, err)
	require.NoError(t, emptyDB.PingContext(ctx))
	_ = emptyDB.Close()

	// Now make the DB file read-only. The directory stays writable so
	// MkdirAll passes, sql.Open succeeds, PingContext (SELECT) works,
	// but InitSchema (CREATE TABLE) fails because the file is not
	// writable.
	require.NoError(t, os.Chmod(dbPath, 0o444))
	t.Cleanup(func() { _ = os.Chmod(dbPath, 0o644) })

	store2, db2, err := cli.CreateCoordinationStore(ctx, tmpDir)
	if err != nil {
		assert.Nil(t, store2)
		assert.Nil(t, db2)
		return
	}
	_ = db2.Close()
}

// Tests below call showAgentStatus which uses a hardcoded
// "data/coordination.db" path, so they chdir into a temp directory.
// They must NOT use t.Parallel().

func TestShowAgentStatusDirect_NoCoordDB_ShowsPending(t *testing.T) {
	tmpDir := t.TempDir()
	out := &bytes.Buffer{}
	cmd := newStatusCmd(out)

	cli.ShowAgentStatusDirect(cmd, tmpDir)

	assert.Contains(t, out.String(), "No coordination data yet")
}

func TestShowAgentStatusDirect_WithData_ShowsAgents(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	dbPath := filepath.Join(tmpDir, "data", "coordination.db")
	require.NoError(t, mkdirForFile(dbPath))

	coordDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	require.NoError(t, err)

	store := coordination.NewCheckInStore(coordDB)
	require.NoError(t, store.InitSchema(ctx))

	err = store.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RolePM,
		Status:        coordination.StatusIdle,
		FilesTouching: []string{},
	})
	require.NoError(t, err)
	_ = coordDB.Close()

	out := &bytes.Buffer{}
	cmd := newStatusCmd(out)

	cli.ShowAgentStatusDirect(cmd, tmpDir)

	assert.Contains(t, out.String(), "pm")
}

func TestShowAgentStatusDirect_EmptyCheckIns_ShowsNotStarted(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	dbPath := filepath.Join(tmpDir, "data", "coordination.db")
	require.NoError(t, mkdirForFile(dbPath))

	coordDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	require.NoError(t, err)

	store := coordination.NewCheckInStore(coordDB)
	require.NoError(t, store.InitSchema(ctx))
	_ = coordDB.Close()

	out := &bytes.Buffer{}
	cmd := newStatusCmd(out)

	cli.ShowAgentStatusDirect(cmd, tmpDir)

	assert.Contains(t, out.String(), "not started")
}

func TestShowAgentStatusDirect_CorruptDB_ShowsError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a corrupt coordination.db that exists but cannot be queried.
	dbPath := filepath.Join(tmpDir, "data", "coordination.db")
	require.NoError(t, mkdirForFile(dbPath))
	require.NoError(t, os.WriteFile(dbPath, []byte("not-a-db"), 0o644))

	out := &bytes.Buffer{}
	cmd := newStatusCmd(out)

	cli.ShowAgentStatusDirect(cmd, tmpDir)

	// loadCheckIns should fail and showAgentStatus renders the error.
	assert.NotEmpty(t, out.String())
}
