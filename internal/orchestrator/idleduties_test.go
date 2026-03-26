package orchestrator_test

import (
	"context"
	"database/sql"
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

func setupIdleDutiesOrch(t *testing.T) (*orchestrator.Orchestrator, *pipeline.WorkItemStore, map[agent.Role]*fakeProcessRunner) {
	t.Helper()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

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

	runners := map[agent.Role]*fakeProcessRunner{
		agent.RolePM:        {output: []byte(`{"type":"result","result":"[]"}` + "\n")},
		agent.RoleEngineer1: {output: []byte(`{"type":"result","result":"Looks good to me."}` + "\n")},
		agent.RoleEngineer2: {output: []byte(`{"type":"result","result":"Nice work on that auth flow."}` + "\n")},
		agent.RoleTechLead:  {output: []byte(`{"type":"result","result":"Clean separation of concerns."}` + "\n")},
		agent.RoleDesigner:  {output: []byte(`{"type":"result","result":"PASS"}` + "\n")},
	}

	agents := make(map[agent.Role]*agent.Agent, len(runners))
	for role, runner := range runners {
		agents[role] = buildAgent(t, runner, role, memDB)
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      3,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
		},
		agents, checkIns, bot, orchestrator.NewAssigner(agents[agent.RolePM], "TEST"),
	)
	orch.SetPipeline(pipeStore)
	orch.SetRoster(map[agent.Role]string{
		agent.RoleEngineer1: "Mara",
		agent.RoleEngineer2: "Kael",
		agent.RoleTechLead:  "Ren",
		agent.RoleDesigner:  "Yui",
	})

	return orch, pipeStore, runners
}

func TestRunIdleDuties_IdleEngineer_CommentsOnOthersPR(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, runners := setupIdleDutiesOrch(t)

	// Engineer-1 has an open PR.
	itemID, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-IDLE1",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StagePROpened,
		Branch:   "feat/jam-idle1",
	})
	require.NoError(t, err)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test-org/test-repo/pull/1"))

	// Engineer-2 is idle and should comment.
	orch.RunIdleDuties(ctx, []agent.Role{agent.RoleEngineer2})

	runners[agent.RoleEngineer2].mu.Lock()
	eng2Calls := len(runners[agent.RoleEngineer2].calls)
	runners[agent.RoleEngineer2].mu.Unlock()
	assert.GreaterOrEqual(t, eng2Calls, 1, "idle engineer should comment on colleague's PR")
}

func TestRunIdleDuties_DoesNotCommentOnOwnPR(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, runners := setupIdleDutiesOrch(t)

	// Engineer-1 has an open PR.
	itemID2, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-IDLE2",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StagePROpened,
		Branch:   "feat/jam-idle2",
	})
	require.NoError(t, err)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID2, "https://github.com/test-org/test-repo/pull/2"))

	// Engineer-1 is idle but should NOT comment on own PR.
	runners[agent.RoleEngineer1].mu.Lock()
	beforeCount := len(runners[agent.RoleEngineer1].calls)
	runners[agent.RoleEngineer1].mu.Unlock()

	orch.RunIdleDuties(ctx, []agent.Role{agent.RoleEngineer1})

	runners[agent.RoleEngineer1].mu.Lock()
	afterCount := len(runners[agent.RoleEngineer1].calls)
	runners[agent.RoleEngineer1].mu.Unlock()
	assert.Equal(t, beforeCount, afterCount, "engineer should not comment on own PR")
}

