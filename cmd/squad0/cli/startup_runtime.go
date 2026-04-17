package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

// wireAgentMCP registers the memory MCP at user scope so every claude
// subprocess sees it alongside the managed claude.ai Linear connector,
// then runs the startup smoke test. Per-agent DB selection happens at
// spawn time via the SQUAD0_MEMORY_DB env var; the MCP binary reads
// that env var to pick which agent's database to open.
//
// User scope (rather than per-agent --mcp-config) is load-bearing:
// passing --mcp-config to `claude -p` causes the managed Linear
// connector's tools to be excluded from the session even though the
// connector itself reports status=connected. With no --mcp-config and
// memory registered user-scope, every spawn inherits the full set
// (Linear + memory) the way the interactive `claude` does.
func wireAgentMCP(
	ctx context.Context,
	out io.Writer,
	agents map[agent.Role]*agent.Agent,
	modelMap map[agent.Role]string,
	_ /* dataDir */, targetRepoDir string,
	linearAPIKey string,
) error {
	registerMemoryMCP(ctx, out)
	registerLinearMCP(ctx, out, linearAPIKey)

	result := verifyMCPHealth(ctx, agents[agent.RolePM], modelMap[agent.RolePM], targetRepoDir)

	if result.OverallErr != nil {
		return fmt.Errorf("MCP smoke test: %w\n\n%s", result.OverallErr, mcpSetupHint())
	}

	if result.MemoryErr != nil {
		return fmt.Errorf("MCP smoke test: memory MCP unhealthy: %w\n\n%s", result.MemoryErr, mcpSetupHint())
	}

	if result.LinearErr != nil {
		return handleLinearErr(out, result.LinearErr, linearAPIKey != "")
	}

	_, _ = fmt.Fprint(out, tui.StepDone("MCP servers verified (Linear connected, memory connected)"))
	return nil
}

func handleLinearErr(out io.Writer, linearErr error, linearAPIConfigured bool) error {
	if !linearAPIConfigured {
		return fmt.Errorf("MCP smoke test: Linear MCP unhealthy and no LINEAR_API_KEY fallback: %w\n\n%s", linearErr, mcpSetupHint())
	}
	_, _ = fmt.Fprint(out, tui.StepWarn(fmt.Sprintf("Linear MCP degraded (%v) — using direct GraphQL API for ticket operations", linearErr)))
	return nil
}

// registerLinearMCP wires up squad0-linear at user scope, giving
// sessions a stable Linear toolset that doesn't depend on the
// OAuth-managed claude.ai connector. Warns and continues on any
// issue — squad0 can still operate via the direct GraphQL path in
// MoveLinearTicketStateAPI.
func registerLinearMCP(ctx context.Context, out io.Writer, apiKey string) {
	if apiKey == "" {
		_, _ = fmt.Fprint(out, tui.StepWarn("LINEAR_API_KEY not configured — squad0-linear MCP will not register"))
		return
	}
	binaryPath := resolveLinearBinaryPath()
	if binaryPath == "" {
		_, _ = fmt.Fprint(out, tui.StepWarn("squad0-linear-mcp binary not found next to squad0 — Linear MCP will not register"))
		return
	}
	if err := ensureUserScopeLinearMCP(ctx, binaryPath, apiKey); err != nil {
		_, _ = fmt.Fprint(out, tui.StepWarn(fmt.Sprintf("user-scope Linear MCP registration failed: %v", err)))
		return
	}
	_, _ = fmt.Fprint(out, tui.StepDone("Linear MCP registered (user scope)"))
}

// registerMemoryMCP wires up squad0-memory at user scope, surfacing
// any issue as a TUI warning instead of failing the whole startup —
// the smoke test will still trip if Linear/memory aren't reachable.
func registerMemoryMCP(ctx context.Context, out io.Writer) {
	memoryBinaryPath := resolveMemoryBinaryPath()
	if memoryBinaryPath == "" {
		_, _ = fmt.Fprint(out, tui.StepWarn("squad0-memory-mcp binary not found next to squad0 — memory MCP will not register"))
		return
	}
	if err := ensureUserScopeMemoryMCP(ctx, memoryBinaryPath); err != nil {
		_, _ = fmt.Fprint(out, tui.StepWarn(fmt.Sprintf("user-scope memory MCP registration failed: %v", err)))
		return
	}
	_, _ = fmt.Fprint(out, tui.StepDone("Memory MCP registered (user scope)"))
}

// claudeMCPRunner runs `claude mcp …` subcommands. Exposed as an
// interface so tests can drive ensureUserScopeMemoryMCP without
// spawning a real claude binary.
type claudeMCPRunner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type execClaudeMCPRunner struct{}

func (execClaudeMCPRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "claude", args...).CombinedOutput()
}

// ensureUserScopeMemoryMCP makes sure `claude mcp` knows about
// `squad0-memory`. Idempotent: lists current registrations first and
// re-adds so the command path is always current after rebuilds.
func ensureUserScopeMemoryMCP(ctx context.Context, binaryPath string) error {
	return ensureUserScopeMemoryMCPWith(ctx, execClaudeMCPRunner{}, binaryPath)
}

func ensureUserScopeMemoryMCPWith(ctx context.Context, runner claudeMCPRunner, binaryPath string) error {
	listOutput, _ := runner.Run(ctx, "mcp", "list")
	if strings.Contains(string(listOutput), "squad0-memory:") {
		_, _ = runner.Run(ctx, "mcp", "remove", "squad0-memory", "--scope", "user")
	}

	if output, err := runner.Run(ctx, "mcp", "add", "--scope", "user", "squad0-memory", binaryPath); err != nil {
		return fmt.Errorf("claude mcp add: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// ensureUserScopeLinearMCP registers squad0-linear at user scope with
// LINEAR_API_KEY injected at spawn time. Avoiding --mcp-config is
// still load-bearing (the managed Linear connector only exposes its
// tools to sessions without --mcp-config); user-scope registration
// is the supported way to add our own stdio MCP alongside it.
func ensureUserScopeLinearMCP(ctx context.Context, binaryPath, apiKey string) error {
	return ensureUserScopeLinearMCPWith(ctx, execClaudeMCPRunner{}, binaryPath, apiKey)
}

func ensureUserScopeLinearMCPWith(ctx context.Context, runner claudeMCPRunner, binaryPath, apiKey string) error {
	listOutput, _ := runner.Run(ctx, "mcp", "list")
	if strings.Contains(string(listOutput), "squad0-linear:") {
		_, _ = runner.Run(ctx, "mcp", "remove", "squad0-linear", "--scope", "user")
	}

	args := []string{"mcp", "add", "--scope", "user", "--env", "LINEAR_API_KEY=" + apiKey, "squad0-linear", binaryPath}
	if output, err := runner.Run(ctx, args...); err != nil {
		return fmt.Errorf("claude mcp add: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func resolveMemoryBinaryPath() string {
	return resolveSiblingBinary("squad0-memory-mcp")
}

func resolveLinearBinaryPath() string {
	return resolveSiblingBinary("squad0-linear-mcp")
}

func resolveSiblingBinary(name string) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	candidate := filepath.Join(filepath.Dir(exe), name)
	if _, statErr := os.Stat(candidate); statErr == nil {
		return candidate
	}

	return ""
}
