package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// mcpServerStatus is one entry in the stream-json init payload's
// mcp_servers array.
type mcpServerStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// mcpInitMessage is the subset of the stream-json init payload we care
// about for the MCP health check. Fields outside this shape are
// ignored, which keeps the parser resilient to future additions.
type mcpInitMessage struct {
	Type       string            `json:"type"`
	SubType    string            `json:"subtype"`
	MCPServers []mcpServerStatus `json:"mcp_servers"`
}

// verifyMCPHealth is the startup smoke-test hook. Overridable so
// tests can skip the real claude subprocess — production uses
// realVerifyMCPHealth which actually spawns `claude -p`.
var verifyMCPHealth = realVerifyMCPHealth

// realVerifyMCPHealth spawns a one-shot claude subprocess using the
// PM agent's MCP config, parses the init line, and asserts that the
// two load-bearing MCP servers are both reachable:
//
//   - "claude.ai Linear" must be status=="connected". Without it
//     every ticket-state transition and Linear query falls on the
//     floor, producing the failure loop that has defined every
//     Linear-MCP outage in this project's history.
//   - "memory" must be status=="connected". A relative --db path or
//     a missing binary causes status=="failed" silently, and every
//     recall/store tool call errors out with an unhelpful message.
//
// If either server is unhealthy the error includes the exact status
// string claude reported so the operator can act on it instead of
// guessing. The prompt is the cheapest claude call we can make so the
// smoke test adds negligible startup latency.
func realVerifyMCPHealth(ctx context.Context, pmAgent *agent.Agent, model, workDir string) error {
	if pmAgent == nil {
		return fmt.Errorf("no PM agent to smoke-test MCP with")
	}
	if model == "" {
		return fmt.Errorf("PM model is empty — cannot spawn smoke-test subprocess")
	}
	if pmAgent.MCPConfigPath == "" {
		return fmt.Errorf("PM MCPConfigPath is empty — EnsureAgentMCPConfig did not run")
	}

	smokeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	args := []string{
		"-p",
		"--model", model,
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--mcp-config", pmAgent.MCPConfigPath,
	}

	cmd := exec.CommandContext(smokeCtx, "claude", args...)
	cmd.Stdin = strings.NewReader("ok")
	if workDir != "" {
		cmd.Dir = workDir
	}

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("running claude smoke test: %w", err)
	}

	init, found := parseMCPInit(string(output))
	if !found {
		return fmt.Errorf("no MCP init line in claude smoke-test output")
	}

	linear := findServer(init, "claude.ai Linear")
	if linear == nil {
		return fmt.Errorf("claude.ai Linear MCP not advertised — the managed connector is not enabled on this machine")
	}
	if linear.Status != "connected" {
		return fmt.Errorf("claude.ai Linear MCP status=%q, want \"connected\"", linear.Status)
	}

	memory := findServer(init, "memory")
	if memory == nil {
		return fmt.Errorf("memory MCP not advertised — check MemoryBinaryPath and EnsureAgentMCPConfig")
	}
	if memory.Status != "connected" {
		return fmt.Errorf("memory MCP status=%q, want \"connected\" (check --db path is absolute and the file is writable)", memory.Status)
	}

	return nil
}

func parseMCPInit(raw string) (mcpInitMessage, bool) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var msg mcpInitMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Type == "system" && msg.SubType == "init" {
			return msg, true
		}
	}
	return mcpInitMessage{}, false
}

func findServer(init mcpInitMessage, name string) *mcpServerStatus {
	for i := range init.MCPServers {
		if init.MCPServers[i].Name == name {
			return &init.MCPServers[i]
		}
	}
	return nil
}

// mcpSetupHint is appended to the smoke-test error so the operator
// knows exactly which one-time setup step fixes it, instead of
// reinventing the config generator for the sixth time.
func mcpSetupHint() string {
	return strings.Join([]string{
		"Linear MCP is a user-scope managed connector — it is NOT configured by squad0.",
		"Set it up once by running:",
		"  claude  # interactive, then /mcp to authorise claude.ai Linear",
		"or visit the Linear connector settings at claude.ai and approve access.",
		"Memory MCP is configured by squad0 (data/mcp/<role>/.mcp.json); if it is failing,",
		"check that bin/squad0-memory-mcp exists and the agent DB path is absolute.",
	}, "\n")
}
