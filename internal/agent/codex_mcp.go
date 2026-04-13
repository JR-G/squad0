package agent

import (
	"context"
	"fmt"
	"log"
)

// CodexMCPServer describes an MCP server to register with the Codex CLI.
type CodexMCPServer struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string // KEY=VALUE pairs passed via --env.
}

// EnsureCodexMCPServers registers MCP servers with the Codex CLI so
// fallback sessions have the same tools as Claude. Idempotent —
// removes then re-adds each server to pick up config changes.
func EnsureCodexMCPServers(ctx context.Context, runner ProcessRunner, servers []CodexMCPServer) error {
	for _, server := range servers {
		if err := registerCodexMCPServer(ctx, runner, server); err != nil {
			return fmt.Errorf("registering codex MCP server %s: %w", server.Name, err)
		}
	}
	return nil
}

func registerCodexMCPServer(ctx context.Context, runner ProcessRunner, server CodexMCPServer) error {
	// Remove first — idempotent, ignore errors (server might not exist).
	_, _ = runner.Run(ctx, "", "", "codex", "mcp", "remove", server.Name)

	args := []string{"mcp", "add", server.Name}
	for key, val := range server.Env {
		args = append(args, "--env", key+"="+val)
	}
	args = append(args, "--")
	args = append(args, server.Command)
	args = append(args, server.Args...)

	_, err := runner.Run(ctx, "", "", "codex", args...)
	if err != nil {
		return err
	}

	log.Printf("registered codex MCP server: %s", server.Name)
	return nil
}

// BuildCodexMCPServers returns the list of MCP servers that should be
// registered with Codex, matching what Claude gets via .mcp.json.
func BuildCodexMCPServers(opts MCPOptions) []CodexMCPServer {
	servers := []CodexMCPServer{
		{
			Name:    "linear",
			Command: "bunx",
			Args: []string{
				"-y",
				"mcp-remote",
				"https://mcp.linear.app/mcp",
				"--header",
				"Authorization:${LINEAR_AUTH_HEADER}",
			},
		},
	}

	if opts.MemoryBinaryPath != "" && opts.AgentDBPath != "" {
		servers = append(servers, CodexMCPServer{
			Name:    "memory",
			Command: opts.MemoryBinaryPath,
			Args:    []string{"--db", opts.AgentDBPath},
		})
	}

	return servers
}
