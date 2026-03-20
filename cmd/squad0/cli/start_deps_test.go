package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
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

// fakeSecretLoader implements cli.SecretLoader for testing.
type fakeSecretLoader struct {
	secrets secrets.Secrets
	err     error
}

func (loader *fakeSecretLoader) LoadAll(_ context.Context) (secrets.Secrets, error) {
	return loader.secrets, loader.err
}

func TestLoadSecrets_Success_ReturnsSecrets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	out := &bytes.Buffer{}
	expected := secrets.Secrets{
		SlackBotToken: "xoxb-test-token",
		SlackAppToken: "xapp-test-token",
	}
	loader := &fakeSecretLoader{secrets: expected}

	result, err := cli.LoadSecrets(ctx, loader, out)

	require.NoError(t, err)
	assert.Equal(t, expected.SlackBotToken, result.SlackBotToken)
	assert.Equal(t, expected.SlackAppToken, result.SlackAppToken)
	assert.Contains(t, out.String(), "Secrets loaded")
}

func TestLoadSecrets_Error_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	out := &bytes.Buffer{}
	loader := &fakeSecretLoader{err: errors.New("keychain locked")}

	result, err := cli.LoadSecrets(ctx, loader, out)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading secrets")
	assert.Contains(t, err.Error(), "keychain locked")
	assert.Equal(t, secrets.Secrets{}, result)
	assert.Contains(t, out.String(), "Secrets missing")
}

func TestDefaultStartDeps_ReturnsPopulatedStruct(t *testing.T) {
	t.Parallel()

	deps := cli.DefaultStartDeps()

	assert.NotNil(t, deps.SecretLoader, "SecretLoader should not be nil")
	assert.NotNil(t, deps.Output, "Output should not be nil")
	assert.Equal(t, "data", deps.DataDir)
	assert.Equal(t, "agents", deps.PersonalityDir)
}

func TestCreateSlackBot_ReturnsNonNilBot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	projectDB, agentDBs, err := cli.OpenAllDatabases(ctx, tmpDir)
	require.NoError(t, err)
	defer cli.CloseDatabases(projectDB, agentDBs)

	cfg := config.DefaultConfig()
	slackSecrets := secrets.Secrets{
		SlackBotToken: "xoxb-test",
		SlackAppToken: "xapp-test",
	}

	bot := cli.CreateSlackBot(ctx, cfg, slackSecrets, agentDBs)

	require.NotNil(t, bot)
}

func TestRunOrchestratorWithContext_ShortTimeout_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Use a short timeout so the setup completes but the goroutines
	// (orchestrator loop, scheduler, socket listener) exit promptly.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	loader := &fakeSecretLoader{
		secrets: secrets.Secrets{
			SlackBotToken: "xoxb-test",
			SlackAppToken: "xapp-test",
		},
	}

	deps := cli.StartDeps{
		SecretLoader:   loader,
		Output:         &bytes.Buffer{},
		DataDir:        tmpDir,
		PersonalityDir: t.TempDir(),
	}

	cfg := config.DefaultConfig()

	err := cli.RunOrchestratorWithContext(ctx, cfg, deps)

	// Should return an error when the context times out or when a
	// goroutine (e.g. socket listener with no real server) fails.
	require.Error(t, err)
}

func TestRunOrchestratorWithContext_BadDataDir_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	loader := &fakeSecretLoader{
		secrets: secrets.Secrets{
			SlackBotToken: "xoxb-test",
			SlackAppToken: "xapp-test",
		},
	}

	deps := cli.StartDeps{
		SecretLoader:   loader,
		Output:         &bytes.Buffer{},
		DataDir:        "/dev/null/impossible",
		PersonalityDir: t.TempDir(),
	}

	err := cli.RunOrchestratorWithContext(ctx, config.DefaultConfig(), deps)

	require.Error(t, err)
}

func TestRunOrchestratorWithContext_SecretFailure_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ctx := context.Background()
	loader := &fakeSecretLoader{err: errors.New("vault sealed")}

	deps := cli.StartDeps{
		SecretLoader:   loader,
		Output:         &bytes.Buffer{},
		DataDir:        tmpDir,
		PersonalityDir: t.TempDir(),
	}

	err := cli.RunOrchestratorWithContext(ctx, config.DefaultConfig(), deps)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading secrets")
}

