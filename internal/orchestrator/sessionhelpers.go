package orchestrator

import (
	"log"
	"path/filepath"

	"github.com/JR-G/squad0/internal/agent"
)

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
