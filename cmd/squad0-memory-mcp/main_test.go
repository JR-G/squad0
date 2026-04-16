package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_InvalidDB_ReturnsError(t *testing.T) {
	t.Parallel()

	err := run("/nonexistent/path/test.db")

	require.Error(t, err)
}

func TestRun_ValidDB_StartsAndStops(t *testing.T) {
	t.Parallel()

	dbPath := t.TempDir() + "/test.db"

	// run blocks on server.Run which reads from stdin.
	// With no stdin it returns immediately.
	err := run(dbPath)

	assert.NoError(t, err)
}

func TestResolveDBPath_EnvWinsOverFlag(t *testing.T) {
	t.Parallel()

	got := resolveDBPath("/from/flag.db", "/from/env.db")

	assert.Equal(t, "/from/env.db", got)
}

func TestResolveDBPath_FlagUsedWhenEnvEmpty(t *testing.T) {
	t.Parallel()

	got := resolveDBPath("/from/flag.db", "")

	assert.Equal(t, "/from/flag.db", got)
}

func TestResolveDBPath_BothEmpty_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, resolveDBPath("", ""))
}
