package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
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
	"github.com/JR-G/squad0/internal/pipeline"
	"github.com/JR-G/squad0/internal/secrets"
	"github.com/JR-G/squad0/internal/tui"
	_ "github.com/mattn/go-sqlite3" // SQLite driver for coordination DB.
	"github.com/spf13/cobra"
)

// EventLoopFunc is the function that runs the blocking event loop.
type EventLoopFunc func(ctx context.Context, bot *slack.Bot, sched *orchestrator.Scheduler, orch *orchestrator.Orchestrator) error

// StartDeps holds injectable dependencies for the start command.
type StartDeps struct {
	SecretLoader   SecretLoader
	Output         io.Writer
	DataDir        string
	PersonalityDir string
	EventLoop      EventLoopFunc
}

// SecretLoader loads secrets from a backing store.
type SecretLoader interface {
	LoadAll(ctx context.Context) (secrets.Secrets, error)
}

func defaultStartDeps() StartDeps {
	runner := secrets.ExecRunner{}
	kc := secrets.NewKeychain(secrets.ServiceName, runner)
	mgr := secrets.NewManager(kc)

	return StartDeps{
		SecretLoader:   mgr,
		Output:         os.Stdout,
		DataDir:        "data",
		PersonalityDir: "agents",
		EventLoop:      runEventLoop,
	}
}

func newStartCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the orchestrator loop",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			return runOrchestrator(cfg, defaultStartDeps())
		},
	}
}

func runOrchestrator(cfg config.Config, deps StartDeps) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	return runOrchestratorWithContext(ctx, cfg, deps)
}

