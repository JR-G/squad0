package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/config"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/health"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/logging"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/secrets"
	"github.com/JR-G/squad0/internal/tui"
	"github.com/spf13/cobra"
)

// BuildModelMap exports buildModelMap for testing.
func BuildModelMap(cfg config.Config) map[agent.Role]string {
	return buildModelMap(cfg)
}

// ParseCronToInterval exports parseCronToInterval for testing.
func ParseCronToInterval(cron string) time.Duration {
	return parseCronToInterval(cron)
}

// DurationUntilHour exports durationUntilHour for testing.
func DurationUntilHour(hour int, now time.Time) time.Duration {
	return durationUntilHour(hour, now)
}

// SetupLogger exports setupLogger for testing with a configurable data dir.
func SetupLogger(dataDir string, out io.Writer) (*logging.Logger, error) {
	return setupLogger(dataDir, out)
}

// OpenAllDatabases exports openAllDatabases for testing with a
// configurable base directory.
func OpenAllDatabases(ctx context.Context, baseDir string) (*memory.DB, map[agent.Role]*memory.DB, error) {
	return openAllDatabases(ctx, baseDir)
}

// CloseDatabases exports closeDatabases for testing.
func CloseDatabases(projectDB *memory.DB, agentDBs map[agent.Role]*memory.DB) {
	closeDatabases(projectDB, agentDBs)
}

// CreateAgents exports createAgents for testing.
func CreateAgents(
	agentDBs map[agent.Role]*memory.DB,
	embedder *memory.Embedder,
	modelMap map[agent.Role]string,
	personalityDir string,
	dataDir ...string,
) (map[agent.Role]*agent.Agent, error) {
	dir := ""
	if len(dataDir) > 0 {
		dir = dataDir[0]
	}
	return createAgents(agentDBs, embedder, modelMap, personalityDir, dir, "")
}

// BuildSingleAgent exports buildSingleAgent for testing.
func BuildSingleAgent(
	role agent.Role,
	agentDB *memory.DB,
	embedder *memory.Embedder,
	modelMap map[agent.Role]string,
	loader *agent.PersonalityLoader,
) *agent.Agent {
	runner := agent.ExecProcessRunner{}
	return buildSingleAgent(role, agentDB, embedder, modelMap, loader, runner)
}

// CreateCoordinationStore exports createCoordinationStore for testing
// with a configurable base directory.
func CreateCoordinationStore(ctx context.Context, baseDir string) (*coordination.CheckInStore, *sql.DB, error) {
	return createCoordinationStore(ctx, baseDir)
}

// CreateHealthMonitor exports createHealthMonitor for testing.
func CreateHealthMonitor() *health.Monitor {
	return createHealthMonitor()
}

// CommandDispatcherWrapper wraps commandDispatcher for external testing.
type CommandDispatcherWrapper struct {
	inner *commandDispatcher
}

// NewCommandDispatcher exports newCommandDispatcher for testing.
func NewCommandDispatcher(orch *orchestrator.Orchestrator, bot *slack.Bot) *CommandDispatcherWrapper {
	return &CommandDispatcherWrapper{inner: newCommandDispatcher(orch, bot, nil, nil, slack.LinkConfig{})}
}

// NewCommandDispatcherWithConversation exports newCommandDispatcher
// with a conversation engine for testing threaded message routing.
func NewCommandDispatcherWithConversation(
	orch *orchestrator.Orchestrator,
	bot *slack.Bot,
	conversation *orchestrator.ConversationEngine,
) *CommandDispatcherWrapper {
	return &CommandDispatcherWrapper{inner: newCommandDispatcher(orch, bot, conversation, nil, slack.LinkConfig{})}
}

// HandleMessage exports handleMessage for testing.
func (wrapper *CommandDispatcherWrapper) HandleMessage(ctx context.Context, msg slack.IncomingMessage) {
	wrapper.inner.handleMessage(ctx, msg)
}

// RouteCommand exports routeCommand for testing.
func (wrapper *CommandDispatcherWrapper) RouteCommand(ctx context.Context, cmd slack.Command) string {
	return wrapper.inner.routeCommand(ctx, cmd)
}

// ShowEmptyAgentList exports showEmptyAgentList for testing.
func ShowEmptyAgentList(cmd *cobra.Command) {
	showEmptyAgentList(cmd)
}

// LoadCheckIns exports loadCheckIns for testing.
func LoadCheckIns(ctx context.Context, dbPath string) ([]coordination.CheckIn, error) {
	return loadCheckIns(ctx, dbPath)
}

// ShowStatus exports showStatus for testing.
func ShowStatus(cmd *cobra.Command) {
	_ = showStatus(cmd)
}

