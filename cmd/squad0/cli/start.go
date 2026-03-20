package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
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
	"github.com/JR-G/squad0/internal/secrets"
	"github.com/JR-G/squad0/internal/tui"
	_ "github.com/mattn/go-sqlite3" // SQLite driver for coordination DB.
	"github.com/spf13/cobra"
)

func newStartCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the orchestrator loop",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			return runOrchestrator(cfg)
		},
	}
}

func runOrchestrator(cfg config.Config) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Print(tui.Banner())

	appLogger, err := setupLogger()
	if err != nil {
		return err
	}
	defer func() { _ = appLogger.Close() }()

	slackSecrets, err := loadSlackSecrets(ctx)
	if err != nil {
		return err
	}
	fmt.Print(tui.StepDone("Secrets loaded"))

	projectDB, agentDBs, err := openAllDatabases(ctx)
	if err != nil {
		return err
	}
	defer closeDatabases(projectDB, agentDBs)
	fmt.Print(tui.StepDone("Databases opened"))

	embedder := memory.NewEmbedder(cfg.Embeddings.OllamaURL, cfg.Embeddings.Model)
	modelMap := buildModelMap(cfg)
	agents, err := createAgents(agentDBs, embedder, modelMap)
	if err != nil {
		return fmt.Errorf("creating agents: %w", err)
	}
	fmt.Print(tui.StepDone(fmt.Sprintf("%d agents created", len(agents))))

	bot := createSlackBot(ctx, cfg, slackSecrets, agentDBs)
	fmt.Print(tui.StepDone("Slack bot connected"))

	checkInStore, coordDB, err := createCoordinationStore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = coordDB.Close() }()
	fmt.Print(tui.StepDone("Coordination DB ready"))

	monitor := createHealthMonitor()
	alerter := health.NewAlerter(monitor, bot, "triage")
	scheduler := orchestrator.NewScheduler(bot, monitor, alerter, orchestrator.SchedulerConfig{
		StandupInterval:   parseCronToInterval(cfg.Rituals.StandupCron),
		HealthInterval:    5 * time.Minute,
		RetroAfterTickets: cfg.Rituals.RetroAfterTickets,
	})

	pmAgent := agents[agent.RolePM]
	assigner := orchestrator.NewAssigner(pmAgent)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:  time.Duration(cfg.Agents.CooldownSeconds) * time.Second,
			MaxParallel:   cfg.Agents.MaxParallel,
			CooldownAfter: time.Duration(cfg.Agents.CooldownSeconds) * time.Second,
		},
		agents, checkInStore, bot, assigner,
	)

	commandHandler := newCommandDispatcher(orch, bot)
	bot.OnMessage(commandHandler.handleMessage)

	fmt.Print(tui.StepDone("All systems ready"))
	fmt.Println()

	appLogger.Info("system", "startup", "orchestrator starting")

	errCh := make(chan error, 3)
	go func() { errCh <- bot.ListenForEvents(ctx) }()
	go func() { errCh <- scheduler.Run(ctx) }()
	go func() { errCh <- orch.Run(ctx) }()

	return <-errCh
}

func setupLogger() (*logging.Logger, error) {
	appLogger, err := logging.NewLogger("data/logs")
	if err != nil {
		fmt.Print(tui.StepFail("Logger failed"))
		return nil, fmt.Errorf("creating logger: %w", err)
	}
	fmt.Print(tui.StepDone("Logger started"))
	return appLogger, nil
}

func loadSlackSecrets(ctx context.Context) (secrets.Secrets, error) {
	runner := secrets.ExecRunner{}
	kc := secrets.NewKeychain(secrets.ServiceName, runner)
	mgr := secrets.NewManager(kc)

	slackSecrets, err := mgr.LoadAll(ctx)
	if err != nil {
		fmt.Print(tui.StepFail("Secrets missing"))
		return secrets.Secrets{}, fmt.Errorf("loading secrets: %w", err)
	}
	return slackSecrets, nil
}

func openAllDatabases(ctx context.Context) (*memory.DB, map[agent.Role]*memory.DB, error) {
	if err := os.MkdirAll("data/agents", 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating data directories: %w", err)
	}

	projectDB, err := memory.Open(ctx, "data/project.db")
	if err != nil {
		return nil, nil, fmt.Errorf("opening project DB: %w", err)
	}

	agentDBs := make(map[agent.Role]*memory.DB, len(agent.AllRoles()))
	for _, role := range agent.AllRoles() {
		dbPath := filepath.Join("data", "agents", string(role)+".db")
		agentDB, dbErr := memory.Open(ctx, dbPath)
		if dbErr != nil {
			closeDatabases(projectDB, agentDBs)
			return nil, nil, fmt.Errorf("opening DB for %s: %w", role, dbErr)
		}
		agentDBs[role] = agentDB
	}

	return projectDB, agentDBs, nil
}

