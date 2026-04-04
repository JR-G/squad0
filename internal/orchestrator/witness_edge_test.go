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

func TestScanChannel_SingleMessage_TooFewLines_Skips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, pmRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	// Seed only 1 message — scanChannel requires len >= 2.
	conversation.OnMessage(ctx, "engineering", "ceo", "Hello team")

	pmRunner.mu.Lock()
	beforeCount := len(pmRunner.calls)
	pmRunner.mu.Unlock()

	orch.RunWitnessScan(ctx)

	pmRunner.mu.Lock()
	afterCount := len(pmRunner.calls)
	pmRunner.mu.Unlock()

	assert.Equal(t, beforeCount, afterCount, "should not call any agent with only 1 message")
}

func TestScanChannel_QuestionFromPM_Skips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	// Seed history with the PM asking a question — witness should skip
	// because the question is FROM the PM.
	conversation.SeedHistory("engineering", []string{
		"engineer-1: I finished the refactor.",
		string(agent.RolePM) + ": should we ship this now?",
	})

	runner.mu.Lock()
	beforeCount := len(runner.calls)
	runner.mu.Unlock()

	orch.RunWitnessScan(ctx)

	runner.mu.Lock()
	afterCount := len(runner.calls)
	runner.mu.Unlock()

	assert.Equal(t, beforeCount, afterCount, "should not respond to PM's own question")
}

func TestScanChannel_QuestionFromTechLead_Skips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	// Tech Lead asks a question — witness should skip.
	conversation.SeedHistory("engineering", []string{
		"engineer-2: The auth module is done.",
		string(agent.RoleTechLead) + ": should we add more tests?",
	})

	runner.mu.Lock()
	beforeCount := len(runner.calls)
	runner.mu.Unlock()

	orch.RunWitnessScan(ctx)

	runner.mu.Lock()
	afterCount := len(runner.calls)
	runner.mu.Unlock()

	assert.Equal(t, beforeCount, afterCount, "should not respond to Tech Lead's own question")
}

func TestScanChannel_QuestionFromRosterName_Skips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)
	orch.SetRoster(map[agent.Role]string{
		agent.RolePM:       "Nova",
		agent.RoleTechLead: "Kai",
	})

	// PM's chosen name "Nova" appears in the last message — should skip.
	conversation.SeedHistory("engineering", []string{
		"engineer-1: Ready for review.",
		"Nova: should we merge this now?",
	})

	runner.mu.Lock()
	beforeCount := len(runner.calls)
	runner.mu.Unlock()

	orch.RunWitnessScan(ctx)

	runner.mu.Lock()
	afterCount := len(runner.calls)
	runner.mu.Unlock()

	assert.Equal(t, beforeCount, afterCount, "should not respond when PM roster name matches")
}

func TestScanChannel_WitnessAgentBusy_Skips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	// Mark PM as busy.
	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RolePM,
		Status:        "working",
		FilesTouching: []string{},
	}))
	// Mark Tech Lead as busy too, so neither witness can respond.
	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RoleTechLead,
		Status:        "working",
		FilesTouching: []string{},
	}))

	// Engineer asks a process question — PM would normally answer but is busy.
	conversation.SeedHistory("engineering", []string{
		"engineer-3: context message here.",
		"engineer-1: should we deploy this now?",
	})

	runner.mu.Lock()
	beforeCount := len(runner.calls)
	runner.mu.Unlock()

	orch.RunWitnessScan(ctx)

	runner.mu.Lock()
	afterCount := len(runner.calls)
	runner.mu.Unlock()

	assert.Equal(t, beforeCount, afterCount, "should not call agent when witness role is busy")
}

func TestScanChannel_PassResponse_DroppedSilently(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// Agent returns PASS — the witness should drop the response silently.
	passRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, passRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	conversation.SeedHistory("engineering", []string{
		"engineer-2: context message here.",
		"engineer-1: should we ship this now?",
	})

	// No panic and no message posted (no bot configured).
	assert.NotPanics(t, func() {
		orch.RunWitnessScan(ctx)
	})
}

func TestScanChannel_MissingWitnessAgent_Skips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	// Only include engineers — no PM or Tech Lead, so witness can't respond.
	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, memDB),
		agent.RoleEngineer2: buildAgent(t, runner, agent.RoleEngineer2, memDB),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(memDB),
		agent.RoleEngineer2: memory.NewFactStore(memDB),
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	conversation.SeedHistory("engineering", []string{
		"engineer-2: context message here.",
		"engineer-1: should we deploy now?",
	})

	assert.NotPanics(t, func() {
		orch.RunWitnessScan(ctx)
	})
}

func TestScanChannel_MoreThanFiveLines_TruncatesTail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Let me weigh in on that."}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, pmRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	conversation := orchestrator.NewConversationEngine(agents, factStores, bot, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, bot, nil,
	)
	orch.SetConversationEngine(conversation)

	// Seed > 5 messages, ending with a question from an engineer.
	conversation.SeedHistory("engineering", []string{
		"engineer-1: first message.",
		"engineer-2: second message.",
		"engineer-3: third message.",
		"engineer-1: fourth message.",
		"engineer-2: fifth message.",
		"engineer-3: sixth message.",
		"engineer-1: should we proceed with the deployment?",
	})

	orch.RunWitnessScan(ctx)

	// The PM should have been called — the tail truncation to 5 lines
	// is exercised because there are > 5 messages.
	pmRunner.mu.Lock()
	totalCalls := len(pmRunner.calls)
	pmRunner.mu.Unlock()

	assert.GreaterOrEqual(t, totalCalls, 1, "PM should have been called for the question")
}
