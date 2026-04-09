package orchestrator_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// isRoleIdle — exercising the idle-check path in pipelineops.go
// ---------------------------------------------------------------------------

func TestIsRoleIdle_IdleAgent_ReturnsTrue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusIdle, FilesTouching: []string{},
	}))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{}, nil, checkIns, nil, nil,
	)

	assert.True(t, orch.IsRoleIdleForTest(ctx, agent.RoleEngineer1))
}

func TestIsRoleIdle_WorkingAgent_ReturnsFalse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{},
	}))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{}, nil, checkIns, nil, nil,
	)

	assert.False(t, orch.IsRoleIdleForTest(ctx, agent.RoleEngineer1))
}

func TestIsRoleIdle_NoCheckIn_DefaultsToTrue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{}, nil, checkIns, nil, nil,
	)

	// No check-in row — defaults to idle.
	assert.True(t, orch.IsRoleIdleForTest(ctx, agent.RoleEngineer1))
}

// ---------------------------------------------------------------------------
// SummariseThread — exercise the long-thread path
// ---------------------------------------------------------------------------

func TestSummariseThread_LongThread_Summarises(t *testing.T) {
	t.Parallel()

	lines := make([]string, 12)
	for idx := range lines {
		lines[idx] = fmt.Sprintf("speaker-%d: message %d", idx%3, idx)
	}

	result := orchestrator.SummariseThread(lines, 8)
	assert.Contains(t, result, "4 earlier messages")
	assert.Contains(t, result, "speaker-0")
}

func TestSummariseThread_WithQuestion_NotesIt(t *testing.T) {
	t.Parallel()

	lines := make([]string, 12)
	for idx := range lines {
		lines[idx] = fmt.Sprintf("speaker-%d: message %d", idx%3, idx)
	}
	lines[len(lines)-1] = "speaker-2: what do you think?"

	result := orchestrator.SummariseThread(lines, 8)
	assert.Contains(t, result, "unanswered question")
}
