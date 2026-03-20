package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/config"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/health"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/logging"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
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

// SetupLogger exports setupLogger for testing with a configurable path.
func SetupLogger(logDir string) (*logging.Logger, error) {
	appLogger, err := logging.NewLogger(logDir)
	if err != nil {
		return nil, fmt.Errorf("creating logger: %w", err)
	}
	return appLogger, nil
}

// OpenAllDatabases exports openAllDatabases for testing with a
// configurable base directory.
func OpenAllDatabases(ctx context.Context, baseDir string) (*memory.DB, map[agent.Role]*memory.DB, error) {
	agentDir := filepath.Join(baseDir, "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating data directories: %w", err)
	}

	projectDB, err := memory.Open(ctx, filepath.Join(baseDir, "project.db"))
	if err != nil {
		return nil, nil, fmt.Errorf("opening project DB: %w", err)
	}

	agentDBs := make(map[agent.Role]*memory.DB, len(agent.AllRoles()))
	for _, role := range agent.AllRoles() {
		dbPath := filepath.Join(agentDir, string(role)+".db")
		agentDB, dbErr := memory.Open(ctx, dbPath)
		if dbErr != nil {
			closeDatabases(projectDB, agentDBs)
			return nil, nil, fmt.Errorf("opening DB for %s: %w", role, dbErr)
		}
		agentDBs[role] = agentDB
	}

	return projectDB, agentDBs, nil
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
) (map[agent.Role]*agent.Agent, error) {
	return createAgents(agentDBs, embedder, modelMap)
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
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating data directory: %w", err)
	}

	dbPath := filepath.Join(baseDir, "coordination.db")
	coordDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, nil, fmt.Errorf("opening coordination DB: %w", err)
	}

	if err := coordDB.PingContext(ctx); err != nil {
		_ = coordDB.Close()
		return nil, nil, fmt.Errorf("pinging coordination DB: %w", err)
	}

	store := coordination.NewCheckInStore(coordDB)
	if err := store.InitSchema(ctx); err != nil {
		_ = coordDB.Close()
		return nil, nil, fmt.Errorf("initialising coordination schema: %w", err)
	}

	return store, coordDB, nil
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
	return &CommandDispatcherWrapper{inner: newCommandDispatcher(orch, bot)}
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

// SetupLoggerDirect calls the production setupLogger with cwd set to
// tmpDir. Must not be called in parallel.
func SetupLoggerDirect(tmpDir string) (*logging.Logger, error) {
	origDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(tmpDir); err != nil {
		return nil, err
	}
	defer func() { _ = os.Chdir(origDir) }()

	return setupLogger()
}

// OpenAllDatabasesDirect calls the production openAllDatabases with cwd
// set to tmpDir. Must not be called in parallel.
func OpenAllDatabasesDirect(ctx context.Context, tmpDir string) (*memory.DB, map[agent.Role]*memory.DB, error) {
	origDir, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	if err := os.Chdir(tmpDir); err != nil {
		return nil, nil, err
	}
	defer func() { _ = os.Chdir(origDir) }()

	return openAllDatabases(ctx)
}

// CreateCoordinationStoreDirect calls the production
// createCoordinationStore with cwd set to tmpDir. Must not be called
// in parallel.
func CreateCoordinationStoreDirect(ctx context.Context, tmpDir string) (*coordination.CheckInStore, *sql.DB, error) {
	origDir, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	if err := os.Chdir(tmpDir); err != nil {
		return nil, nil, err
	}
	defer func() { _ = os.Chdir(origDir) }()

	return createCoordinationStore(ctx)
}

// ShowAgentStatusDirect calls the production showAgentStatus with cwd
// set to tmpDir. Must not be called in parallel.
func ShowAgentStatusDirect(cmd *cobra.Command, tmpDir string) {
	origDir, err := os.Getwd()
	if err != nil {
		return
	}
	if err := os.Chdir(tmpDir); err != nil {
		return
	}
	defer func() { _ = os.Chdir(origDir) }()

	showAgentStatus(context.Background(), cmd)
}
