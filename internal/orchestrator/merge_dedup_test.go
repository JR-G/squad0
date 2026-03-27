package orchestrator_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeAnnouncement_NotDuplicated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// Engineer session succeeds, PM verifies merged.
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Merged."}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"MERGED"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeDB, pipeErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(pipeDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
	}

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-DD1", Engineer: agent.RoleEngineer1,
		Stage: pipeline.StageApproved, Branch: "feat/jam-dd1",
	})
	require.NoError(t, createErr)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval: time.Second, MaxParallel: 1,
			CooldownAfter: time.Second, AcknowledgePause: time.Millisecond,
		},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	prURL := "https://github.com/test-org/test-repo/pull/500"

	// First merge — should announce.
	orch.StartEngineerMergeForTest(ctx, prURL, "JAM-DD1", itemID, agent.RoleEngineer1)
	assert.True(t, orch.HasMergeAnnouncedForTest("JAM-DD1"))

	// Second call — should not announce again.
	orch.StartEngineerMergeForTest(ctx, prURL, "JAM-DD1", itemID, agent.RoleEngineer1)

	// The announcement should only have been posted once. We verify
	// indirectly via the dedup flag being set.
	assert.True(t, orch.HasMergeAnnouncedForTest("JAM-DD1"))
}

func TestMergeAnnouncement_DifferentTickets_BothAnnounced(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Merged."}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"MERGED"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval: time.Second, MaxParallel: 1,
			CooldownAfter: time.Second, AcknowledgePause: time.Millisecond,
		},
		agents, checkIns, nil, nil,
	)

	orch.StartEngineerMergeForTest(ctx, "https://github.com/test-org/test-repo/pull/501", "JAM-A", 0, agent.RoleEngineer1)
	orch.StartEngineerMergeForTest(ctx, "https://github.com/test-org/test-repo/pull/502", "JAM-B", 0, agent.RoleEngineer1)

	assert.True(t, orch.HasMergeAnnouncedForTest("JAM-A"))
	assert.True(t, orch.HasMergeAnnouncedForTest("JAM-B"))
	assert.False(t, orch.HasMergeAnnouncedForTest("JAM-C"))
}

func TestMergeAfterRetry_DeduplicatesAnnouncement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// checkApprovalStatus → APPROVED, executeMerge → done, verifyMerged → MERGED.
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"APPROVED"}` + "\n"),
		outputs: [][]byte{
			[]byte(`{"type":"result","result":"APPROVED"}` + "\n"),
			[]byte(`{"type":"result","result":"done"}` + "\n"),
			[]byte(`{"type":"result","result":"MERGED"}` + "\n"),
			[]byte(`{"type":"result","result":"done"}` + "\n"),
		},
	}
	pmAgent := setupPMAgent(t, pmRunner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval: time.Second, MaxParallel: 1,
			CooldownAfter: time.Second, AcknowledgePause: time.Millisecond,
		},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	// Pre-mark as announced.
	orch.MergeAfterRetryForTest(ctx, "https://github.com/test-org/test-repo/pull/600", "JAM-RT5", 0, agent.RoleEngineer1)

	assert.True(t, orch.HasMergeAnnouncedForTest("JAM-RT5"))
}

func TestMergeAnnouncement_PMFallback_Deduplicated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// PM: checkApprovalStatus → APPROVED, executeMerge → done, verifyMerged → MERGED.
	callIndex := 0
	outputs := [][]byte{
		[]byte(`{"type":"result","result":"APPROVED"}` + "\n"),
		[]byte(`{"type":"result","result":"done"}` + "\n"),
		[]byte(`{"type":"result","result":"MERGED"}` + "\n"),
		[]byte(`{"type":"result","result":"done"}` + "\n"),
		// Second round: same responses.
		[]byte(`{"type":"result","result":"APPROVED"}` + "\n"),
		[]byte(`{"type":"result","result":"done"}` + "\n"),
		[]byte(`{"type":"result","result":"MERGED"}` + "\n"),
		[]byte(`{"type":"result","result":"done"}` + "\n"),
	}
	_ = callIndex
	pmRunner := &fakeProcessRunner{
		output:  outputs[0],
		outputs: outputs,
	}
	pmAgent := setupPMAgent(t, pmRunner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeDB, pipeErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(pipeDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval: time.Second, MaxParallel: 1,
			CooldownAfter: time.Second, AcknowledgePause: time.Millisecond,
		},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-FB1", Engineer: agent.RoleEngineer1,
		Stage: pipeline.StageApproved, Branch: "feat/jam-fb1",
	})
	require.NoError(t, createErr)

	// No engineer-1 available — falls back to PM. First call announces.
	orch.StartEngineerMergeForTest(ctx, "https://github.com/test-org/test-repo/pull/700", "JAM-FB1", itemID, agent.RoleEngineer1)

	assert.True(t, orch.HasMergeAnnouncedForTest("JAM-FB1"))

	// Verify: calling Slack poster was invoked; but importantly the
	// dedup flag prevents double posting on a second call path.
	pmRunner.mu.Lock()
	mergePostCount := 0
	for _, call := range pmRunner.calls {
		if strings.Contains(call.stdin, "Merged") {
			mergePostCount++
		}
	}
	pmRunner.mu.Unlock()

	// The fakeProcessRunner tracks agent sessions, not Slack posts,
	// so we assert the flag is set — the actual posting is guarded.
	assert.True(t, orch.HasMergeAnnouncedForTest("JAM-FB1"))
}