func closeDatabases(projectDB *memory.DB, agentDBs map[agent.Role]*memory.DB) {
	for _, db := range agentDBs {
		_ = db.Close()
	}
	if projectDB != nil {
		_ = projectDB.Close()
	}
}

func buildModelMap(cfg config.Config) map[agent.Role]string {
	return map[agent.Role]string{
		agent.RolePM:        cfg.Agents.Models.PM,
		agent.RoleTechLead:  cfg.Agents.Models.TechLead,
		agent.RoleEngineer1: cfg.Agents.Models.Engineer,
		agent.RoleEngineer2: cfg.Agents.Models.Engineer,
		agent.RoleEngineer3: cfg.Agents.Models.Engineer,
		agent.RoleReviewer:  cfg.Agents.Models.Reviewer,
		agent.RoleDesigner:  cfg.Agents.Models.Designer,
	}
}

func createAgents(
	agentDBs map[agent.Role]*memory.DB,
	embedder *memory.Embedder,
	modelMap map[agent.Role]string,
) (map[agent.Role]*agent.Agent, error) {
	loader := agent.NewPersonalityLoader("agents")
	runner := agent.ExecProcessRunner{}
	agents := make(map[agent.Role]*agent.Agent, len(agent.AllRoles()))

	for _, role := range agent.AllRoles() {
		agentDB, ok := agentDBs[role]
		if !ok {
			return nil, fmt.Errorf("no database for role %s", role)
		}

		agents[role] = buildSingleAgent(role, agentDB, embedder, modelMap, loader, runner)
	}

	return agents, nil
}

func buildSingleAgent(
	role agent.Role,
	agentDB *memory.DB,
	embedder *memory.Embedder,
	modelMap map[agent.Role]string,
	loader *agent.PersonalityLoader,
	runner agent.ProcessRunner,
) *agent.Agent {
	graphStore := memory.NewGraphStore(agentDB)
	factStore := memory.NewFactStore(agentDB)
	episodeStore := memory.NewEpisodeStore(agentDB)
	ftsStore := memory.NewFTSStore(agentDB)
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)

	model := agent.ModelForRole(role, modelMap)
	session := agent.NewSession(runner)

	return agent.NewAgent(role, model, session, loader, retriever, agentDB, episodeStore, embedder)
}

func createSlackBot(
	ctx context.Context,
	cfg config.Config,
	slackSecrets secrets.Secrets,
	agentDBs map[agent.Role]*memory.DB,
) *slack.Bot {
	graphStores := make(map[agent.Role]*memory.GraphStore, len(agentDBs))
	factStores := make(map[agent.Role]*memory.FactStore, len(agentDBs))

	for role, db := range agentDBs {
		graphStores[role] = memory.NewGraphStore(db)
		factStores[role] = memory.NewFactStore(db)
	}

	personaStore := slack.NewPersonaStore(graphStores, factStores)
	personas := personaStore.LoadAllPersonas(ctx)

	channels := make(map[string]string, len(cfg.Slack.Channels))
	for _, name := range cfg.Slack.Channels {
		channels[name] = name
	}

	bot := slack.NewBot(slack.BotConfig{
		BotToken:   slackSecrets.SlackBotToken,
		AppToken:   slackSecrets.SlackAppToken,
		Channels:   channels,
		Personas:   personas,
		MinSpacing: 2 * time.Second,
	})

	return bot
}

func createCoordinationStore(ctx context.Context) (*coordination.CheckInStore, *sql.DB, error) {
	if err := os.MkdirAll("data", 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating data directory: %w", err)
	}

	coordDB, err := sql.Open("sqlite3", "data/coordination.db?_journal_mode=WAL&_busy_timeout=5000")
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

func createHealthMonitor() *health.Monitor {
	return health.NewMonitor(agent.AllRoles(), health.MonitorConfig{
		MaxIdleTime:          10 * time.Minute,
		MaxSessionTime:       30 * time.Minute,
		MaxConsecutiveErrors: 3,
	})
}

func parseCronToInterval(_ string) time.Duration {
	return 24 * time.Hour
}
