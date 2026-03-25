package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyMerged_ReturnsMerged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"MERGED"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, runner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assert.True(t, orch.VerifyMergedForTest(ctx, pmAgent, "https://github.com/test-org/test-repo/pull/42"))
}

func TestVerifyMerged_ReturnsOpen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"OPEN"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, runner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assert.False(t, orch.VerifyMergedForTest(ctx, pmAgent, "https://github.com/test-org/test-repo/pull/42"))
}

func TestVerifyMerged_Error_ReturnsFalse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}
	pmAgent := setupPMAgent(t, runner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assert.False(t, orch.VerifyMergedForTest(ctx, pmAgent, "https://github.com/test-org/test-repo/pull/42"))
}
