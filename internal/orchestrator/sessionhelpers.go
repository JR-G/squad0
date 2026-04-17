package orchestrator

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
)

// RecordSessionStartForTest exports recordSessionStart for testing.
func (orch *Orchestrator) RecordSessionStartForTest(role agent.Role) {
	orch.recordSessionStart(role)
}

// RecordSessionEndForTest exports recordSessionEnd for testing.
func (orch *Orchestrator) RecordSessionEndForTest(role agent.Role, ticket string, success bool) {
	orch.recordSessionEnd(role, ticket, success)
}

func (orch *Orchestrator) recordSessionStart(role agent.Role) {
	orch.health.RecordSessionStart(role)
}

func (orch *Orchestrator) recordSessionEnd(role agent.Role, ticket string, success bool) {
	orch.health.RecordSessionEnd(role, ticket, success)
}

// EnsureAgentMCPConfigForTest exports ensureAgentMCPConfig for testing.
func (orch *Orchestrator) EnsureAgentMCPConfigForTest(agentInstance *agent.Agent, baseDir string) {
	EnsureAgentMCPConfig(agentInstance, baseDir, orch.cfg.MemoryBinaryPath)
}

// AgentFactStoresForTest exports agentFactStores for testing.
func (orch *Orchestrator) AgentFactStoresForTest() map[agent.Role]*memory.FactStore {
	return orch.agentFactStores()
}

// EnsureAgentMCPConfig writes a stable .mcp.json for the given agent
// under baseDir/<role>/.mcp.json and sets the agent's MCPConfigPath
// to the ABSOLUTE path. Called once per agent at startup. The file
// is never removed — it lives for the lifetime of the squad0
// process so that any subsequent session (runSession, DirectSession,
// fix-up, learnings flush) can read it with an absolute
// --mcp-config regardless of cwd. Agents run in worktrees and
// target-repo dirs where a relative path would resolve to the wrong
// place, so MCPConfigPath must be absolute.
//
// The AgentDBPath inside the config is also made absolute here so
// the memory MCP server can find its SQLite file regardless of the
// subprocess's working directory. A prior bug stored a relative
// "data/agents/<role>.db" which failed to open whenever claude ran
// in the target repo or a worktree — the memory server silently
// reported status=failed in mcp_servers init and every memory tool
// call errored out.
func EnsureAgentMCPConfig(agentInstance *agent.Agent, baseDir, memoryBinaryPath string) {
	absDBPath, err := resolveAbsDBPath(agentInstance)
	if err != nil {
		log.Printf("failed to resolve DB path for %s: %v", agentInstance.Role(), err)
		return
	}

	mcpCfg := agent.BuildMCPConfig(agent.MCPOptions{
		MemoryBinaryPath: memoryBinaryPath,
		AgentDBPath:      absDBPath,
	})

	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		log.Printf("failed to resolve MCP base dir for %s: %v", agentInstance.Role(), err)
		return
	}

	agentDir := filepath.Join(absBase, string(agentInstance.Role()))
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		log.Printf("failed to create MCP config dir for %s: %v", agentInstance.Role(), err)
		return
	}

	if err := agent.WriteMCPConfig(agentDir, mcpCfg); err != nil {
		log.Printf("failed to write MCP config for %s: %v", agentInstance.Role(), err)
		return
	}

	agentInstance.MCPConfigPath = filepath.Join(agentDir, ".mcp.json")
}

func resolveAbsDBPath(agentInstance *agent.Agent) (string, error) {
	raw := agentInstance.DBPath()
	if raw == "" {
		return "", nil
	}
	return filepath.Abs(raw)
}

func (orch *Orchestrator) breakSilence(ctx context.Context) {
	if orch.conversation == nil {
		return
	}
	orch.conversation.BreakSilence(ctx)
}

// agentFactStores returns per-agent fact stores from the conversation
// engine. Used by the seance to pull cross-agent beliefs.
func (orch *Orchestrator) agentFactStores() map[agent.Role]*memory.FactStore {
	if orch.conversation == nil {
		return nil
	}
	return orch.conversation.FactStores()
}
