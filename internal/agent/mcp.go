package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MCPServerConfig represents a single MCP server entry in .mcp.json.
// Stdio servers set Command + Args; HTTP servers set Type + URL.
// Only one form is populated per entry — the omitempty tags ensure
// Claude Code sees exactly the shape it expects.
type MCPServerConfig struct {
	Type    string   `json:"type,omitempty"`
	URL     string   `json:"url,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

// MCPConfig represents the full .mcp.json file structure.
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// MCPOptions holds the paths needed to configure MCP servers for an
// agent session.
type MCPOptions struct {
	MemoryBinaryPath string
	AgentDBPath      string
}

// BuildMCPConfig returns the MCP configuration for an agent session.
//
// Linear MCP is DELIBERATELY NOT configured here. Claude Code ships
// a built-in "claude.ai Linear" managed MCP that is available to
// every `claude` subprocess for free, authenticated via the user's
// main OAuth token in ~/.claude/.credentials.json (scope
// user:mcp_servers). The tools come through as
// mcp__claude_ai_Linear__* — get_issue, save_issue, list_teams,
// list_issues, etc. — and the agent can use them without any
// per-agent authorisation flow.
//
// A previous implementation wrote a second Linear entry to
// .mcp.json pointing at the raw https://mcp.linear.app/mcp URL.
// That caused every spawned session to crash because Claude Code
// tried to OAuth the unmanaged endpoint, failed, and refused to
// start the session at all. The managed proxy is the only sane
// path on a user account.
//
// Only the per-agent memory MCP is written here, and only when the
// binary path and DB path are both set.
func BuildMCPConfig(opts MCPOptions) MCPConfig {
	servers := map[string]MCPServerConfig{}

	if opts.MemoryBinaryPath != "" && opts.AgentDBPath != "" {
		servers["memory"] = MCPServerConfig{
			Command: opts.MemoryBinaryPath,
			Args:    []string{"--db", opts.AgentDBPath},
		}
	}

	return MCPConfig{MCPServers: servers}
}

// DefaultMCPConfig returns a minimal MCP configuration with only the
// Linear MCP server. Use BuildMCPConfig for full configuration.
func DefaultMCPConfig() MCPConfig {
	return BuildMCPConfig(MCPOptions{})
}

// WriteMCPConfig writes the .mcp.json file to the given directory.
func WriteMCPConfig(dir string, cfg MCPConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling MCP config: %w", err)
	}

	path := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing MCP config to %s: %w", path, err)
	}

	return nil
}

// RemoveMCPConfig removes the .mcp.json file from the given directory.
func RemoveMCPConfig(dir string) error {
	path := filepath.Join(dir, ".mcp.json")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
