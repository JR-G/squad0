package orchestrator_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvestigateConcerns_DirectSessionFails_ConcernUnresolved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	// Runner that always returns an error.
	runner := &fakeProcessRunner{
		err: errors.New("session crashed"),
	}

	engAgent := buildAgent(t, runner, agent.RoleEngineer1, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      1,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
		},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, nil, nil,
	)

	tracker := orchestrator.NewConcernTracker()
	tracker.AddConcern(agent.RoleEngineer1, "worried about session stability", "TASK-10")
	orch.SetConcernTracker(tracker)

	// Should not panic when DirectSession returns an error.
	assert.NotPanics(t, func() {
		orch.InvestigateConcerns(ctx, []agent.Role{agent.RoleEngineer1})
	})

	// Concern remains unresolved because the session failed.
	unresolved := tracker.UnresolvedForRole(agent.RoleEngineer1)
	assert.Len(t, unresolved, 1, "concern should remain unresolved after session failure")
}

func TestInvestigateConcerns_AgentMissing_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	// No agents in the map — engineer-1 is missing.
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      1,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
		},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	tracker := orchestrator.NewConcernTracker()
	tracker.AddConcern(agent.RoleEngineer1, "worried about missing agent", "TASK-11")
	orch.SetConcernTracker(tracker)

	// Should not panic when the agent is not in the map.
	assert.NotPanics(t, func() {
		orch.InvestigateConcerns(ctx, []agent.Role{agent.RoleEngineer1})
	})

	// Concern remains unresolved because the agent was not found.
	unresolved := tracker.UnresolvedForRole(agent.RoleEngineer1)
	assert.Len(t, unresolved, 1, "concern should remain when agent is missing")
}

func TestInvestigateConcerns_MultipleRoles_OnlyInvestigatesOwn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Checked — all looks fine."}` + "\n"),
	}

	eng1Agent := buildAgent(t, runner, agent.RoleEngineer1, memDB)

	memDB2, err2 := memory.Open(ctx, ":memory:")
	require.NoError(t, err2)
	t.Cleanup(func() { _ = memDB2.Close() })

	eng2Agent := buildAgent(t, runner, agent.RoleEngineer2, memDB2)

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      2,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
		},
		map[agent.Role]*agent.Agent{
			agent.RoleEngineer1: eng1Agent,
			agent.RoleEngineer2: eng2Agent,
		},
		checkIns, bot, nil,
	)

	tracker := orchestrator.NewConcernTracker()
	tracker.AddConcern(agent.RoleEngineer1, "worried about data consistency", "TASK-12")
	// Engineer-2 has no concerns.
	orch.SetConcernTracker(tracker)

	orch.InvestigateConcerns(ctx, []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2})

	// Engineer-1's concern should be resolved.
	eng1Unresolved := tracker.UnresolvedForRole(agent.RoleEngineer1)
	assert.Empty(t, eng1Unresolved, "engineer-1 concern should be resolved")

	// Engineer-2 had no concerns, nothing should change.
	eng2Unresolved := tracker.UnresolvedForRole(agent.RoleEngineer2)
	assert.Empty(t, eng2Unresolved)
}

func TestInvestigateConcerns_PassResponse_ResolvesWithoutPosting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	// Agent returns "PASS" — filterPassResponse will return empty.
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	engAgent := buildAgent(t, runner, agent.RoleEngineer1, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      1,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
		},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, nil, nil,
	)

	tracker := orchestrator.NewConcernTracker()
	tracker.AddConcern(agent.RoleEngineer1, "worried about pass path", "TASK-20")
	orch.SetConcernTracker(tracker)

	orch.InvestigateConcerns(ctx, []agent.Role{agent.RoleEngineer1})

	// Concern should be resolved even though response was empty (pass).
	unresolved := tracker.UnresolvedForRole(agent.RoleEngineer1)
	assert.Empty(t, unresolved, "concern should be resolved on pass response")
}

func TestInvestigateConcerns_NarrationOnly_ResolvesWithoutPosting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	// Agent returns only narration lines that cleanIdleResponse strips.
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Comment posted to Slack.\n---\n# Summary"}` + "\n"),
	}

	engAgent := buildAgent(t, runner, agent.RoleEngineer1, memDB)

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      1,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
		},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, bot, nil,
	)

	tracker := orchestrator.NewConcernTracker()
	tracker.AddConcern(agent.RoleEngineer1, "worried about narration path", "TASK-21")
	orch.SetConcernTracker(tracker)

	orch.InvestigateConcerns(ctx, []agent.Role{agent.RoleEngineer1})

	// Concern should be resolved.
	unresolved := tracker.UnresolvedForRole(agent.RoleEngineer1)
	assert.Empty(t, unresolved, "concern should be resolved even when clean response is empty")
}
