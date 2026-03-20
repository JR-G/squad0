package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MCPServerConfig represents a single MCP server entry in .mcp.json.
type MCPServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
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

// BuildMCPConfig returns the MCP configuration for an agent session,
// including the Linear MCP server and the agent's personal memory server.
func BuildMCPConfig(opts MCPOptions) MCPConfig {
	servers := map[string]MCPServerConfig{
		"linear": {
			Command: "bunx",
			Args:    []string{"@linear/mcp-server"},
		},
	}

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
