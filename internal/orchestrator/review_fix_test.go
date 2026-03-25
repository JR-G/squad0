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

func TestMergeAndComplete_CIFailing_AnnouncesAndReturns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"CI_FAILING"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	// PM returns CI_FAILING — should announce and return, not loop.
	assert.NotPanics(t, func() {
		orch.MergeWithEngineerForTest(ctx, "https://github.com/test-org/test-repo/pull/1", "JAM-1", 0, agent.RoleEngineer1)
	})
}
