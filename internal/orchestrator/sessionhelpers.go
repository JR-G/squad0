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
	if orch.monitor == nil {
		return
	}
	orch.monitor.RecordSessionStart(role)
}

func (orch *Orchestrator) recordSessionEnd(role agent.Role, ticket string, success bool) {
	if orch.monitor == nil {
		return
	}
	orch.monitor.RecordSessionEnd(role, ticket, success)
}

// EnsureAgentMCPConfigForTest exports ensureAgentMCPConfig for testing.
func (orch *Orchestrator) EnsureAgentMCPConfigForTest(agentInstance *agent.Agent, baseDir string) {
	orch.ensureAgentMCPConfig(agentInstance, baseDir)
}

// AgentFactStoresForTest exports agentFactStores for testing.
func (orch *Orchestrator) AgentFactStoresForTest() map[agent.Role]*memory.FactStore {
	return orch.agentFactStores()
}

// ensureAgentMCPConfig writes a stable .mcp.json for the given agent
// under baseDir/<role>/.mcp.json and sets the agent's MCPConfigPath
// to the ABSOLUTE path. Called once per agent at startup. The file
// is never removed — it lives for the lifetime of the squad0
// process so that any subsequent session (runSession, DirectSession,
// fix-up, learnings flush) can read it with an absolute
// --mcp-config regardless of cwd. Agents run in worktrees and
// target-repo dirs where a relative path would resolve to the wrong
// place, so MCPConfigPath must be absolute.
func (orch *Orchestrator) ensureAgentMCPConfig(agentInstance *agent.Agent, baseDir string) {
	mcpCfg := agent.BuildMCPConfig(agent.MCPOptions{
		MemoryBinaryPath: orch.cfg.MemoryBinaryPath,
		AgentDBPath:      agentInstance.DBPath(),
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