// ShowAgentStatusWithPath exports showAgentStatus for testing with a
// configurable coordination DB path.
func ShowAgentStatusWithPath(cmd *cobra.Command, coordDBPath string) {
	ctx := context.Background()

	if _, err := os.Stat(coordDBPath); os.IsNotExist(err) {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), tui.Section("Agents"))
		_, _ = fmt.Fprint(cmd.OutOrStdout(), tui.StepPending("No coordination data yet — run `squad0 start` first"))
		return
	}

	checkIns, err := loadCheckIns(ctx, coordDBPath)
	if err != nil {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), tui.StepFail(err.Error()))
		return
	}

	if len(checkIns) == 0 {
		showEmptyAgentList(cmd)
		return
	}

	_, _ = fmt.Fprint(cmd.OutOrStdout(), tui.FormatAgentStatus(checkIns, nil))
}

// ShowAgentStatusDirect calls the production showAgentStatus with cwd
// set to tmpDir. Must not be called in parallel.
func ShowAgentStatusDirect(cmd *cobra.Command, tmpDir string) {
	origDir, err := os.Getwd()
	if err != nil {
		return
	}
	if chErr := os.Chdir(tmpDir); chErr != nil {
		return
	}
	defer func() { _ = os.Chdir(origDir) }()

	showAgentStatus(context.Background(), cmd)
}

// LoadSecrets exports loadSecrets for testing.
func LoadSecrets(ctx context.Context, loader SecretLoader, out io.Writer) (secrets.Secrets, error) {
	return loadSecrets(ctx, loader, out)
}

// RunOrchestratorWithContext exports runOrchestratorWithContext for
// testing.
func RunOrchestratorWithContext(ctx context.Context, cfg config.Config, deps StartDeps) error {
	return runOrchestratorWithContext(ctx, cfg, deps)
}

// CreatePersonaStore exports createPersonaStore for testing.
func CreatePersonaStore(agentDBs map[agent.Role]*memory.DB) *slack.PersonaStore {
	return createPersonaStore(agentDBs)
}

// CreateSlackBot exports createSlackBot for testing.
func CreateSlackBot(
	ctx context.Context,
	cfg config.Config,
	slackSecrets secrets.Secrets,
	personaStore *slack.PersonaStore,
) *slack.Bot {
	return createSlackBot(ctx, cfg, slackSecrets, personaStore)
}

// DefaultStartDeps exports defaultStartDeps for testing.
func DefaultStartDeps() StartDeps {
	return defaultStartDeps()
}

// ReadFromTUI exports readFromTUI for testing.
func ReadFromTUI(name string, input io.Reader) (string, error) {
	return readFromTUI(name, input)
}

// ResolveTargetRepo exports resolveTargetRepo for testing.
func ResolveTargetRepo(targetRepo string) string {
	return resolveTargetRepo(targetRepo)
}

// ReadSecretValue exports readSecretValue for testing.
func ReadSecretValue(deps *SecretsCommandDeps, name string) (string, error) {
	return readSecretValue(deps, name)
}

// ResolveStdin exports resolveStdin for testing.
func ResolveStdin(deps *SecretsCommandDeps) io.Reader {
	return resolveStdin(deps)
}

// BuildLinkConfig exports buildLinkConfig for testing.
func BuildLinkConfig(cfg config.Config) slack.LinkConfig {
	return buildLinkConfig(cfg)
}

// ResolveMemoryBinaryPath exports resolveMemoryBinaryPath for testing.
func ResolveMemoryBinaryPath() string {
	return resolveMemoryBinaryPath()
}

// ConfigureGitHubAppToken exports configureGitHubAppToken for testing.
func ConfigureGitHubAppToken(ctx context.Context, agents map[agent.Role]*agent.Agent, loader SecretLoader, out io.Writer) {
	configureGitHubAppToken(ctx, agents, loader, out)
}

// ApplyGitHubAppTokenWithURL exports applyGitHubAppTokenWithURL for testing with a mock API.
func ApplyGitHubAppTokenWithURL(ctx context.Context, agents map[agent.Role]*agent.Agent, appID, installID, privateKey, apiURL string, out io.Writer) {
	applyGitHubAppTokenWithURL(ctx, agents, appID, installID, privateKey, apiURL, out)
}

// SeedConversationHistory exports seedConversationHistory for testing.
func SeedConversationHistory(
	ctx context.Context,
	bot *slack.Bot,
	conversation *orchestrator.ConversationEngine,
	cfg config.Config,
) {
	seedConversationHistory(ctx, bot, conversation, cfg)
}
