package cli

import (
	"context"
	"database/sql"
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
)

// wireBridges creates a SessionBridge per agent based on RuntimeConfig
// and sets it on each agent. Agents without bridges work identically
// to before (fresh process per QuickChat).
func wireBridges(
	agents map[agent.Role]*agent.Agent,
	cfg config.RuntimeConfig,
	codexModel string,
	dataDir string,
) {
	for role, agentInstance := range agents {
		bridge := createBridgeForRole(role, cfg, codexModel, dataDir)
		if bridge == nil {
			continue
		}
		agentInstance.SetBridge(bridge)
		log.Printf("runtime: %s using %s (fallback: %s)", role, bridge.Active().Name(), cfg.Fallback)
	}
}

func createBridgeForRole(
	role agent.Role,
	cfg config.RuntimeConfig,
	codexModel string,
	dataDir string,
) *runtime.SessionBridge {
	activeName := cfg.Default
	if override, ok := cfg.Overrides[string(role)]; ok {
		activeName = override
	}

	runner := agent.ExecProcessRunner{}
	inboxDir := filepath.Join(dataDir, "inbox", string(role))
	outboxDir := filepath.Join(dataDir, "outbox", string(role))

	active := buildRuntime(activeName, role, runner, codexModel, inboxDir, outboxDir)
	if active == nil {
		return nil
	}

	var fallback runtime.Runtime
	if cfg.Fallback != "" && cfg.Fallback != activeName {
		fallback = buildRuntime(cfg.Fallback, role, runner, codexModel, inboxDir, outboxDir)
	}

	return runtime.NewSessionBridge(role, active, fallback)
}

func buildRuntime(
	name string,
	role agent.Role,
	runner agent.ProcessRunner,
	codexModel string,
	inboxDir, outboxDir string,
) runtime.Runtime {
	switch name {
	case "claude":
		inbox, err := runtime.NewInbox(inboxDir, outboxDir)
		if err != nil {
			log.Printf("runtime: failed to create inbox for %s: %v", role, err)
			return nil
		}
		return runtime.NewClaudePersistentRuntime(
			runtime.ExecTmuxExecutor{},
			inbox,
			string(role),
			"", // model comes from agent config
			"", // workdir comes from session
		)
	case "codex":
		return runtime.NewCodexRuntime(runner, codexModel, "")
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
