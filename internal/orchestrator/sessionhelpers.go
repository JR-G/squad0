package orchestrator

import (
	"context"
	"log"
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

// WriteMCPConfigForTest exports writeMCPConfig for testing.
func (orch *Orchestrator) WriteMCPConfigForTest(agentInstance *agent.Agent, workDir string) {
	orch.writeMCPConfig(agentInstance, workDir)
}

// AgentFactStoresForTest exports agentFactStores for testing.
func (orch *Orchestrator) AgentFactStoresForTest() map[agent.Role]*memory.FactStore {
	return orch.agentFactStores()
}

// writeMCPConfig writes the .mcp.json to the session working directory
// and sets the agent's MCPConfigPath so the Claude Code process can
// find it.
func (orch *Orchestrator) writeMCPConfig(agentInstance *agent.Agent, workDir string) {
	mcpCfg := agent.BuildMCPConfig(agent.MCPOptions{
		MemoryBinaryPath: orch.cfg.MemoryBinaryPath,
		AgentDBPath:      agentInstance.DBPath(),
	})

	if err := agent.WriteMCPConfig(workDir, mcpCfg); err != nil {
		log.Printf("failed to write MCP config for %s: %v", agentInstance.Role(), err)
		return
	}

	agentInstance.MCPConfigPath = filepath.Join(workDir, ".mcp.json")
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
