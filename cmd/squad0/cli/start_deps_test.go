package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/config"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
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

	personaStore := cli.CreatePersonaStore(agentDBs)
	bot := cli.CreateSlackBot(ctx, cfg, slackSecrets, personaStore)

	require.NotNil(t, bot)
}

func TestRunOrchestratorWithContext_FullSetup_ReachesEventLoop(t *testing.T) {
	t.Parallel()

	restoreMCP := cli.StubVerifyMCPHealth()
	defer restoreMCP()

	tmpDir := t.TempDir()
	out := &bytes.Buffer{}

	ctx := context.Background()

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
		EventLoop: func(_ context.Context, _ *islack.Bot, _ *orchestrator.Scheduler, _ *orchestrator.Orchestrator) error {
			return nil
		},
	}

	err := cli.RunOrchestratorWithContext(ctx, config.DefaultConfig(), deps)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "Squad0")
	assert.Contains(t, out.String(), "All systems ready")
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

// NOTE: TestRunOrchestratorWithContext_OutputContainsBanner removed —
// it starts real Slack websocket goroutines that hang in non-TTY
// environments (git hooks, CI). Banner output is tested indirectly
// via the TUI package tests.

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

// NOTE: TestNewStartCommand_ValidConfig_FailsOnSecrets removed —
// it calls NewRootCommand() which hardcodes defaultStartDeps() (real keychain).
// On machines with secrets configured it hangs in the event loop.

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

	personaStore := cli.CreatePersonaStore(agentDBs)
	bot := cli.CreateSlackBot(ctx, cfg, slackSecrets, personaStore)

	require.NotNil(t, bot)
}

func TestResolveTargetRepo_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	result := cli.ResolveTargetRepo("")
	assert.Empty(t, result)
}

func TestResolveTargetRepo_WithRepo_ReturnsPath(t *testing.T) {
	t.Parallel()

	result := cli.ResolveTargetRepo("github.com/JR-G/makebook")

	assert.Contains(t, result, "repos")
	assert.Contains(t, result, "makebook")
	assert.NotEmpty(t, result)
}

func TestResolveMemoryBinaryPath_ReturnsStringOrEmpty(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		_ = cli.ResolveMemoryBinaryPath()
	})
}

func TestCreateAgents_SetsDBPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	_, agentDBs, err := cli.OpenAllDatabases(ctx, tmpDir)
	require.NoError(t, err)
	defer func() {
		for _, db := range agentDBs {
			_ = db.Close()
		}
	}()

	embedder := memory.NewEmbedder("http://localhost:11434", "nomic-embed-text")
	modelMap := cli.BuildModelMap(config.DefaultConfig())

	agents, err := cli.CreateAgents(agentDBs, embedder, modelMap, t.TempDir(), tmpDir)
	require.NoError(t, err)

	for role, agentInstance := range agents {
		assert.Contains(t, agentInstance.DBPath(), string(role),
			"DB path should include role name")
	}
}

func TestResolveStdin_NilDeps_ReturnsNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, cli.ResolveStdin(nil))
}

func TestResolveStdin_NilStdin_ReturnsNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, cli.ResolveStdin(&cli.SecretsCommandDeps{}))
}

func TestReadSecretValue_WithStdin_ReadsFromIt(t *testing.T) {
	t.Parallel()

	deps := &cli.SecretsCommandDeps{Stdin: strings.NewReader("my-secret\n")}

	value, err := cli.ReadSecretValue(deps, "TEST")

	require.NoError(t, err)
	assert.Equal(t, "my-secret", value)
}

func TestResolveStdin_WithStdin_ReturnsIt(t *testing.T) {
	t.Parallel()

	reader := strings.NewReader("test")
	deps := &cli.SecretsCommandDeps{Stdin: reader}

	assert.Equal(t, reader, cli.ResolveStdin(deps))
}

func TestBuildLinkConfig_WithTargetRepo(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Linear.Workspace = "jamesrg"
	cfg.GitHub.Owner = "JR-G"
	cfg.Project.TargetRepo = "github.com/JR-G/makebook"

	links := cli.BuildLinkConfig(cfg)

	assert.Equal(t, "jamesrg", links.LinearWorkspace)
	assert.Equal(t, "JR-G", links.GitHubOwner)
	assert.Equal(t, "makebook", links.GitHubRepo)
}

func TestBuildLinkConfig_NoTargetRepo(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Linear.Workspace = "jamesrg"

	links := cli.BuildLinkConfig(cfg)

	assert.Equal(t, "jamesrg", links.LinearWorkspace)
	assert.Empty(t, links.GitHubRepo)
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
