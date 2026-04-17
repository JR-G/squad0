package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRealMain_NoAPIKey_Exits1WithMessage(t *testing.T) {
	t.Parallel()

	var errBuf bytes.Buffer
	code := realMain("", &errBuf)

	assert.Equal(t, 1, code)
	assert.Contains(t, errBuf.String(), "LINEAR_API_KEY not set")
}

func TestRealMain_WithAPIKey_RunsUntilStdinEOF(t *testing.T) {
	t.Parallel()

	// Pipe a closed stdin so server.Run returns immediately rather
	// than blocking forever. This exercises the happy path of realMain.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = writer.Close()
		_ = reader.Close()
	})
	_ = writer.Close() // immediate EOF

	var errBuf bytes.Buffer
	code := realMain("test-key", &errBuf)
	assert.Equal(t, 0, code)
}

func TestEnvAPIKeyConstant(t *testing.T) {
	t.Parallel()
	assert.True(t, strings.Contains(envAPIKey, "LINEAR"))
}