func TestRunOrchestratorWithContext_LoggerFailure_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Block the logs directory by placing a file where a directory is
	// expected.
	logsPath := tmpDir + "/logs"
	require.NoError(t, os.WriteFile(logsPath, []byte("blocker"), 0o444))

	ctx := context.Background()
	loader := &fakeSecretLoader{
		secrets: secrets.Secrets{
			SlackBotToken: "xoxb-test",
			SlackAppToken: "xapp-test",
		},
	}

	deps := cli.StartDeps{
		SecretLoader:   loader,
		Output:         &bytes.Buffer{},
		DataDir:        tmpDir,
		PersonalityDir: t.TempDir(),
	}

	err := cli.RunOrchestratorWithContext(ctx, config.DefaultConfig(), deps)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating logger")
}

func TestRunOrchestratorWithContext_OutputContainsBanner(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	out := &bytes.Buffer{}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	loader := &fakeSecretLoader{
		secrets: secrets.Secrets{
			SlackBotToken: "xoxb-test",
			SlackAppToken: "xapp-test",
		},
	}

	deps := cli.StartDeps{
		SecretLoader:   loader,
		Output:         out,
		DataDir:        tmpDir,
		PersonalityDir: t.TempDir(),
	}

	_ = cli.RunOrchestratorWithContext(ctx, config.DefaultConfig(), deps)

	assert.Contains(t, out.String(), "Squad0")
	assert.Contains(t, out.String(), "All systems ready")
}

func TestCreateAgents_WithPersonalityDir_CreatesAll(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	projectDB, agentDBs, err := cli.OpenAllDatabases(ctx, tmpDir)
	require.NoError(t, err)
	defer cli.CloseDatabases(projectDB, agentDBs)

	embedder := memory.NewEmbedder("http://localhost:11434", "nomic-embed-text")
	modelMap := cli.BuildModelMap(config.DefaultConfig())
	personalityDir := t.TempDir()

	agents, err := cli.CreateAgents(agentDBs, embedder, modelMap, personalityDir)

	require.NoError(t, err)
	assert.Len(t, agents, len(agent.AllRoles()))
}

func TestNewStartCommand_InvalidConfig_ReturnsError(t *testing.T) {
	t.Parallel()

	rootCmd := cli.NewRootCommand()
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"start", "--config", "/nonexistent/config.toml"})

	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading config")
}

func TestNewStartCommand_ValidConfig_FailsOnSecrets(t *testing.T) {
	t.Parallel()

	// Create a minimal valid config file.
	configPath := writeMinimalConfig(t)

	rootCmd := cli.NewRootCommand()
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"start", "--config", configPath})

	err := rootCmd.Execute()

	// Should fail on secrets loading (no keychain secrets in test),
	// but this covers the config.Load success path in newStartCommand.
	require.Error(t, err)
}

func writeMinimalConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := dir + "/squad0.toml"
	content := `[project]
name = "test"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestNewStatusCommand_Executes_ShowsBanner(t *testing.T) {
	t.Parallel()

	rootCmd := cli.NewRootCommand()
	out := &bytes.Buffer{}
	rootCmd.SetOut(out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"status"})

	// status command calls showSecretStatus and showAgentStatus.
	// Both use real system state, so we just verify it does not
	// panic and includes the banner.
	_ = rootCmd.Execute()

	assert.Contains(t, out.String(), "Squad0")
}

func TestCreateSlackBot_EmptyChannels_ReturnsBot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	projectDB, agentDBs, err := cli.OpenAllDatabases(ctx, tmpDir)
	require.NoError(t, err)
	defer cli.CloseDatabases(projectDB, agentDBs)

	cfg := config.DefaultConfig()
	cfg.Slack.Channels = nil

	slackSecrets := secrets.Secrets{
		SlackBotToken: "xoxb-test",
		SlackAppToken: "xapp-test",
	}

	bot := cli.CreateSlackBot(ctx, cfg, slackSecrets, agentDBs)

	require.NotNil(t, bot)
}

func TestRunOrchestratorWithContext_CoordStoreFailure_ReturnsError(t *testing.T) {
	t.Parallel()

	// Set up a tmpDir where databases open fine but the coordination
	// store cannot be created. We do this by pre-creating
	// coordination.db as a directory so sql.Open fails on ping.
	tmpDir := t.TempDir()

	// Create a directory at the coordination.db path to make
	// sql.Open fail.
	coordPath := tmpDir + "/coordination.db"
	require.NoError(t, os.MkdirAll(coordPath, 0o755))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	loader := &fakeSecretLoader{
		secrets: secrets.Secrets{
			SlackBotToken: "xoxb-test",
			SlackAppToken: "xapp-test",
		},
	}

	deps := cli.StartDeps{
		SecretLoader:   loader,
		Output:         &bytes.Buffer{},
		DataDir:        tmpDir,
		PersonalityDir: t.TempDir(),
	}

	err := cli.RunOrchestratorWithContext(ctx, config.DefaultConfig(), deps)

	require.Error(t, err)
}