func runOrchestratorWithContext(ctx context.Context, cfg config.Config, deps StartDeps) error {
	out := deps.Output

	_, _ = fmt.Fprint(out, tui.Banner())

	appLogger, consoleWriter, err := setupLogger(deps.DataDir, out)
	if err != nil {
		return err
	}
	defer func() { _ = appLogger.Close() }()

	slackSecrets, err := loadSecrets(ctx, deps.SecretLoader, out)
	if err != nil {
		return err
	}

	projectDB, agentDBs, err := openAllDatabases(ctx, deps.DataDir)
	if err != nil {
		return err
	}
	defer closeDatabases(projectDB, agentDBs)
	_, _ = fmt.Fprint(out, tui.StepDone("Databases opened"))

	embedder := memory.NewEmbedder(cfg.Embeddings.OllamaURL, cfg.Embeddings.Model)
	modelMap := buildModelMap(cfg)
	targetRepoDir := resolveTargetRepo(cfg.Project.TargetRepo)
	agents, err := createAgents(agentDBs, embedder, modelMap, deps.PersonalityDir, deps.DataDir, targetRepoDir, cfg.Agents.CodexFallbackModel)
	if err != nil {
		return fmt.Errorf("creating agents: %w", err)
	}
	_, _ = fmt.Fprint(out, tui.StepDone(fmt.Sprintf("%d agents created", len(agents))))

	// Register MCP servers with Codex so fallback sessions have Linear access.
	if cfg.Agents.CodexFallbackModel != "" {
		ensureCodexMCP(ctx, out)
	}

	// Wire runtime bridges — persistent sessions for Claude, fresh for Codex.
	wireBridges(agents, cfg.Agents.Runtime, cfg.Agents.CodexFallbackModel, modelMap, targetRepoDir, deps.DataDir)

	personaStore := createPersonaStore(agentDBs)
	bot := createSlackBot(ctx, cfg, slackSecrets, personaStore)
	_, _ = fmt.Fprint(out, tui.StepDone("Slack bot connected"))

	orchestrator.RunIntroductions(ctx, agents, personaStore, bot)
	bot.UpdatePersonas(personaStore.LoadAllPersonas(ctx))

	briefingDone := filepath.Join(deps.DataDir, ".briefing_done")
	if _, statErr := os.Stat(briefingDone); os.IsNotExist(statErr) {
		orchestrator.RunPMBriefing(ctx, agents, bot)
		_ = os.WriteFile(briefingDone, []byte("done"), 0o644)
	}

	checkInStore, coordDB, err := createCoordinationStore(ctx, deps.DataDir)
	if err != nil {
		return err
	}
	defer func() { _ = coordDB.Close() }()
	_, _ = fmt.Fprint(out, tui.StepDone("Coordination DB ready"))

	monitor := createHealthMonitor()
	alerter := health.NewAlerter(monitor, bot, "triage")
	scheduler := orchestrator.NewScheduler(bot, monitor, alerter, orchestrator.SchedulerConfig{
		StandupInterval:   parseCronToInterval(cfg.Rituals.StandupCron),
		HealthInterval:    5 * time.Minute,
		RetroAfterTickets: cfg.Rituals.RetroAfterTickets,
	})

	pmAgent := agents[agent.RolePM]
	assigner := orchestrator.NewAssigner(pmAgent, cfg.Linear.TeamID)

	workEnabled := cfg.Linear.TeamID != ""

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Duration(cfg.Agents.CooldownSeconds) * time.Second,
			MaxParallel:      cfg.Agents.MaxParallel,
			CooldownAfter:    time.Duration(cfg.Agents.CooldownSeconds) * time.Second,
			WorkEnabled:      workEnabled,
			TargetRepoDir:    targetRepoDir,
			MemoryBinaryPath: resolveMemoryBinaryPath(),
			DiscussionWait:   20 * time.Second,
			Links:            buildLinkConfig(cfg),
		},
		agents, checkInStore, bot, assigner,
	)

	orch.SetHealthMonitor(monitor)

	pipelineStore := pipeline.NewWorkItemStore(coordDB)
	if pipeErr := pipelineStore.InitSchema(ctx); pipeErr != nil {
		return fmt.Errorf("initialising pipeline: %w", pipeErr)
	}
	orch.SetPipeline(pipelineStore)

	handoffStore := pipeline.NewHandoffStore(coordDB)
	if handoffErr := handoffStore.InitSchema(ctx); handoffErr != nil {
		return fmt.Errorf("initialising handoffs: %w", handoffErr)
	}
	orch.SetHandoffStore(handoffStore)

	projectEpisodeStore := memory.NewEpisodeStore(projectDB)
	orch.SetProjectEpisodeStore(projectEpisodeStore)

	projectFactStore := memory.NewFactStore(projectDB)
	orch.SetProjectFactStore(projectFactStore)

	concerns := orchestrator.NewConcernTracker()
	orch.SetConcernTracker(concerns)

	// Situation queue + escalation for PM management.
	situations, escalations := wireSituations()
	orch.SetSituationQueue(situations)
	orch.SetEscalationTracker(escalations)

	// Specialisation tracking for intelligent assignment.
	specStore := wireSpecialisation(ctx, coordDB)
	if specStore != nil {
		orch.SetSpecialisationStore(specStore)
	}

	// Smart dispatch: direct Linear queries + dependency/priority/skill filtering.
	if slackSecrets.LinearAPIKey != "" {
		assigner.SetLinearAPIKey(slackSecrets.LinearAPIKey)
		assigner.SetSmartAssigner(orchestrator.NewSmartAssigner(pipelineStore))
		_ = os.Setenv("LINEAR_API_KEY", slackSecrets.LinearAPIKey)
		_, _ = fmt.Fprint(out, tui.StepDone("Smart dispatch enabled"))
	}

	eventBus := orchestrator.NewEventBus()
	orch.RegisterDefaultHandlers(eventBus)
	orch.SetEventBus(eventBus)

	if !workEnabled {
		_, _ = fmt.Fprint(out, tui.StepWarn("Linear not configured — agents will chat but not work"))
	}

	agentFactStores := make(map[agent.Role]*memory.FactStore, len(agentDBs))
	for role, db := range agentDBs {
		agentFactStores[role] = memory.NewFactStore(db)
	}

	// Wire intelligent routing, opinions, and budget.
	orch.SetComplexityClassifier(wireRouting(cfg))
	orch.SetOpinionStore(wireOpinions(agentFactStores))
	orch.SetTokenLedger(wireBudget(cfg.Agents.Budget))

	personas := personaStore.LoadAllPersonas(ctx)
	roster := make(map[agent.Role]string, len(personas))
	for role, persona := range personas {
		roster[role] = persona.Name
	}

	// Wire roster to console writer so logs show agent names.
	stringRoster := make(map[string]string, len(roster))
	for role, name := range roster {
		stringRoster[string(role)] = name
	}
	consoleWriter.SetRoster(stringRoster)

	alerter.SetRoster(roster)
	scheduler.SetPipeline(pipelineStore)
	scheduler.SetAgents(agents)
	scheduler.SetRoster(roster)

	conversation := orchestrator.NewConversationEngine(agents, agentFactStores, bot, roster)
	conversation.SetProjectFactStore(projectFactStore)
	conversation.SetConcernTracker(concerns)
	orch.SetConversationEngine(conversation)
	orch.SetRoster(roster)

	seedConversationHistory(ctx, bot, conversation, cfg)

	commandHandler := newCommandDispatcher(orch, bot, conversation, personas, buildLinkConfig(cfg))
	bot.OnMessage(commandHandler.handleMessage)

	for _, a := range agents {
		orch.WriteMCPConfigForTest(a, targetRepoDir)
	}
	configureGitHubAppToken(ctx, agents, deps.SecretLoader, out)

	_, _ = fmt.Fprintln(out, tui.StepDone("All systems ready"))
	appLogger.Info("system", "startup", "orchestrator starting")

	loop := deps.EventLoop
	if loop == nil {
		loop = runEventLoop
	}

	return loop(ctx, bot, scheduler, orch)
}

func runEventLoop(
	ctx context.Context,
	bot *slack.Bot,
	scheduler *orchestrator.Scheduler,
	orch *orchestrator.Orchestrator,
) error {
	errCh := make(chan error, 3)
	go func() { errCh <- bot.ListenForEvents(ctx) }()
	go func() { errCh <- scheduler.Run(ctx) }()
	go func() { errCh <- orch.Run(ctx) }()

	return <-errCh
}

