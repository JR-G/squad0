package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStatusCmd(out *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	return cmd
}

func TestShowEmptyAgentList_ListsAllRoles(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := newStatusCmd(out)

	cli.ShowEmptyAgentList(cmd)

	result := out.String()
	for _, role := range agent.AllRoles() {
		assert.Contains(t, result, string(role))
	}
	assert.Contains(t, result, "not started")
}

func TestLoadCheckIns_ValidDB_ReturnsCheckIns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "coordination.db")

	coordDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	require.NoError(t, err)

	store := coordination.NewCheckInStore(coordDB)
	err = store.InitSchema(ctx)
	require.NoError(t, err)

	err = store.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RoleEngineer1,
		Status:        coordination.StatusWorking,
		Ticket:        "SQ-100",
		FilesTouching: []string{"main.go"},
	})
	require.NoError(t, err)
	_ = coordDB.Close()

	checkIns, loadErr := cli.LoadCheckIns(ctx, dbPath)

	require.NoError(t, loadErr)
	require.Len(t, checkIns, 1)
	assert.Equal(t, agent.RoleEngineer1, checkIns[0].Agent)
	assert.Equal(t, "SQ-100", checkIns[0].Ticket)
}

func TestLoadCheckIns_EmptyDB_ReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "coordination.db")

	coordDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	require.NoError(t, err)

	store := coordination.NewCheckInStore(coordDB)
	err = store.InitSchema(ctx)
	require.NoError(t, err)
	_ = coordDB.Close()

	checkIns, loadErr := cli.LoadCheckIns(ctx, dbPath)

	require.NoError(t, loadErr)
	assert.Empty(t, checkIns)
}

func TestLoadCheckIns_InvalidPath_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	checkIns, err := cli.LoadCheckIns(ctx, "/nonexistent/db.sqlite")

	assert.Error(t, err)
	assert.Nil(t, checkIns)
}

func TestShowStatus_PrintsBanner(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := newStatusCmd(out)

	cli.ShowStatus(cmd)

	assert.Contains(t, out.String(), "Squad0")
}

func TestShowAgentStatus_NoCoordDB_ShowsPendingMessage(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := newStatusCmd(out)

	cli.ShowAgentStatusWithPath(cmd, "/nonexistent/coordination.db")

	assert.Contains(t, out.String(), "No coordination data yet")
}

func TestShowAgentStatus_WithCheckIns_ShowsAgents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "coordination.db")

	coordDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	require.NoError(t, err)

	store := coordination.NewCheckInStore(coordDB)
	err = store.InitSchema(ctx)
	require.NoError(t, err)

	err = store.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RolePM,
		Status:        coordination.StatusIdle,
		FilesTouching: []string{},
	})
	require.NoError(t, err)
	_ = coordDB.Close()

	out := &bytes.Buffer{}
	cmd := newStatusCmd(out)

	cli.ShowAgentStatusWithPath(cmd, dbPath)

	assert.Contains(t, out.String(), "pm")
}

func TestShowAgentStatus_EmptyCheckIns_ShowsEmptyList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "coordination.db")

	coordDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	require.NoError(t, err)

	store := coordination.NewCheckInStore(coordDB)
	err = store.InitSchema(ctx)
	require.NoError(t, err)
	_ = coordDB.Close()

	out := &bytes.Buffer{}
	cmd := newStatusCmd(out)

	cli.ShowAgentStatusWithPath(cmd, dbPath)

	assert.Contains(t, out.String(), "not started")
}
