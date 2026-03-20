package cli_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/config"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildModelMap_MapsAllRoles(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	result := cli.BuildModelMap(cfg)

	assert.Equal(t, cfg.Agents.Models.PM, result[agent.RolePM])
	assert.Equal(t, cfg.Agents.Models.TechLead, result[agent.RoleTechLead])
	assert.Equal(t, cfg.Agents.Models.Engineer, result[agent.RoleEngineer1])
	assert.Equal(t, cfg.Agents.Models.Engineer, result[agent.RoleEngineer2])
	assert.Equal(t, cfg.Agents.Models.Engineer, result[agent.RoleEngineer3])
	assert.Equal(t, cfg.Agents.Models.Reviewer, result[agent.RoleReviewer])
	assert.Equal(t, cfg.Agents.Models.Designer, result[agent.RoleDesigner])
}

func TestBuildModelMap_CustomModels(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Agents.Models.PM = "custom-pm"
	cfg.Agents.Models.Engineer = "custom-eng"

	result := cli.BuildModelMap(cfg)

	assert.Equal(t, "custom-pm", result[agent.RolePM])
	assert.Equal(t, "custom-eng", result[agent.RoleEngineer1])
	assert.Equal(t, "custom-eng", result[agent.RoleEngineer2])
	assert.Equal(t, "custom-eng", result[agent.RoleEngineer3])
}

func TestParseCronToInterval_ReturnsDay(t *testing.T) {
	t.Parallel()

	result := cli.ParseCronToInterval("0 9 * * *")

	assert.Equal(t, 24*time.Hour, result)
}

func TestParseCronToInterval_IgnoresInput(t *testing.T) {
	t.Parallel()

	result := cli.ParseCronToInterval("anything")

	assert.Equal(t, 24*time.Hour, result)
}

func TestSetupLogger_ValidPath_ReturnsLogger(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	out := &bytes.Buffer{}

	appLogger, err := cli.SetupLogger(dataDir, out)

	require.NoError(t, err)
	require.NotNil(t, appLogger)
	assert.Contains(t, out.String(), "Logger started")
	_ = appLogger.Close()
}

func TestSetupLogger_InvalidPath_ReturnsError(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}

	appLogger, err := cli.SetupLogger("/dev/null/impossible/path", out)

	assert.Error(t, err)
	assert.Nil(t, appLogger)
	assert.Contains(t, out.String(), "Logger failed")
}

func TestOpenAllDatabases_ValidPath_OpensAllDBs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	projectDB, agentDBs, err := cli.OpenAllDatabases(ctx, tmpDir)

	require.NoError(t, err)
	require.NotNil(t, projectDB)
	assert.Len(t, agentDBs, len(agent.AllRoles()))

	for _, role := range agent.AllRoles() {
		assert.Contains(t, agentDBs, role)
	}

	cli.CloseDatabases(projectDB, agentDBs)
}

func TestOpenAllDatabases_InvalidPath_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	projectDB, agentDBs, err := cli.OpenAllDatabases(ctx, "/dev/null/nope")

	assert.Error(t, err)
	assert.Nil(t, projectDB)
	assert.Nil(t, agentDBs)
}

func TestCloseDatabases_NilProject_NoPanic(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		cli.CloseDatabases(nil, nil)
	})
}

func TestCloseDatabases_WithDBs_ClosesAll(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	projectDB, agentDBs, err := cli.OpenAllDatabases(ctx, tmpDir)
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		cli.CloseDatabases(projectDB, agentDBs)
	})
}

func TestCreateAgents_ValidDBs_CreatesAll(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	projectDB, agentDBs, err := cli.OpenAllDatabases(ctx, tmpDir)
	require.NoError(t, err)
	defer cli.CloseDatabases(projectDB, agentDBs)

	embedder := memory.NewEmbedder("http://localhost:11434", "nomic-embed-text")
	modelMap := cli.BuildModelMap(config.DefaultConfig())

	agents, err := cli.CreateAgents(agentDBs, embedder, modelMap, t.TempDir())

	require.NoError(t, err)
	assert.Len(t, agents, len(agent.AllRoles()))

	for _, role := range agent.AllRoles() {
		assert.Contains(t, agents, role)
		assert.Equal(t, role, agents[role].Role())
	}
}

func TestCreateAgents_MissingDB_ReturnsError(t *testing.T) {
	t.Parallel()

	agentDBs := map[agent.Role]*memory.DB{}
	embedder := memory.NewEmbedder("http://localhost:11434", "nomic-embed-text")
	modelMap := cli.BuildModelMap(config.DefaultConfig())

	agents, err := cli.CreateAgents(agentDBs, embedder, modelMap, t.TempDir())

	assert.Error(t, err)
	assert.Nil(t, agents)
	assert.Contains(t, err.Error(), "no database for role")
}

func TestCreateCoordinationStore_ValidPath_ReturnsStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	store, db, err := cli.CreateCoordinationStore(ctx, tmpDir)

	require.NoError(t, err)
	require.NotNil(t, store)
	require.NotNil(t, db)
	_ = db.Close()
}

func TestCreateCoordinationStore_InvalidPath_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, db, err := cli.CreateCoordinationStore(ctx, "/dev/null/nope")

	assert.Error(t, err)
	assert.Nil(t, store)
	assert.Nil(t, db)
}

func TestCreateHealthMonitor_ReturnsMonitor(t *testing.T) {
	t.Parallel()

	monitor := cli.CreateHealthMonitor()

	require.NotNil(t, monitor)

	for _, role := range agent.AllRoles() {
		health, err := monitor.GetHealth(role)
		require.NoError(t, err)
		assert.Equal(t, role, health.Role)
	}
}

func TestBuildSingleAgent_ReturnsAgent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	agentDB, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)
	defer func() { _ = agentDB.Close() }()

	embedder := memory.NewEmbedder("http://localhost:11434", "nomic-embed-text")
	modelMap := map[agent.Role]string{
		agent.RoleEngineer1: "claude-sonnet-4-6",
	}
	loader := agent.NewPersonalityLoader(t.TempDir())

	result := cli.BuildSingleAgent(agent.RoleEngineer1, agentDB, embedder, modelMap, loader)

	require.NotNil(t, result)
	assert.Equal(t, agent.RoleEngineer1, result.Role())
}

func TestCreateCoordinationStore_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	store, db, err := cli.CreateCoordinationStore(ctx, tmpDir)

	assert.Error(t, err)
	assert.Nil(t, store)
	assert.Nil(t, db)
}

func TestOpenAllDatabases_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	projectDB, agentDBs, err := cli.OpenAllDatabases(ctx, tmpDir)

	assert.Error(t, err)
	assert.Nil(t, projectDB)
	assert.Nil(t, agentDBs)
}