func setupLogger(dataDir string, out io.Writer) (*logging.Logger, *logging.ConsoleWriter, error) {
	logDir := filepath.Join(dataDir, "logs")
	appLogger, err := logging.NewLogger(logDir)
	if err != nil {
		_, _ = fmt.Fprint(out, tui.StepFail("Logger failed"))
		return nil, nil, fmt.Errorf("creating logger: %w", err)
	}

	consoleWriter := logging.NewConsoleWriter(os.Stderr)
	log.SetOutput(consoleWriter)
	log.SetFlags(0)

	_, _ = fmt.Fprint(out, tui.StepDone("Logger started"))
	return appLogger, consoleWriter, nil
}

func loadSecrets(ctx context.Context, loader SecretLoader, out io.Writer) (secrets.Secrets, error) {
	slackSecrets, err := loader.LoadAll(ctx)
	if err != nil {
		_, _ = fmt.Fprint(out, tui.StepFail("Secrets missing"))
		return secrets.Secrets{}, fmt.Errorf("loading secrets: %w", err)
	}
	_, _ = fmt.Fprint(out, tui.StepDone("Secrets loaded"))
	return slackSecrets, nil
}

func openAllDatabases(ctx context.Context, dataDir string) (*memory.DB, map[agent.Role]*memory.DB, error) {
	agentDir := filepath.Join(dataDir, "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating data directories: %w", err)
	}

	projectDB, err := memory.Open(ctx, filepath.Join(dataDir, "project.db"))
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
	personalityDir string,
	dataDir string,
	targetRepoDir string,
	codexFallbackModel string,
) (map[agent.Role]*agent.Agent, error) {
	loader := agent.NewPersonalityLoader(personalityDir)
	runner := agent.ExecProcessRunner{}
	agents := make(map[agent.Role]*agent.Agent, len(agent.AllRoles()))

	for _, role := range agent.AllRoles() {
		agentDB, ok := agentDBs[role]
		if !ok {
			return nil, fmt.Errorf("no database for role %s", role)
		}

		newAgent := buildSingleAgent(role, agentDB, embedder, modelMap, loader, runner, codexFallbackModel)
		dbPath := filepath.Join(dataDir, "agents", string(role)+".db")
		newAgent.SetDBPath(dbPath)
		newAgent.SetDefaultWorkDir(targetRepoDir)
		agents[role] = newAgent
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
	codexModel string,
) *agent.Agent {
	graphStore := memory.NewGraphStore(agentDB)
	factStore := memory.NewFactStore(agentDB)
	episodeStore := memory.NewEpisodeStore(agentDB)
	ftsStore := memory.NewFTSStore(agentDB)
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)

	model := agent.ModelForRole(role, modelMap)
	session := agent.NewSession(runner)
	if codexModel != "" {
		session.SetCodexFallback(codexModel)
	}

	newAgent := agent.NewAgent(role, model, session, loader, retriever, agentDB, episodeStore, embedder)
	newAgent.SetMemoryStores(graphStore, factStore)

	return newAgent
}

func createPersonaStore(agentDBs map[agent.Role]*memory.DB) *slack.PersonaStore {
	graphStores := make(map[agent.Role]*memory.GraphStore, len(agentDBs))
	factStores := make(map[agent.Role]*memory.FactStore, len(agentDBs))

	for role, db := range agentDBs {
		graphStores[role] = memory.NewGraphStore(db)
		factStores[role] = memory.NewFactStore(db)
	}

	return slack.NewPersonaStore(graphStores, factStores)
}

func createSlackBot(
	ctx context.Context,
	cfg config.Config,
	slackSecrets secrets.Secrets,
	personaStore *slack.PersonaStore,
) *slack.Bot {
	personas := personaStore.LoadAllPersonas(ctx)

	return slack.NewBot(slack.BotConfig{
		BotToken:   slackSecrets.SlackBotToken,
		AppToken:   slackSecrets.SlackAppToken,
		Channels:   cfg.Slack.Channels,
		Personas:   personas,
		MinSpacing: 2 * time.Second,
	})
}

func createCoordinationStore(ctx context.Context, dataDir string) (*coordination.CheckInStore, *sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "coordination.db")
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

func createHealthMonitor() *health.Monitor {
	return health.NewMonitor(agent.AllRoles(), health.MonitorConfig{
		MaxIdleTime:          10 * time.Minute,
		MaxSessionTime:       30 * time.Minute,
		MaxConsecutiveErrors: 3,
	})
}

func ensureCodexMCP(ctx context.Context, out io.Writer) {
	runner := agent.ExecProcessRunner{}
	servers := agent.BuildCodexMCPServers(agent.MCPOptions{})
	if err := agent.EnsureCodexMCPServers(ctx, runner, servers); err != nil {
		_, _ = fmt.Fprint(out, tui.StepWarn(fmt.Sprintf("Codex MCP setup failed: %v", err)))
		return
	}
	_, _ = fmt.Fprint(out, tui.StepDone("Codex MCP servers registered"))
}

func resolveTargetRepo(targetRepo string) string {
	if targetRepo == "" {
		return ""
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	repoName := filepath.Base(targetRepo)
	return filepath.Join(home, "repos", repoName)
}
