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

// DefaultMCPConfig returns the standard MCP configuration for agent
// sessions, including the Linear MCP server.
func DefaultMCPConfig() MCPConfig {
	return MCPConfig{
		MCPServers: map[string]MCPServerConfig{
			"linear": {
				Command: "bunx",
				Args:    []string{"@linear/mcp-server"},
			},
		},
	}
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
