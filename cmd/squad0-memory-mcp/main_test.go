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
