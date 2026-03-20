package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// JSONRPCRequest represents an incoming JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents an outgoing JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolDefinition describes a tool available via MCP.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ToolCallParams holds the parameters for a tools/call request.
type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolResult holds the result of a tool call.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent holds a single content block in a tool result.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Server handles MCP protocol communication over stdin/stdout.
type Server struct {
	handler RequestHandler
	reader  io.Reader
	writer  io.Writer
}

// RequestHandler processes MCP requests and returns responses.
type RequestHandler interface {
	HandleInitialize(id interface{}) JSONRPCResponse
	HandleToolsList(id interface{}) JSONRPCResponse
	HandleToolsCall(id interface{}, params ToolCallParams) JSONRPCResponse
}

// NewServer creates an MCP Server with the given handler.
func NewServer(handler RequestHandler) *Server {
	return &Server{
		handler: handler,
		reader:  os.Stdin,
		writer:  os.Stdout,
	}
}

// NewServerWithIO creates an MCP Server with custom IO for testing.
func NewServerWithIO(handler RequestHandler, reader io.Reader, writer io.Writer) *Server {
	return &Server{
		handler: handler,
		reader:  reader,
		writer:  writer,
	}
}

// Run starts the MCP server, reading requests from stdin and writing
// responses to stdout. Blocks until the context is cancelled or stdin
// is closed.
func (server *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(server.reader)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		response := server.handleLine(line)
		if response == nil {
			continue
		}

		if err := server.writeResponse(*response); err != nil {
			return fmt.Errorf("writing response: %w", err)
		}
	}

	return scanner.Err()
}

func (server *Server) handleLine(line string) *JSONRPCResponse {
	var request JSONRPCRequest
	if err := json.Unmarshal([]byte(line), &request); err != nil {
		resp := errorResponse(nil, -32700, "parse error")
		return &resp
	}

	response := server.dispatch(request)
	return &response
}

func (server *Server) dispatch(request JSONRPCRequest) JSONRPCResponse {
	switch request.Method {
	case "initialize": //nolint:misspell // MCP protocol uses American spelling
		return server.handler.HandleInitialize(request.ID)
	case "tools/list":
		return server.handler.HandleToolsList(request.ID)
	case "tools/call":
		return server.dispatchToolsCall(request)
	case "notifications/initialized": //nolint:misspell // MCP protocol uses American spelling
		return JSONRPCResponse{JSONRPC: "2.0", ID: request.ID}
	default:
		return errorResponse(request.ID, -32601, fmt.Sprintf("method not found: %s", request.Method))
	}
}

func (server *Server) dispatchToolsCall(request JSONRPCRequest) JSONRPCResponse {
	var params ToolCallParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return errorResponse(request.ID, -32602, "invalid params")
	}

	return server.handler.HandleToolsCall(request.ID, params)
}

func (server *Server) writeResponse(response JSONRPCResponse) error {
	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshalling response: %w", err)
	}

	_, err = fmt.Fprintf(server.writer, "%s\n", data)
	return err
}

func errorResponse(id interface{}, code int, message string) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}
