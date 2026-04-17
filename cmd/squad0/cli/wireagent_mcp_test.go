package cli_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWireAgentMCP_MemoryErr_HardFails(t *testing.T) {
	restore := cli.StubVerifyMCPHealthWithResult(cli.MCPHealthResult{
		MemoryErr: errors.New("memory boom"),
	})
	t.Cleanup(restore)

	var out bytes.Buffer
	err := cli.WireAgentMCP(context.Background(), &out, nil, nil, t.TempDir(), t.TempDir(), true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "memory MCP unhealthy")
}

func TestWireAgentMCP_OverallErr_HardFails(t *testing.T) {
	restore := cli.StubVerifyMCPHealthWithResult(cli.MCPHealthResult{
		OverallErr: errors.New("subprocess crashed"),
	})
	t.Cleanup(restore)

	var out bytes.Buffer
	err := cli.WireAgentMCP(context.Background(), &out, nil, nil, t.TempDir(), t.TempDir(), true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subprocess crashed")
}

func TestWireAgentMCP_LinearErr_NoAPI_HardFails(t *testing.T) {
	restore := cli.StubVerifyMCPHealthWithResult(cli.MCPHealthResult{
		LinearErr: errors.New("linear boom"),
	})
	t.Cleanup(restore)

	var out bytes.Buffer
	err := cli.WireAgentMCP(context.Background(), &out, nil, nil, t.TempDir(), t.TempDir(), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no LINEAR_API_KEY fallback")
}

func TestWireAgentMCP_LinearErr_WithAPI_WarnsAndContinues(t *testing.T) {
	restore := cli.StubVerifyMCPHealthWithResult(cli.MCPHealthResult{
		LinearErr: errors.New("linear tools not exposed"),
	})
	t.Cleanup(restore)

	var out bytes.Buffer
	err := cli.WireAgentMCP(context.Background(), &out, nil, nil, t.TempDir(), t.TempDir(), true)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "degraded")
}

func TestWireAgentMCP_AllHealthy_ReportsSuccess(t *testing.T) {
	restore := cli.StubVerifyMCPHealthWithResult(cli.MCPHealthResult{})
	t.Cleanup(restore)

	var out bytes.Buffer
	err := cli.WireAgentMCP(context.Background(), &out, nil, nil, t.TempDir(), t.TempDir(), true)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "MCP servers verified")
}
