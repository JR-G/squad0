package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/config"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/routing"
	"github.com/JR-G/squad0/internal/runtime"
	"github.com/JR-G/squad0/internal/tui"
)

// wireBridges creates a SessionBridge per agent based on RuntimeConfig
// and sets it on each agent. Agents without bridges work identically
// to before (fresh process per QuickChat).
func wireBridges(
	agents map[agent.Role]*agent.Agent,
	cfg config.RuntimeConfig,
	codexModel string,
	modelMap map[agent.Role]string,
	targetRepoDir string,
	_ string,
) {
	for role, agentInstance := range agents {
		model := modelMap[role]
		bridge := createBridgeForRole(role, cfg, codexModel, model, targetRepoDir)
		if bridge == nil {
			continue
		}
		agentInstance.SetBridge(bridge)
		log.Printf("runtime: %s using %s (model=%s, fallback=%s)", role, bridge.Active().Name(), model, cfg.Fallback)
	}
}

func createBridgeForRole(
	role agent.Role,
	cfg config.RuntimeConfig,
	codexModel, claudeModel, workDir string,
) *runtime.SessionBridge {
	activeName := cfg.Default
	if override, ok := cfg.Overrides[string(role)]; ok {
		activeName = override
	}

	runner := agent.ExecProcessRunner{}

	active := buildRuntime(activeName, role, runner, codexModel, claudeModel, workDir)
	if active == nil {
		return nil
	}

	var fallback runtime.Runtime
	if cfg.Fallback != "" && cfg.Fallback != activeName {
		fallback = buildRuntime(cfg.Fallback, role, runner, codexModel, claudeModel, workDir)
	}

	return runtime.NewSessionBridge(role, active, fallback)
}

func buildRuntime(
	name string,
	role agent.Role,
	runner agent.ProcessRunner, //nolint:unparam // varies in production via createBridgeForRole
	codexModel, claudeModel, workDir string,
) runtime.Runtime {
	switch name {
	case "claude":
		// Fresh process per interaction — proven, stable, and the
		// only Claude runtime squad0 supports. A prior "persistent
		// tmux" runtime was deleted as dead code; if you need to
		// bring it back, reintroduce it deliberately, not as an
		// opportunistic fix.
		session := agent.NewSession(runner)
		if codexModel != "" {
			session.SetCodexFallback(codexModel)
		}
		return runtime.NewClaudeProcessRuntime(session, claudeModel, workDir)
	case "codex":
		return runtime.NewCodexRuntime(runner, codexModel, workDir)
	default:
		log.Printf("runtime: unknown runtime %q for %s", name, role)
		return nil
	}
}

// wireRouting creates the ComplexityClassifier and wires it into the
// assigner for semantic model routing and adaptive discussion depth.
func wireRouting(cfg config.Config) *routing.ComplexityClassifier {
	return routing.NewComplexityClassifier(
		"claude-haiku-4-5-20251001",
		cfg.Agents.Models.Engineer,
		cfg.Agents.Models.TechLead,
	)
}

// wireSpecialisation creates the SpecialisationStore on the coordination
// DB and returns it for the orchestrator.
func wireSpecialisation(ctx context.Context, coordDB *sql.DB) *routing.SpecialisationStore {
	store := routing.NewSpecialisationStore(coordDB)
	if err := store.InitSchema(ctx); err != nil {
		log.Printf("specialisation store init failed: %v", err)
		return nil
	}
	return store
}

// wireOpinions creates the OpinionStore from agent fact stores.
func wireOpinions(agentFactStores map[agent.Role]*memory.FactStore) *routing.OpinionStore {
	return routing.NewOpinionStore(agentFactStores)
}

// wireBudget creates the TokenLedger from budget config.
func wireBudget(cfg config.BudgetConfig) *routing.TokenLedger {
	return routing.NewTokenLedger(cfg.MaxTokensPerTicket, cfg.MaxTokensPerAgentDay)
}

// wireSituations creates the situation queue and escalation tracker.
func wireSituations() (*orchestrator.SituationQueue, *orchestrator.EscalationTracker) {
	return orchestrator.NewSituationQueue(), orchestrator.NewEscalationTracker()
}

func buildLinkConfig(cfg config.Config) slack.LinkConfig {
	repo := ""
	if cfg.Project.TargetRepo != "" {
		repo = filepath.Base(cfg.Project.TargetRepo)
	}

	return slack.LinkConfig{
		LinearWorkspace: cfg.Linear.Workspace,
		GitHubOwner:     cfg.GitHub.Owner,
		GitHubRepo:      repo,
	}
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

// wireAgentMCP writes each agent's .mcp.json (memory MCP only — the
// Linear connector is user-scope, not configured by squad0) and then
// runs the startup MCP smoke test. Must happen BEFORE wireBridges so
// agents have MCPConfigPath populated when their runtime spawns
// claude. Returns an error with an actionable setup hint if the
// smoke test trips — never silently boot with a broken MCP.
func wireAgentMCP(
	ctx context.Context,
	out io.Writer,
	agents map[agent.Role]*agent.Agent,
	modelMap map[agent.Role]string,
	dataDir, targetRepoDir string,
) error {
	memoryBinaryPath := resolveMemoryBinaryPath()
	mcpBaseDir := filepath.Join(dataDir, "mcp")
	for _, agentInstance := range agents {
		orchestrator.EnsureAgentMCPConfig(agentInstance, mcpBaseDir, memoryBinaryPath)
	}

	if err := verifyMCPHealth(ctx, agents[agent.RolePM], modelMap[agent.RolePM], targetRepoDir); err != nil {
		return fmt.Errorf("MCP smoke test: %w\n\n%s", err, mcpSetupHint())
	}
	_, _ = fmt.Fprint(out, tui.StepDone("MCP servers verified (Linear connected, memory connected)"))
	return nil
}

func resolveMemoryBinaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	candidate := filepath.Join(filepath.Dir(exe), "squad0-memory-mcp")
	if _, statErr := os.Stat(candidate); statErr == nil {
		return candidate
	}

	return ""
}
