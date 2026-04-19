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
	err := cli.WireAgentMCP(context.Background(), &out, nil, nil, t.TempDir(), t.TempDir(), "lin_secret")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "memory MCP unhealthy")
}

func TestWireAgentMCP_OverallErr_HardFails(t *testing.T) {
	restore := cli.StubVerifyMCPHealthWithResult(cli.MCPHealthResult{
		OverallErr: errors.New("subprocess crashed"),
	})
	t.Cleanup(restore)

	var out bytes.Buffer
	err := cli.WireAgentMCP(context.Background(), &out, nil, nil, t.TempDir(), t.TempDir(), "lin_secret")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subprocess crashed")
}

func TestWireAgentMCP_LinearErr_NoAPI_HardFails(t *testing.T) {
	restore := cli.StubVerifyMCPHealthWithResult(cli.MCPHealthResult{
		LinearErr: errors.New("linear boom"),
	})
	t.Cleanup(restore)

	var out bytes.Buffer
	err := cli.WireAgentMCP(context.Background(), &out, nil, nil, t.TempDir(), t.TempDir(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no LINEAR_API_KEY fallback")
}

func TestWireAgentMCP_LinearErr_WithAPI_WarnsAndContinues(t *testing.T) {
	restore := cli.StubVerifyMCPHealthWithResult(cli.MCPHealthResult{
		LinearErr: errors.New("linear tools not exposed"),
	})
	t.Cleanup(restore)

	var out bytes.Buffer
	err := cli.WireAgentMCP(context.Background(), &out, nil, nil, t.TempDir(), t.TempDir(), "lin_secret")
	require.NoError(t, err)
	assert.Contains(t, out.String(), "degraded")
}

func TestWireAgentMCP_AllHealthy_ReportsSuccess(t *testing.T) {
	restore := cli.StubVerifyMCPHealthWithResult(cli.MCPHealthResult{})
	t.Cleanup(restore)

	var out bytes.Buffer
	err := cli.WireAgentMCP(context.Background(), &out, nil, nil, t.TempDir(), t.TempDir(), "lin_secret")
	require.NoError(t, err)
	assert.Contains(t, out.String(), "MCP servers verified")
}

func TestRegisterLinearMCP_NoAPIKey_WarnsAndReturns(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cli.RegisterLinearMCP(context.Background(), &out, "")
	assert.Contains(t, out.String(), "LINEAR_API_KEY not configured")
}

func TestRegisterLinearMCP_WithAPIKey_AttemptsRegistration(t *testing.T) {
	t.Parallel()
	// With an API key but no real claude binary in test env, the
	// underlying ensureUserScopeLinearMCP shells out to claude and
	// likely fails. Either way the path is exercised — we only care
	// that it doesn't panic and produces some output.
	var out bytes.Buffer
	cli.RegisterLinearMCP(context.Background(), &out, "test-key")
	assert.NotEmpty(t, out.String())
}
