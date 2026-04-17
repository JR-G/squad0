package orchestrator_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupVerifyMergedOrch(t *testing.T) (*orchestrator.Orchestrator, *agent.Agent) {
	t.Helper()

	ctx := context.Background()
	pmAgent := setupPMAgent(t, &fakeProcessRunner{output: []byte(`{"type":"result","result":""}` + "\n")})

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	return orch, pmAgent
}

func TestVerifyMerged_ReturnsMerged(t *testing.T) {
	restore := orchestrator.SetMergeVerifierForTest(func(_ context.Context, _, _ string) bool { return true })
	t.Cleanup(restore)

	orch, pmAgent := setupVerifyMergedOrch(t)

	assert.True(t, orch.VerifyMergedForTest(context.Background(), pmAgent, "https://github.com/test-org/test-repo/pull/42"))
}

func TestVerifyMerged_ReturnsOpen(t *testing.T) {
	restore := orchestrator.SetMergeVerifierForTest(func(_ context.Context, _, _ string) bool { return false })
	t.Cleanup(restore)

	orch, pmAgent := setupVerifyMergedOrch(t)

	assert.False(t, orch.VerifyMergedForTest(context.Background(), pmAgent, "https://github.com/test-org/test-repo/pull/42"))
}

func TestVerifyMerged_Error_ReturnsFalse(t *testing.T) {
	restore := orchestrator.SetMergeVerifierForTest(func(_ context.Context, _, _ string) bool {
		_ = errors.New("simulated gh failure")
		return false
	})
	t.Cleanup(restore)

	orch, pmAgent := setupVerifyMergedOrch(t)

	assert.False(t, orch.VerifyMergedForTest(context.Background(), pmAgent, "https://github.com/test-org/test-repo/pull/42"))
}
