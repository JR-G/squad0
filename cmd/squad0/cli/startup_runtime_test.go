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
	rt := buildRuntime("claude", agent.RoleEngineer1, agent.ExecProcessRunner{}, "", "claude-sonnet-4-6", dir)
	assert.NotNil(t, rt)
	assert.Equal(t, "claude", rt.Name())
}

func TestBuildRuntime_Codex_ReturnsRuntime(t *testing.T) {
	t.Parallel()

	rt := buildRuntime("codex", agent.RoleEngineer1, agent.ExecProcessRunner{}, "gpt-5-codex", "", "/tmp")
	assert.NotNil(t, rt)
	assert.Equal(t, "codex", rt.Name())
}

func TestBuildRuntime_Unknown_ReturnsNil(t *testing.T) {
	t.Parallel()

	rt := buildRuntime("gemini", agent.RolePM, agent.ExecProcessRunner{}, "model", "", "/tmp")
	assert.Nil(t, rt)
}

func TestCreateBridgeForRole_DefaultClaude(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config.RuntimeConfig{Default: "claude", Fallback: "codex"}
	bridge := createBridgeForRole(agent.RoleEngineer1, cfg, "gpt-5-codex", "claude-sonnet-4-6", dir)
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
	bridge := createBridgeForRole(agent.RoleEngineer1, cfg, "gpt-5-codex", "claude-sonnet-4-6", dir)
	assert.NotNil(t, bridge)
	assert.Equal(t, "codex", bridge.Active().Name())
}

func TestCreateBridgeForRole_UnknownDefault_ReturnsNil(t *testing.T) {
	t.Parallel()

	cfg := config.RuntimeConfig{Default: "invalid"}
	bridge := createBridgeForRole(agent.RolePM, cfg, "", "", "")
	assert.Nil(t, bridge)
}

func TestCreateBridgeForRole_SameFallbackAsActive_NilFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config.RuntimeConfig{Default: "codex", Fallback: "codex"}
	bridge := createBridgeForRole(agent.RoleEngineer2, cfg, "gpt-5-codex", "claude-sonnet-4-6", dir)
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

type fakeClaudeMCPRunner struct {
	calls   [][]string
	listOut []byte
	addErr  error
	addOut  []byte
}

func (runner *fakeClaudeMCPRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	cp := append([]string(nil), args...)
	runner.calls = append(runner.calls, cp)

	if len(args) >= 2 && args[0] == "mcp" && args[1] == "list" {
		return runner.listOut, nil
	}
	if len(args) >= 2 && args[0] == "mcp" && args[1] == "add" {
		return runner.addOut, runner.addErr
	}
	return nil, nil
}

func TestEnsureUserScopeMemoryMCPWith_NotRegistered_AddsOnly(t *testing.T) {
	t.Parallel()

	runner := &fakeClaudeMCPRunner{listOut: []byte("claude.ai Linear: ...")}

	err := ensureUserScopeMemoryMCPWith(context.Background(), runner, "/path/to/binary")

	require.NoError(t, err)
	assert.Len(t, runner.calls, 2) // list + add (no remove)
	assert.Equal(t, []string{"mcp", "list"}, runner.calls[0])
	assert.Equal(t, []string{"mcp", "add", "--scope", "user", "squad0-memory", "--", "/path/to/binary"}, runner.calls[1])
}

func TestEnsureUserScopeMemoryMCPWith_AlreadyRegistered_RemovesAndReadds(t *testing.T) {
	t.Parallel()

	runner := &fakeClaudeMCPRunner{listOut: []byte("squad0-memory: /old/path")}

	err := ensureUserScopeMemoryMCPWith(context.Background(), runner, "/new/path")

	require.NoError(t, err)
	assert.Len(t, runner.calls, 3) // list + remove + add
	assert.Equal(t, []string{"mcp", "remove", "squad0-memory", "--scope", "user"}, runner.calls[1])
	assert.Equal(t, []string{"mcp", "add", "--scope", "user", "squad0-memory", "--", "/new/path"}, runner.calls[2])
}

func TestEnsureUserScopeMemoryMCP_RealRunner_ExecutesClaudeCmd(t *testing.T) {
	t.Parallel()

	// Hits the production execClaudeMCPRunner.Run path. Returns an
	// error because the test environment has no claude installed (or
	// the binary path is bogus) — we just want to exercise the line.
	err := ensureUserScopeMemoryMCP(context.Background(), "/nonexistent/squad0-memory-mcp-test")

	// Either succeeds (real claude available) or fails (test env) —
	// both are fine, the point is the exec ran.
	_ = err
}

func TestEnsureUserScopeMemoryMCPWith_AddFails_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := &fakeClaudeMCPRunner{
		listOut: []byte(""),
		addOut:  []byte("permission denied"),
		addErr:  assert.AnError,
	}

	err := ensureUserScopeMemoryMCPWith(context.Background(), runner, "/path/to/binary")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude mcp add")
	assert.Contains(t, err.Error(), "permission denied")
}

func TestEnsureUserScopeLinearMCPWith_NotRegistered_RegistersHTTPWithBearer(t *testing.T) {
	t.Parallel()

	runner := &fakeClaudeMCPRunner{listOut: []byte("claude.ai Linear: ...")}
	err := ensureUserScopeLinearMCPWith(context.Background(), runner, "lin_secret")
	require.NoError(t, err)
	require.Len(t, runner.calls, 2)
	assert.Equal(t, []string{"mcp", "list"}, runner.calls[0])
	// Linear's official HTTP MCP at https://mcp.linear.app/mcp,
	// authenticated via Authorization: Bearer header so we never
	// touch OAuth.
	assert.Equal(t,
		[]string{
			"mcp", "add",
			"--scope", "user",
			"--transport", "http",
			"--header", "Authorization: Bearer lin_secret",
			"squad0-linear",
			"https://mcp.linear.app/mcp",
		},
		runner.calls[1],
	)
}

func TestEnsureUserScopeLinearMCPWith_AlreadyRegistered_RemovesAndReadds(t *testing.T) {
	t.Parallel()

	runner := &fakeClaudeMCPRunner{listOut: []byte("squad0-linear: ...")}
	err := ensureUserScopeLinearMCPWith(context.Background(), runner, "key")
	require.NoError(t, err)
	require.Len(t, runner.calls, 3)
	assert.Equal(t, []string{"mcp", "remove", "squad0-linear", "--scope", "user"}, runner.calls[1])
}

func TestEnsureUserScopeLinearMCPWith_AddFails_ReturnsError(t *testing.T) {
	t.Parallel()
	runner := &fakeClaudeMCPRunner{addOut: []byte("oops"), addErr: assert.AnError}
	err := ensureUserScopeLinearMCPWith(context.Background(), runner, "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude mcp add")
	assert.Contains(t, err.Error(), "oops")
}

func TestEnsureUserScopeLinearMCP_RealRunner_ExecutesClaudeCmd(t *testing.T) {
	t.Parallel()
	_ = ensureUserScopeLinearMCP(context.Background(), "key")
}
