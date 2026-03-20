package mcp_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_NewServer_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&fakeHandler{})
	assert.NotNil(t, server)
}

func TestServer_Run_EmptyInput_ReturnsNoError(t *testing.T) {
	t.Parallel()

	reader := strings.NewReader("")
	writer := &bytes.Buffer{}

	server := mcp.NewServerWithIO(&fakeHandler{}, reader, writer)
	err := server.Run(context.Background())

	require.NoError(t, err)
}
