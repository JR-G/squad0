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
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitForQuiet_NoEngine_UsesFixedWait(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	planRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I'll update the schema."}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, planRunner, agent.RoleEngineer1, db)

	// No bot, no conversation engine — waitForQuiet falls back to fixed wait.
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:   time.Second,
			MaxParallel:    1,
			CooldownAfter:  time.Second,
			DiscussionWait: 10 * time.Millisecond,
		},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent, agent.RoleEngineer1: engAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assignment := orchestrator.Assignment{
		Role: agent.RoleEngineer1, Ticket: "JAM-1", Description: "update schema",
	}

	start := time.Now()
	_ = orch.RunDiscussionForTest(ctx, engAgent, assignment)
	elapsed := time.Since(start)

	// Should return quickly since DiscussionWait is 10ms.
	assert.Less(t, elapsed, 2*time.Second)
}

func TestWaitForQuiet_CancelledContext_ReturnsImmediately(t *testing.T) {
	t.Parallel()

	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	planRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I'll fix the bug."}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, planRunner, agent.RoleEngineer1, db)

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	chatRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	agents[agent.RolePM] = pmAgent
	agents[agent.RoleEngineer1] = engAgent
	factStores[agent.RolePM] = memory.NewFactStore(db)
	factStores[agent.RoleEngineer1] = memory.NewFactStore(db)
	for _, role := range allRoles {
		if role == agent.RolePM || role == agent.RoleEngineer1 {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	conversation := orchestrator.NewConversationEngine(agents, factStores, bot, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:      time.Second,
			MaxParallel:       1,
			CooldownAfter:     time.Second,
			QuietThreshold:    time.Hour, // Very long — but context will cancel.
			QuietPollInterval: 5 * time.Millisecond,
			DiscussionWait:    time.Hour,
		},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetConversationEngine(conversation)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	assignment := orchestrator.Assignment{
		Role: agent.RoleEngineer1, Ticket: "JAM-2", Description: "fix bug",
	}

	start := time.Now()
	_ = orch.RunDiscussionForTest(ctx, engAgent, assignment)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 2*time.Second)
}

func TestWaitForQuiet_MaxWaitReached_Proceeds(t *testing.T) {
	t.Parallel()

	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	planRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I'll refactor the module."}` + "\n"),
	}
	// This chat runner keeps posting so the channel never goes quiet.
	chatRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I have something to say."}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, planRunner, agent.RoleEngineer1, db)

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	agents[agent.RolePM] = pmAgent
	agents[agent.RoleEngineer1] = engAgent
	factStores[agent.RolePM] = memory.NewFactStore(db)
	factStores[agent.RoleEngineer1] = memory.NewFactStore(db)
	for _, role := range allRoles {
		if role == agent.RolePM || role == agent.RoleEngineer1 {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	conversation := orchestrator.NewConversationEngine(agents, factStores, bot, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:      time.Second,
			MaxParallel:       1,
			CooldownAfter:     time.Second,
			QuietThreshold:    time.Hour, // Very long quiet threshold.
			QuietPollInterval: 5 * time.Millisecond,
			DiscussionWait:    50 * time.Millisecond, // Short max wait.
		},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetConversationEngine(conversation)

	ctx := context.Background()
	assignment := orchestrator.Assignment{
		Role: agent.RoleEngineer1, Ticket: "JAM-3", Description: "refactor",
	}

	start := time.Now()
	_ = orch.RunDiscussionForTest(ctx, engAgent, assignment)
	elapsed := time.Since(start)

	// Should finish quickly because DiscussionWait (maxWait) is 50ms.
	assert.Less(t, elapsed, 2*time.Second)
}
