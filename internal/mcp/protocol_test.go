package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeHandler struct{}

func (handler *fakeHandler) HandleInitialize(id interface{}) mcp.JSONRPCResponse {
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0", ID: id,
		Result: map[string]string{"status": "ok"},
	}
}

func (handler *fakeHandler) HandleToolsList(id interface{}) mcp.JSONRPCResponse {
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0", ID: id,
		Result: map[string]interface{}{"tools": []string{}},
	}
}

func (handler *fakeHandler) HandleToolsCall(id interface{}, params mcp.ToolCallParams) mcp.JSONRPCResponse {
	return mcp.JSONRPCResponse{
		JSONRPC: "2.0", ID: id,
		Result: mcp.ToolResult{Content: []mcp.ToolContent{{Type: "text", Text: "called: " + params.Name}}},
	}
}

func TestServer_Run_Initialize_ReturnsResponse(t *testing.T) { //nolint:misspell // MCP protocol
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" //nolint:misspell // MCP protocol
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}

	server := mcp.NewServerWithIO(&fakeHandler{}, reader, writer)

	err := server.Run(context.Background())

	require.NoError(t, err)
	assertJSONContains(t, writer.String(), "ok")
}

func TestServer_Run_ToolsList_ReturnsTools(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}

	server := mcp.NewServerWithIO(&fakeHandler{}, reader, writer)
	_ = server.Run(context.Background())

	assertJSONContains(t, writer.String(), "tools")
}

func TestServer_Run_ToolsCall_DispatchesToHandler(t *testing.T) {
	t.Parallel()

	params := mcp.ToolCallParams{Name: "recall", Arguments: map[string]interface{}{"query": "test"}}
	paramsJSON, _ := json.Marshal(params)
	input := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":` + string(paramsJSON) + `}` + "\n"
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}

	server := mcp.NewServerWithIO(&fakeHandler{}, reader, writer)
	_ = server.Run(context.Background())

	assertJSONContains(t, writer.String(), "called: recall")
}

func TestServer_Run_UnknownMethod_ReturnsError(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":4,"method":"unknown/method"}` + "\n"
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}

	server := mcp.NewServerWithIO(&fakeHandler{}, reader, writer)
	_ = server.Run(context.Background())

	assertJSONContains(t, writer.String(), "method not found")
}

func TestServer_Run_InvalidJSON_ReturnsParseError(t *testing.T) {
	t.Parallel()

	input := "not json\n"
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}

	server := mcp.NewServerWithIO(&fakeHandler{}, reader, writer)
	_ = server.Run(context.Background())

	assertJSONContains(t, writer.String(), "parse error")
}

func TestServer_Run_InvalidToolCallParams_ReturnsError(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":"not an object"}` + "\n"
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}

	server := mcp.NewServerWithIO(&fakeHandler{}, reader, writer)
	_ = server.Run(context.Background())

	assertJSONContains(t, writer.String(), "invalid params")
}

func TestServer_Run_NotificationsInitialized_ReturnsEmptyResponse(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" //nolint:misspell // MCP protocol
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}

	server := mcp.NewServerWithIO(&fakeHandler{}, reader, writer)
	_ = server.Run(context.Background())

	assert.NotEmpty(t, writer.String())
}

func TestServer_Run_MultipleRequests_AllProcessed(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" + //nolint:misspell // MCP protocol
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}

	server := mcp.NewServerWithIO(&fakeHandler{}, reader, writer)
	_ = server.Run(context.Background())

	lines := strings.Split(strings.TrimSpace(writer.String()), "\n")
	assert.Len(t, lines, 2)
}

func assertJSONContains(t *testing.T, output, expected string) {
	t.Helper()
	assert.Contains(t, output, expected)
}
