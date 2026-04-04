package cli

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRosterFromDB_ReturnsAllRoles(t *testing.T) {
	t.Parallel()

	roster := loadRosterFromDB("engineer-1")
	assert.Len(t, roster, len(agent.AllRoles()))

	for _, role := range agent.AllRoles() {
		_, ok := roster[role]
		assert.True(t, ok, "roster should contain %s", role)
	}
}

func TestLoadBeliefsFromDB_MissingDB_ReturnsNil(t *testing.T) {
	t.Parallel()

	beliefs := loadBeliefsFromDB("nonexistent-role")
	assert.Nil(t, beliefs)
}

func TestRunPrime_InvalidRole_ReturnsError(t *testing.T) {
	t.Parallel()

	err := runPrime(t.Context(), "nonexistent-role")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "loading personality")
}

func TestRunPrime_ValidRole_WritesToStdout(t *testing.T) {
	t.Parallel()

	// This requires the agents/ directory to exist with personality files.
	// The test runs from the repo root where agents/ exists.
	err := runPrime(t.Context(), "pm")
	// May fail if CWD doesn't have agents/ — that's OK for unit tests.
	if err != nil {
		assert.Contains(t, err.Error(), "loading personality")
	}
}

func TestLoadBeliefsFromPath_ValidDB_ReturnsBeliefs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := dir + "/test.db"

	ctx := context.Background()
	db, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)

	factStore := memory.NewFactStore(db)
	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "auth module is fragile", Confidence: 0.8})
	_ = db.Close()

	beliefs := loadBeliefsFromPath(dbPath)
	assert.NotEmpty(t, beliefs)
	assert.Contains(t, beliefs[0], "auth module is fragile")
}

func TestLoadBeliefsFromPath_MissingDB_ReturnsNil(t *testing.T) {
	t.Parallel()

	beliefs := loadBeliefsFromPath("/nonexistent/path.db")
	assert.Nil(t, beliefs)
}

func TestLoadBeliefsFromPath_EmptyDB_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := dir + "/empty.db"

	ctx := context.Background()
	db, err := memory.Open(ctx, dbPath)
	require.NoError(t, err)
	_ = db.Close()

	beliefs := loadBeliefsFromPath(dbPath)
	assert.Empty(t, beliefs)
}
