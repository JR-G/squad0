package mcp_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowReader yields one line then blocks until the context is cancelled.
// After cancellation, the next Read returns io.EOF so the scanner exits.
type slowReader struct {
	lines   []string
	index   int
	ctx     context.Context
	yielded bool
}

func (reader *slowReader) Read(buf []byte) (int, error) {
	if reader.index >= len(reader.lines) {
		// Block until context cancelled, then return EOF.
		<-reader.ctx.Done()
		return 0, io.EOF
	}

	line := reader.lines[reader.index]
	reader.index++
	reader.yielded = true
	n := copy(buf, []byte(line))
	return n, nil
}

func TestServer_Run_ContextCancelled_ReturnsEarly(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	firstLine := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" //nolint:misspell // MCP protocol
	reader := &slowReader{
		lines: []string{firstLine},
		ctx:   ctx,
	}
	writer := &bytes.Buffer{}

	server := mcp.NewServerWithIO(&fakeHandler{}, reader, writer)

	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx)
	}()

	// Cancel context after the first line is processed.
	cancel()

	err := <-done
	// The server should exit — either nil (scanner sees EOF) or context error.
	if err != nil {
		assert.True(t, errors.Is(err, context.Canceled))
	}
}

// failWriter always returns an error on Write.
type failWriter struct{}

func (writer *failWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestServer_Run_WriteError_ReturnsError(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" //nolint:misspell // MCP protocol
	reader := strings.NewReader(input)

	server := mcp.NewServerWithIO(&fakeHandler{}, reader, &failWriter{})
	err := server.Run(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing response")
}

func TestServer_NewServer_UsesStdioByDefault(t *testing.T) {
	t.Parallel()

	// NewServer should produce a non-nil server that uses os.Stdin/Stdout
	// internally. We cannot easily test the IO fields from outside, but
	// we can verify the server is ready to run.
	server := mcp.NewServer(&fakeHandler{})
	require.NotNil(t, server)
}
