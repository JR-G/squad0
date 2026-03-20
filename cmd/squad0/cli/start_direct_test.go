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

// These tests call production functions with hardcoded paths by
// changing the working directory. They must NOT use t.Parallel().

func TestSetupLoggerDirect_CreatesLogger(t *testing.T) {
	tmpDir := t.TempDir()

	appLogger, err := cli.SetupLoggerDirect(tmpDir)

	require.NoError(t, err)
	require.NotNil(t, appLogger)
	_ = appLogger.Close()
}

func TestOpenAllDatabasesDirect_OpensAllDBs(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	projectDB, agentDBs, err := cli.OpenAllDatabasesDirect(ctx, tmpDir)

	require.NoError(t, err)
	require.NotNil(t, projectDB)
	assert.Len(t, agentDBs, len(agent.AllRoles()))

	cli.CloseDatabases(projectDB, agentDBs)
}

func TestCreateCoordinationStoreDirect_ReturnsStore(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	store, coordDB, err := cli.CreateCoordinationStoreDirect(ctx, tmpDir)

	require.NoError(t, err)
	require.NotNil(t, store)
	require.NotNil(t, coordDB)
	_ = coordDB.Close()
}

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

func TestSetupLoggerDirect_UnwritableDir_ReturnsError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a data/logs path that is a file (not a directory) so
	// MkdirAll inside logging.NewLogger fails.
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	logsPath := filepath.Join(dataDir, "logs")
	require.NoError(t, os.WriteFile(logsPath, []byte("blocker"), 0o444))

	appLogger, err := cli.SetupLoggerDirect(tmpDir)

	assert.Error(t, err)
	assert.Nil(t, appLogger)
}

func TestCreateCoordinationStoreDirect_Twice_ReusesDB(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	store1, db1, err := cli.CreateCoordinationStoreDirect(ctx, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, store1)
	_ = db1.Close()

	store2, db2, err := cli.CreateCoordinationStoreDirect(ctx, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, store2)
	_ = db2.Close()
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
