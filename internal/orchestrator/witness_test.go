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

func TestRunWitnessScan_NoConversation_DoesNotPanic(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	assert.NotPanics(t, func() {
		orch.RunWitnessScan(context.Background())
	})
}

func TestRunWitnessScan_UnansweredQuestion_PMResponds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Let's ship it as-is and split next time."}` + "\n"),
		outputs: [][]byte{
			// First call (if OnMessage picks PM): PASS — so PM doesn't answer during seeding.
			[]byte(`{"type":"result","result":"PASS"}` + "\n"),
			// Second call (witness scan): real answer.
			[]byte(`{"type":"result","result":"Let's ship it as-is and split next time."}` + "\n"),
		},
	}
	chatRunner := &fakeProcessRunner{
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
	agents[agent.RolePM] = buildAgent(t, pmRunner, agent.RolePM, memDB)
	factStores[agent.RolePM] = memory.NewFactStore(memDB)

	for _, role := range allRoles {
		if role == agent.RolePM {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, memDB)
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

	// Seed at least 2 messages (scanChannel needs len >= 2), with the
	// channel stale so OnMessage triggers minimal responders.
	conversation.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer3), "just finished the refactor")
	conversation.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer1), "should we ship this now or wait for the next sprint?")
	time.Sleep(50 * time.Millisecond)

	orch.RunWitnessScan(ctx)

	time.Sleep(200 * time.Millisecond)

	pmRunner.mu.Lock()
	totalCalls := len(pmRunner.calls)
	pmRunner.mu.Unlock()

	assert.GreaterOrEqual(t, totalCalls, 1, "PM should have been called")
}

func TestRunWitnessScan_TechQuestion_TechLeadResponds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	tlRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Keep types as a leaf — no auth or DB deps."}` + "\n"),
		outputs: [][]byte{
			[]byte(`{"type":"result","result":"PASS"}` + "\n"),
			[]byte(`{"type":"result","result":"Keep types as a leaf — no auth or DB deps."}` + "\n"),
		},
	}
	chatRunner := &fakeProcessRunner{
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
	agents[agent.RoleTechLead] = buildAgent(t, tlRunner, agent.RoleTechLead, memDB)
	factStores[agent.RoleTechLead] = memory.NewFactStore(memDB)

	for _, role := range allRoles {
		if role == agent.RoleTechLead {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, memDB)
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

	// Seed at least 2 messages with stale timing.
	conversation.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer3), "context message")
	conversation.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer2), "what's the right module boundary for the dependency chain?")
	time.Sleep(50 * time.Millisecond)

	orch.RunWitnessScan(ctx)

	// The witness scan triggers a QuickChat for the Tech Lead.
	// Allow goroutines to complete.
	time.Sleep(200 * time.Millisecond)

	tlRunner.mu.Lock()
	totalCalls := len(tlRunner.calls)
	tlRunner.mu.Unlock()

	// At least 1 call from the witness scan (the TL responding to
	// the architecture question). Conversation engine may add more.
	assert.GreaterOrEqual(t, totalCalls, 1, "Tech Lead should have been called")
}

func TestRunWitnessScan_NoQuestion_DoesNothing(t *testing.T) {
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

	// Seed a statement, not a question.
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer1), "I finished the refactor")

	pmRunner.mu.Lock()
	beforeCount := len(pmRunner.calls)
	pmRunner.mu.Unlock()

	orch.RunWitnessScan(ctx)

	pmRunner.mu.Lock()
	afterCount := len(pmRunner.calls)
	pmRunner.mu.Unlock()

	// No new calls — no question to answer.
	assert.Equal(t, beforeCount, afterCount)
}

func TestScanChannel_TooFewMessages_DoesNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
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
		orchestrator.Config{},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	// Only 1 message — scanChannel requires >= 2.
	conversation.OnMessage(ctx, "engineering", "ceo", "hello?")
	time.Sleep(50 * time.Millisecond)

	runner.mu.Lock()
	before := len(runner.calls)
	runner.mu.Unlock()

	orch.RunWitnessScan(ctx)
	time.Sleep(100 * time.Millisecond)

	runner.mu.Lock()
	after := len(runner.calls)
	runner.mu.Unlock()

	assert.Equal(t, before, after, "should not respond with only 1 message")
}

func TestScanChannel_QuestionFromPM_DoesNotRespond(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
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
		orchestrator.Config{},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	// Seed stale so conversation engine doesn't trigger responders.
	conversation.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer1), "context message")
	conversation.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	// PM asks the question — witness should NOT respond (PM asked it).
	conversation.OnMessage(ctx, "engineering", string(agent.RolePM), "what's the blocker?")
	time.Sleep(50 * time.Millisecond)

	runner.mu.Lock()
	before := len(runner.calls)
	runner.mu.Unlock()

	orch.RunWitnessScan(ctx)
	time.Sleep(100 * time.Millisecond)

	runner.mu.Lock()
	after := len(runner.calls)
	runner.mu.Unlock()

	assert.Equal(t, before, after, "should not respond to PM's own question")
}

func TestScanChannel_ArchQuestion_PicksTechLead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	tlRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"good architecture question"}` + "\n")}
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	chatRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	agents[agent.RoleTechLead] = buildAgent(t, tlRunner, agent.RoleTechLead, memDB)
	factStores[agent.RoleTechLead] = memory.NewFactStore(memDB)
	agents[agent.RolePM] = buildAgent(t, pmRunner, agent.RolePM, memDB)
	factStores[agent.RolePM] = memory.NewFactStore(memDB)
	for _, role := range allRoles {
		if role == agent.RoleTechLead || role == agent.RolePM {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{AcknowledgePause: 0},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	conversation.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer3), "setup context")
	conversation.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer1), "what's the right module boundary for the architecture?")
	time.Sleep(100 * time.Millisecond)

	orch.RunWitnessScan(ctx)
	time.Sleep(200 * time.Millisecond)

	tlRunner.mu.Lock()
	tlCalls := len(tlRunner.calls)
	tlRunner.mu.Unlock()

	assert.GreaterOrEqual(t, tlCalls, 1, "tech lead should respond to architecture question")
}

func TestScanChannel_BusyRole_DoesNotRespond(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"answer"}` + "\n")}
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
		orchestrator.Config{},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	// Mark PM as busy.
	require.NoError(t, checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RolePM, Status: "working", FilesTouching: []string{},
	}))

	conversation.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer3), "context")
	conversation.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer1), "should we ship this now?")
	time.Sleep(50 * time.Millisecond)

	runner.mu.Lock()
	before := len(runner.calls)
	runner.mu.Unlock()

	orch.RunWitnessScan(ctx)
	time.Sleep(100 * time.Millisecond)

	runner.mu.Lock()
	after := len(runner.calls)
	runner.mu.Unlock()

	assert.Equal(t, before, after, "should not respond when PM is busy")
}
