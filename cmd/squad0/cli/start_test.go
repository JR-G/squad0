package cli_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/config"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/secrets"
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

func TestParseCronToInterval_ValidCron_ReturnsDurationLessThan24h(t *testing.T) {
	t.Parallel()

	result := cli.ParseCronToInterval("0 9 * * *")

	assert.Greater(t, result, time.Duration(0))
	assert.LessOrEqual(t, result, 24*time.Hour)
}

func TestParseCronToInterval_InvalidCron_FallsBackTo24h(t *testing.T) {
	t.Parallel()

	result := cli.ParseCronToInterval("anything")

	assert.Equal(t, 24*time.Hour, result)
}

func TestParseCronToInterval_EmptyString_FallsBackTo24h(t *testing.T) {
	t.Parallel()

	result := cli.ParseCronToInterval("")

	assert.Equal(t, 24*time.Hour, result)
}

func TestParseCronToInterval_InvalidHour_FallsBackTo24h(t *testing.T) {
	t.Parallel()

	result := cli.ParseCronToInterval("0 25 * * *")

	assert.Equal(t, 24*time.Hour, result)
}

func TestDurationUntilHour_FutureToday_ReturnsSameDay(t *testing.T) {
	t.Parallel()

	// 08:00 — next occurrence of hour 14 is today at 14:00 = 6 hours.
	now := time.Date(2026, 3, 26, 8, 0, 0, 0, time.UTC)
	result := cli.DurationUntilHour(14, now)

	assert.Equal(t, 6*time.Hour, result)
}

func TestDurationUntilHour_PastToday_ReturnsTomorrow(t *testing.T) {
	t.Parallel()

	// 15:00 — next occurrence of hour 9 is tomorrow at 09:00 = 18 hours.
	now := time.Date(2026, 3, 26, 15, 0, 0, 0, time.UTC)
	result := cli.DurationUntilHour(9, now)

	assert.Equal(t, 18*time.Hour, result)
}

func TestDurationUntilHour_ExactlyNow_ReturnsTomorrow(t *testing.T) {
	t.Parallel()

	// 09:00 exactly — hour 9 has already passed (not strictly after), so tomorrow.
	now := time.Date(2026, 3, 26, 9, 0, 0, 0, time.UTC)
	result := cli.DurationUntilHour(9, now)

	assert.Equal(t, 24*time.Hour, result)
}

func TestSetupLogger_ValidPath_ReturnsLogger(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	out := &bytes.Buffer{}

	appLogger, _, err := cli.SetupLogger(dataDir, out)

	require.NoError(t, err)
	require.NotNil(t, appLogger)
	assert.Contains(t, out.String(), "Logger started")
	_ = appLogger.Close()
}

func TestSetupLogger_InvalidPath_ReturnsError(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}

	appLogger, _, err := cli.SetupLogger("/dev/null/impossible/path", out)

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

type stubSecretLoader struct{}

func (s *stubSecretLoader) LoadAll(_ context.Context) (secrets.Secrets, error) {
	return secrets.Secrets{SlackBotToken: "xoxb-test", SlackAppToken: "xapp-test"}, nil
}

type stubSecretLoaderWithApp struct {
	secrets map[string]string
}

func (s *stubSecretLoaderWithApp) LoadAll(_ context.Context) (secrets.Secrets, error) {
	return secrets.Secrets{SlackBotToken: "xoxb-test", SlackAppToken: "xapp-test"}, nil
}

func (s *stubSecretLoaderWithApp) GetOptional(_ context.Context, name string) (string, error) {
	return s.secrets[name], nil
}

func TestConfigureGitHubAppToken_NoManager_ShowsWarning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var buf bytes.Buffer

	agents := make(map[agent.Role]*agent.Agent)

	// Stub loader isn't a *secrets.Manager, so loadGitHubAppSecrets returns empty.
	cli.ConfigureGitHubAppToken(ctx, agents, &stubSecretLoader{}, &buf)

	assert.Contains(t, buf.String(), "GitHub App not configured")
}

func TestApplyGitHubAppToken_InvalidKey_ShowsFail(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	agents := make(map[agent.Role]*agent.Agent)

	cli.ApplyGitHubAppTokenWithURL(context.Background(), agents, "123", "456", "not a pem", "", &buf)

	assert.Contains(t, buf.String(), "GitHub App key invalid")
}

func TestApplyGitHubAppToken_ValidKey_APIError_ShowsFail(t *testing.T) {
	t.Parallel()

	// Generate a real RSA key for the test.
	pemData := generateTestPEMForCLI(t)
	var buf bytes.Buffer
	agents := make(map[agent.Role]*agent.Agent)

	// No mock server — the provider will try to hit api.github.com and fail.
	// But NewAppTokenProvider succeeds, Token() fails.
	cli.ApplyGitHubAppTokenWithURL(context.Background(), agents, "123", "456", pemData, "", &buf)

	assert.Contains(t, buf.String(), "GitHub App token failed")
}

func generateTestPEMForCLI(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes}

	return string(pem.EncodeToMemory(block))
}

func TestConfigureGitHubAppToken_WithValidApp_APIError_ShowsFail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var buf bytes.Buffer

	pemData := generateTestPEMForCLI(t)

	loader := &stubSecretLoaderWithApp{
		secrets: map[string]string{
			"GITHUB_APP_ID":              "123",
			"GITHUB_APP_INSTALLATION_ID": "456",
			"GITHUB_APP_PRIVATE_KEY":     pemData,
		},
	}

	agents := make(map[agent.Role]*agent.Agent)

	cli.ConfigureGitHubAppToken(ctx, agents, loader, &buf)

	assert.Contains(t, buf.String(), "GitHub App token failed")
}

func TestApplyGitHubAppToken_WithMockAPI_SetsTokenOnAgents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var buf bytes.Buffer

	pemData := generateTestPEMForCLI(t)

	// Mock GitHub API that returns a valid installation token.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"token":      "ghs_test_token_abc",
			"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	// Use ApplyGitHubAppTokenWithURL to point at mock server.
	agents := createTestAgents(t)

	cli.ApplyGitHubAppTokenWithURL(ctx, agents, "123", "456", pemData, server.URL, &buf)

	assert.Contains(t, buf.String(), "GitHub App token configured")
}

func TestConfigureGitHubAppToken_EmptySecrets_ShowsWarning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var buf bytes.Buffer

	// Has GetOptional but returns empty strings.
	loader := &stubSecretLoaderWithApp{secrets: map[string]string{}}
	agents := make(map[agent.Role]*agent.Agent)

	cli.ConfigureGitHubAppToken(ctx, agents, loader, &buf)

	assert.Contains(t, buf.String(), "GitHub App not configured")
}

func createTestAgents(t *testing.T) map[agent.Role]*agent.Agent {
	t.Helper()

	ctx := context.Background()
	personalityDir := t.TempDir()

	for _, role := range agent.AllRoles() {
		require.NoError(t, os.WriteFile(
			filepath.Join(personalityDir, role.PersonalityFile()),
			[]byte("You are "+string(role)+"."), 0o644,
		))
	}

	agents := make(map[agent.Role]*agent.Agent, len(agent.AllRoles()))
	for _, role := range agent.AllRoles() {
		memDB, err := memory.Open(ctx, ":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = memDB.Close() })

		modelMap := cli.BuildModelMap(config.DefaultConfig())
		agents[role] = cli.BuildSingleAgent(role, memDB, memory.NewEmbedder("http://localhost:11434", "test"), modelMap, agent.NewPersonalityLoader(personalityDir))
	}

	return agents
}
