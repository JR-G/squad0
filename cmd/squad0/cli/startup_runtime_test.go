package cli

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/config"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRuntime_Claude_ReturnsRuntime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rt := buildRuntime("claude", agent.RoleEngineer1, agent.ExecProcessRunner{}, "", dir+"/in", dir+"/out")
	assert.NotNil(t, rt)
	assert.Equal(t, "claude-persistent", rt.Name())
}

func TestBuildRuntime_Codex_ReturnsRuntime(t *testing.T) {
	t.Parallel()

	rt := buildRuntime("codex", agent.RoleEngineer1, agent.ExecProcessRunner{}, "gpt-5-codex", "", "")
	assert.NotNil(t, rt)
	assert.Equal(t, "codex", rt.Name())
}

func TestBuildRuntime_Unknown_ReturnsNil(t *testing.T) {
	t.Parallel()

	rt := buildRuntime("gemini", agent.RolePM, agent.ExecProcessRunner{}, "model", "", "")
	assert.Nil(t, rt)
}

func TestCreateBridgeForRole_DefaultClaude(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config.RuntimeConfig{Default: "claude", Fallback: "codex"}
	bridge := createBridgeForRole(agent.RoleEngineer1, cfg, "gpt-5-codex", dir)
	assert.NotNil(t, bridge)
}

func TestCreateBridgeForRole_WithOverride(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config.RuntimeConfig{
		Default:   "claude",
		Fallback:  "codex",
		Overrides: map[string]string{"engineer-1": "codex"},
	}
	bridge := createBridgeForRole(agent.RoleEngineer1, cfg, "gpt-5-codex", dir)
	assert.NotNil(t, bridge)
	assert.Equal(t, "codex", bridge.Active().Name())
}

func TestCreateBridgeForRole_UnknownDefault_ReturnsNil(t *testing.T) {
	t.Parallel()

	cfg := config.RuntimeConfig{Default: "invalid"}
	bridge := createBridgeForRole(agent.RolePM, cfg, "", "")
	assert.Nil(t, bridge)
}

func TestCreateBridgeForRole_SameFallbackAsActive_NilFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config.RuntimeConfig{Default: "codex", Fallback: "codex"}
	bridge := createBridgeForRole(agent.RoleEngineer2, cfg, "gpt-5-codex", dir)
	assert.NotNil(t, bridge)
}

func TestWireRouting_ReturnsClassifier(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	classifier := wireRouting(cfg)
	assert.NotNil(t, classifier)
}

func TestWireSpecialisation_ValidDB_ReturnsStore(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := wireSpecialisation(context.Background(), db)
	assert.NotNil(t, store)
}

func TestWireSpecialisation_ClosedDB_ReturnsNil(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	_ = db.Close()

	store := wireSpecialisation(context.Background(), db)
	assert.Nil(t, store)
}

func TestWireBudget_ReturnsLedger(t *testing.T) {
	t.Parallel()

	ledger := wireBudget(config.BudgetConfig{MaxTokensPerTicket: 100000})
	assert.NotNil(t, ledger)
}

func TestWireSituations_ReturnsBothStores(t *testing.T) {
	t.Parallel()

	queue, tracker := wireSituations()
	assert.NotNil(t, queue)
	assert.NotNil(t, tracker)
}