func TestRunIdleDuties_OnlyCommentsOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, runners := setupIdleDutiesOrch(t)

	itemID3, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-IDLE3",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StagePROpened,
		Branch:   "feat/jam-idle3",
	})
	require.NoError(t, err)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID3, "https://github.com/test-org/test-repo/pull/3"))

	// First call — should comment.
	orch.RunIdleDuties(ctx, []agent.Role{agent.RoleEngineer2})

	runners[agent.RoleEngineer2].mu.Lock()
	firstCount := len(runners[agent.RoleEngineer2].calls)
	runners[agent.RoleEngineer2].mu.Unlock()

	// Second call — should NOT comment again.
	orch.RunIdleDuties(ctx, []agent.Role{agent.RoleEngineer2})

	runners[agent.RoleEngineer2].mu.Lock()
	secondCount := len(runners[agent.RoleEngineer2].calls)
	runners[agent.RoleEngineer2].mu.Unlock()

	assert.Equal(t, firstCount, secondCount, "should not comment on same PR twice")
}

func TestRunIdleDuties_TechLead_CommentsOnArchitecture(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, runners := setupIdleDutiesOrch(t)

	itemID4, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-IDLE4",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StagePROpened,
		Branch:   "feat/jam-idle4",
	})
	require.NoError(t, err)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID4, "https://github.com/test-org/test-repo/pull/4"))

	orch.RunIdleDuties(ctx, []agent.Role{agent.RoleTechLead})

	runners[agent.RoleTechLead].mu.Lock()
	tlCalls := len(runners[agent.RoleTechLead].calls)
	runners[agent.RoleTechLead].mu.Unlock()
	assert.GreaterOrEqual(t, tlCalls, 1, "idle tech lead should post architectural observation")
}

func TestRunIdleDuties_Designer_PassesOnBackendPR(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, runners := setupIdleDutiesOrch(t)

	itemID5, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-IDLE5",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StagePROpened,
		Branch:   "feat/jam-idle5",
	})
	require.NoError(t, err)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID5, "https://github.com/test-org/test-repo/pull/5"))

	// Designer returns PASS — should not post.
	orch.RunIdleDuties(ctx, []agent.Role{agent.RoleDesigner})

	runners[agent.RoleDesigner].mu.Lock()
	designerCalls := len(runners[agent.RoleDesigner].calls)
	runners[agent.RoleDesigner].mu.Unlock()
	// Designer was called but their PASS response is filtered out.
	assert.GreaterOrEqual(t, designerCalls, 1, "designer should be prompted even if response is PASS")
}

func TestRunIdleDuties_NoPipeline_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{}, checkIns, nil, nil,
	)

	assert.NotPanics(t, func() {
		orch.RunIdleDuties(ctx, []agent.Role{agent.RoleEngineer1})
	})
}

func TestRunIdleDuties_NoOpenPRs_DoesNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, _, runners := setupIdleDutiesOrch(t)

	// No work items created — no PRs to comment on.
	runners[agent.RoleEngineer2].mu.Lock()
	beforeCount := len(runners[agent.RoleEngineer2].calls)
	runners[agent.RoleEngineer2].mu.Unlock()

	orch.RunIdleDuties(ctx, []agent.Role{agent.RoleEngineer2})

	runners[agent.RoleEngineer2].mu.Lock()
	afterCount := len(runners[agent.RoleEngineer2].calls)
	runners[agent.RoleEngineer2].mu.Unlock()
	assert.Equal(t, beforeCount, afterCount, "no PRs means no idle duty calls")
}

func TestFilterIdleDutyRoles_ExcludesPMAndReviewer(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{
		agent.RolePM, agent.RoleTechLead, agent.RoleEngineer1,
		agent.RoleEngineer2, agent.RoleReviewer, agent.RoleDesigner,
	}

	filtered := orchestrator.FilterIdleDutyRolesForTest(roles)

	assert.Len(t, filtered, 4)
	for _, role := range filtered {
		assert.NotEqual(t, agent.RolePM, role)
		assert.NotEqual(t, agent.RoleReviewer, role)
	}
}

func TestFilterIdleDutyRoles_EmptyInput(t *testing.T) {
	t.Parallel()

	filtered := orchestrator.FilterIdleDutyRolesForTest(nil)
	assert.Empty(t, filtered)
}
