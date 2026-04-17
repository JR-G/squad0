package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// linearSmokeTestPrompt forces Claude to exercise the full deferred-
// tool flow: ToolSearch → tool_use of the loaded tool. If either step
// fails we miss a tool_use block in the stream and the smoke test
// errors. The prompt intentionally uses list_teams (read-only, no
// side effects) so running the smoke test on startup never mutates
// the board.
const linearSmokeTestPrompt = `The Linear MCP tools are registered as *deferred* tools — their schemas must be loaded via ToolSearch before use.

Do this exactly:
1. Call ToolSearch with arguments: {"query": "select:mcp__claude_ai_Linear__list_teams", "max_results": 1}
2. Call mcp__claude_ai_Linear__list_teams with arguments: {}
3. Reply with just "ok".`

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
	Tools      []string          `json:"tools"`
}

// smokeContentBlock is the assistant-message content shape we care
// about — enough to spot a tool_use of a Linear MCP tool and a
// subsequent tool_result that signals an error.
type smokeContentBlock struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	IsError bool   `json:"is_error"`
}

// smokeStreamLine mirrors the subset of a stream-json line we inspect
// to confirm the deferred-tool flow succeeded.
type smokeStreamLine struct {
	Type    string `json:"type"`
	Message struct {
		Content []smokeContentBlock `json:"content"`
	} `json:"message"`
	Content []smokeContentBlock `json:"content"`
}

// verifyMCPHealth is the startup smoke-test hook. Overridable so
// tests can skip the real claude subprocess — production uses
// realVerifyMCPHealth which actually spawns `claude -p`.
var verifyMCPHealth = realVerifyMCPHealth

// realVerifyMCPHealth spawns a one-shot claude subprocess (NO
// --mcp-config — that flag suppresses managed-connector tool exposure
// even though the connector itself reports connected) and asserts:
//
//   - "claude.ai Linear" is connected AND its tools are exposed in
//     the session's tools list. The connection alone is not enough —
//     the smoke test caught this regression in the wild: status
//     reads "connected" while every ticket session errors with
//     "Linear MCP tools are not available". We assert tool presence
//     to catch that class of failure at startup.
//   - the user-scope memory MCP ("squad0-memory") is connected. The
//     SQUAD0_MEMORY_DB env var is set to the PM's DB so the smoke
//     test exercises the same env-driven path real sessions use.
//
// If either server is unhealthy the error includes the exact status
// string claude reported. The prompt is the cheapest claude call we
// can make so the smoke test adds negligible startup latency.
func realVerifyMCPHealth(ctx context.Context, pmAgent *agent.Agent, model, workDir string) error {
	if pmAgent == nil {
		return fmt.Errorf("no PM agent to smoke-test MCP with")
	}
	if model == "" {
		return fmt.Errorf("PM model is empty — cannot spawn smoke-test subprocess")
	}

	smokeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	args := []string{
		"-p",
		"--model", model,
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
	}

	cmd := exec.CommandContext(smokeCtx, "claude", args...)
	cmd.Stdin = strings.NewReader(linearSmokeTestPrompt)
	if workDir != "" {
		cmd.Dir = workDir
	}

	if dbPath := pmAgent.DBPath(); dbPath != "" {
		cmd.Env = append(os.Environ(), "SQUAD0_MEMORY_DB="+dbPath)
	}

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("running claude smoke test: %w", err)
	}

	raw := string(output)
	init, found := parseMCPInit(raw)
	if !found {
		return fmt.Errorf("no MCP init line in claude smoke-test output")
	}

	if err := assertLinearHealthy(init); err != nil {
		return err
	}
	if err := assertMemoryHealthy(init); err != nil {
		return err
	}
	return assertLinearToolInvoked(raw)
}

// assertLinearToolInvoked scans the stream-json transcript for a
// tool_use of a Linear MCP tool and confirms its tool_result was
// not an error. This catches the deferred-tool class of failure:
// init reports Linear connected and tools exposed, yet real sessions
// cannot actually call the tool because the schema isn't loaded.
func assertLinearToolInvoked(raw string) error {
	linearInvoked := false
	linearErrored := false

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var parsed smokeStreamLine
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			continue
		}

		blocks := parsed.Message.Content
		if len(blocks) == 0 {
			blocks = parsed.Content
		}

		for _, block := range blocks {
			if block.Type == "tool_use" && strings.HasPrefix(block.Name, "mcp__claude_ai_Linear__") {
				linearInvoked = true
			}
			if block.Type == "tool_result" && block.IsError {
				linearErrored = true
			}
		}
	}

	if !linearInvoked {
		return fmt.Errorf("smoke-test prompt asked for a Linear MCP call but no mcp__claude_ai_Linear__* tool_use appeared — deferred-tool loading is broken for this session")
	}
	if linearErrored {
		return fmt.Errorf("Linear MCP tool call returned an error during smoke test — sessions will not be able to use the tool")
	}
	return nil
}

// assertLinearHealthy checks both the connection state and that
// Linear tools are actually exposed in the session.
func assertLinearHealthy(init mcpInitMessage) error {
	linear := findServer(init, "claude.ai Linear")
	if linear == nil {
		return fmt.Errorf("claude.ai Linear MCP not advertised — the managed connector is not enabled on this machine")
	}
	if linear.Status != "connected" {
		return fmt.Errorf("claude.ai Linear MCP status=%q, want \"connected\"", linear.Status)
	}

	for _, name := range init.Tools {
		if strings.HasPrefix(name, "mcp__claude_ai_Linear__") {
			return nil
		}
	}
	return fmt.Errorf("claude.ai Linear is connected but no mcp__claude_ai_Linear__* tools are exposed — sessions will not see Linear (likely --mcp-config is being passed somewhere it shouldn't be)")
}

// assertMemoryHealthy accepts either the user-scope name
// (squad0-memory) or the legacy file-scope name (memory) so a
// half-migrated machine still surfaces a useful error.
func assertMemoryHealthy(init mcpInitMessage) error {
	for _, name := range []string{"squad0-memory", "memory"} {
		entry := findServer(init, name)
		if entry == nil {
			continue
		}
		if entry.Status != "connected" {
			return fmt.Errorf("memory MCP %q status=%q, want \"connected\" (check SQUAD0_MEMORY_DB env var and that the binary is on PATH)", name, entry.Status)
		}
		return nil
	}
	return fmt.Errorf("memory MCP not advertised — run startup again so squad0 can register it user-scope, or check that bin/squad0-memory-mcp exists")
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
// knows exactly which one-time setup step fixes it.
func mcpSetupHint() string {
	return strings.Join([]string{
		"Linear MCP is a user-scope managed connector — squad0 does not configure it.",
		"If it shows as not connected, set it up once by running:",
		"  claude  # interactive, then /mcp to authorise claude.ai Linear",
		"Memory MCP is now registered at user scope on every squad0 start.",
		"If it is failing, check that bin/squad0-memory-mcp exists and that",
		"`claude mcp list` shows squad0-memory pointing at the right binary.",
	}, "\n")
}
