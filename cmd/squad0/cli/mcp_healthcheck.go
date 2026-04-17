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
// the board. Prefers our own squad0-linear MCP (stable, API-key
// auth, no OAuth expiry) and falls back to the managed claude.ai
// connector if our stdio server isn't available.
const linearSmokeTestPrompt = `Call a Linear tool to confirm the integration is live.

Pick whichever tool name is exposed in this session:
  - mcp__squad0_linear__list_teams (preferred — squad0's own server)
  - mcp__claude_ai_Linear__list_teams (fallback — managed connector)

If the tool is deferred (not pre-loaded), first load its schema with
ToolSearch, for example:
  ToolSearch({"query": "select:mcp__squad0_linear__list_teams", "max_results": 1})

Then call the tool with arguments: {}
Then reply with just "ok".`

const statusConnected = "connected"

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

// MCPHealthResult splits smoke-test failures by component so the
// caller can treat them independently: a broken memory MCP must hard-
// fail startup (no fallback), but a broken Linear MCP is tolerable
// when the GraphQL API path is available.
type MCPHealthResult struct {
	LinearErr  error
	MemoryErr  error
	OverallErr error // Transport-level failures (no init, subprocess error).
}

// HasIssues reports whether any check failed.
func (result MCPHealthResult) HasIssues() bool {
	return result.LinearErr != nil || result.MemoryErr != nil || result.OverallErr != nil
}

// realVerifyMCPHealth spawns a one-shot claude subprocess (NO
// --mcp-config — that flag suppresses managed-connector tool exposure
// even though the connector itself reports connected) and returns a
// split result: Linear-side issues and Memory-side issues separately.
// The caller decides which are fatal — Linear is optional when the
// GraphQL API path is configured, Memory never is.
func realVerifyMCPHealth(ctx context.Context, pmAgent *agent.Agent, model, workDir string) MCPHealthResult {
	if pmAgent == nil {
		return MCPHealthResult{OverallErr: fmt.Errorf("no PM agent to smoke-test MCP with")}
	}
	if model == "" {
		return MCPHealthResult{OverallErr: fmt.Errorf("PM model is empty — cannot spawn smoke-test subprocess")}
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
		return MCPHealthResult{OverallErr: fmt.Errorf("running claude smoke test: %w", err)}
	}

	raw := string(output)
	init, found := parseMCPInit(raw)
	if !found {
		return MCPHealthResult{OverallErr: fmt.Errorf("no MCP init line in claude smoke-test output")}
	}

	result := MCPHealthResult{
		LinearErr: assertLinearHealthy(init),
		MemoryErr: assertMemoryHealthy(init),
	}

	if result.LinearErr == nil {
		result.LinearErr = assertLinearToolInvoked(raw)
	}

	return result
}

// isLinearToolName returns true if the tool name belongs to either
// the squad0 stdio Linear MCP or the managed claude.ai connector.
func isLinearToolName(name string) bool {
	return strings.HasPrefix(name, "mcp__squad0_linear__") ||
		strings.HasPrefix(name, "mcp__claude_ai_Linear__")
}

// assertLinearToolInvoked scans the stream-json transcript for a
// tool_use of a Linear MCP tool (either our stdio server or the
// managed connector) and confirms its tool_result was not an error.
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
			if block.Type == "tool_use" && isLinearToolName(block.Name) {
				linearInvoked = true
			}
			if block.Type == "tool_result" && block.IsError {
				linearErrored = true
			}
		}
	}

	if !linearInvoked {
		return fmt.Errorf("smoke-test prompt asked for a Linear MCP call but no tool_use appeared — neither squad0-linear nor the managed connector responded")
	}
	if linearErrored {
		return fmt.Errorf("Linear MCP tool call returned an error during smoke test — sessions will not be able to use the tool")
	}
	return nil
}

// assertLinearHealthy accepts any healthy Linear path: squad0-linear
// (our stdio server) OR the managed claude.ai connector. At least
// one must be connected with tools exposed.
func assertLinearHealthy(init mcpInitMessage) error {
	if err := assertSquad0LinearHealthy(init); err == nil {
		return nil
	}
	if err := assertManagedLinearHealthy(init); err == nil {
		return nil
	}
	return fmt.Errorf("no Linear MCP is healthy: neither squad0-linear nor claude.ai Linear is connected with tools exposed")
}

func assertSquad0LinearHealthy(init mcpInitMessage) error {
	entry := findServer(init, "squad0-linear")
	if entry == nil {
		return fmt.Errorf("squad0-linear not advertised")
	}
	if entry.Status != statusConnected {
		return fmt.Errorf("squad0-linear status=%q", entry.Status)
	}
	for _, name := range init.Tools {
		if strings.HasPrefix(name, "mcp__squad0_linear__") {
			return nil
		}
	}
	return fmt.Errorf("squad0-linear connected but no tools exposed")
}

func assertManagedLinearHealthy(init mcpInitMessage) error {
	entry := findServer(init, "claude.ai Linear")
	if entry == nil {
		return fmt.Errorf("claude.ai Linear MCP not advertised")
	}
	if entry.Status != statusConnected {
		return fmt.Errorf("claude.ai Linear MCP status=%q", entry.Status)
	}
	for _, name := range init.Tools {
		if strings.HasPrefix(name, "mcp__claude_ai_Linear__") {
			return nil
		}
	}
	return fmt.Errorf("claude.ai Linear connected but no tools exposed")
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
		if entry.Status != statusConnected {
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
