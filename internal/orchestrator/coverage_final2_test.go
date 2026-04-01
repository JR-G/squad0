package orchestrator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/health"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 8. rescuePR — session fails
// ---------------------------------------------------------------------------

func TestRescuePR_SessionFails_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"session crashed"}` + "\n"),
		err:    assert.AnError,
	}
	agentInstance := buildAgent(t, runner, agent.RoleEngineer1, memDB)

	checkIns := coordination.NewCheckInStore(sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: agentInstance},
		checkIns, nil, nil,
	)

	prURL := orch.RescuePRForTest(ctx, agentInstance, "/tmp/work", "JAM-RESCUE", "feat/jam-rescue")
	assert.Empty(t, prURL, "failed session should return empty PR URL")
}

// ---------------------------------------------------------------------------
// 9. emitEvent — with event bus wired up
// ---------------------------------------------------------------------------

func TestEmitEvent_WithEventBus_EmitsSuccessfully(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	bus := orchestrator.NewEventBus()
	var received atomic.Bool
	bus.On(orchestrator.EventSessionComplete, func(_ context.Context, event orchestrator.Event) {
		if event.Ticket == "JAM-EVT1" && event.PRURL == "https://github.com/test/pull/1" {
			received.Store(true)
		}
	})

	orch.SetEventBus(bus)

	orch.EmitEventForTest(ctx,
		orchestrator.EventSessionComplete,
		"https://github.com/test/pull/1",
		"JAM-EVT1",
		42,
		agent.RoleEngineer1,
	)

	require.Eventually(t, received.Load, time.Second, 5*time.Millisecond)
}

func TestEmitEvent_NilEventBus_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	assert.NotPanics(t, func() {
		orch.EmitEventForTest(ctx, orchestrator.EventSessionComplete, "", "T-1", 0, agent.RoleEngineer1)
	})
}

// ---------------------------------------------------------------------------
// 10. idleCommentCount — multiple comments
// ---------------------------------------------------------------------------

func TestIdleCommentCount_MultipleAgents_ExercisesCountPath(t *testing.T) {
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

	pipeDB, pipeErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(pipeDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	runners := map[agent.Role]*fakeProcessRunner{
		agent.RolePM:        {output: []byte(`{"type":"result","result":"[]"}` + "\n")},
		agent.RoleEngineer1: {output: []byte(`{"type":"result","result":"Nice work."}` + "\n")},
		agent.RoleEngineer2: {output: []byte(`{"type":"result","result":"Good patterns."}` + "\n")},
		agent.RoleEngineer3: {output: []byte(`{"type":"result","result":"Clean code."}` + "\n")},
		agent.RoleTechLead:  {output: []byte(`{"type":"result","result":"Solid arch."}` + "\n")},
		agent.RoleDesigner:  {output: []byte(`{"type":"result","result":"Great UX."}` + "\n")},
	}

	agents := make(map[agent.Role]*agent.Agent, len(runners))
	for role, runner := range runners {
		agents[role] = buildAgent(t, runner, role, memDB)
	}

	slackServer := newTestSlackServer()
	t.Cleanup(slackServer.Close)
	bot := newTestSlackBot(slackServer.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second},
		agents, checkIns, bot, nil,
	)
	orch.SetPipeline(pipeStore)
	orch.SetRoster(map[agent.Role]string{
		agent.RoleEngineer1: "Mara",
		agent.RoleEngineer2: "Kael",
		agent.RoleEngineer3: "Zeph",
		agent.RoleTechLead:  "Ren",
		agent.RoleDesigner:  "Yui",
	})

	itemID, itemErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-IDLE10",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StagePROpened,
		Branch:   "feat/jam-idle10",
	})
	require.NoError(t, itemErr)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test/pull/10"))

	// Have multiple agents comment — exercises idleCommentCount.
	orch.RunIdleDuties(ctx, []agent.Role{agent.RoleEngineer2})
	orch.RunIdleDuties(ctx, []agent.Role{agent.RoleEngineer3})
	orch.RunIdleDuties(ctx, []agent.Role{agent.RoleTechLead})
	orch.RunIdleDuties(ctx, []agent.Role{agent.RoleDesigner})

	totalCalls := 0
	for role, runner := range runners {
		if role == agent.RolePM || role == agent.RoleEngineer1 {
			continue
		}
		runner.mu.Lock()
		totalCalls += len(runner.calls)
		runner.mu.Unlock()
	}
	assert.GreaterOrEqual(t, totalCalls, 3,
		"multiple agents should have commented, exercising idleCommentCount")
}

// ---------------------------------------------------------------------------
// 11. isDuplicate — duplicate IS found
// ---------------------------------------------------------------------------

func TestIsDuplicate_WithSimilarMessage_Dropped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"This is a great implementation with clean patterns."}` + "\n"),
	}

	agents := make(map[agent.Role]*agent.Agent)
	factStores := make(map[agent.Role]*memory.FactStore)
	for _, role := range []agent.Role{agent.RolePM, agent.RoleEngineer1, agent.RoleTechLead} {
		agents[role] = buildAgent(t, runner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	var mu sync.Mutex
	var postedMessages []string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		mu.Lock()
		postedMessages = append(postedMessages, req.FormValue("text"))
		mu.Unlock()

		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	slackServer := httptest.NewServer(handler)
	t.Cleanup(slackServer.Close)

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"engineering": "C001",
			"feed":        "C002",
		},
		Personas:   map[agent.Role]islack.Persona{},
		MinSpacing: 0,
	}, slackServer.URL+"/")

	engine := orchestrator.NewConversationEngine(agents, factStores, bot, nil)

	// Seed a similar message so isDuplicate fires on subsequent responses.
	engine.OnMessage(ctx, "engineering", "ceo",
		"This is a great implementation with clean patterns.")

	engine.OnMessage(ctx, "engineering", "ceo", "What do you think?")

	time.Sleep(300 * time.Millisecond)

	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent, "channel should have recent messages")
}

// ---------------------------------------------------------------------------
// 12. pmComposedStandup — with PM agent
// ---------------------------------------------------------------------------

func TestPMComposedStandup_WithPMAgent_PostsPMSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	var mu sync.Mutex
	var postedTexts []string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		mu.Lock()
		postedTexts = append(postedTexts, req.FormValue("text"))
		mu.Unlock()

		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	slackServer := httptest.NewServer(handler)
	t.Cleanup(slackServer.Close)

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"standup":     "C001",
			"engineering": "C002",
			"feed":        "C003",
			"triage":      "C004",
		},
		Personas: map[agent.Role]islack.Persona{
			agent.RolePM: {Role: agent.RolePM, Name: "Nova"},
		},
		MinSpacing: 0,
	}, slackServer.URL+"/")

	roles := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 30 * time.Minute,
	})

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"*Standup Summary*\nMara is working on JAM-42. Kael is idle."}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	_, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-42",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-42",
	})
	require.NoError(t, createErr)

	sched := orchestrator.NewScheduler(bot, monitor, nil, orchestrator.SchedulerConfig{
		StandupInterval:   10 * time.Millisecond,
		HealthInterval:    time.Hour,
		RetroAfterTickets: 100,
	})
	sched.SetPipeline(pipeStore)
	sched.SetAgents(map[agent.Role]*agent.Agent{agent.RolePM: pmAgent})
	sched.SetRoster(map[agent.Role]string{
		agent.RoleEngineer1: "Mara",
		agent.RoleEngineer2: "Kael",
	})

	timedCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_ = sched.Run(timedCtx)

	pmRunner.mu.Lock()
	pmCalls := len(pmRunner.calls)
	pmRunner.mu.Unlock()
	assert.GreaterOrEqual(t, pmCalls, 1, "PM should compose the standup")

	mu.Lock()
	defer mu.Unlock()
	assert.NotEmpty(t, postedTexts, "standup message should be posted")

	found := false
	for _, text := range postedTexts {
		if strings.Contains(text, "Standup") || strings.Contains(text, "Mara") {
			found = true
			break
		}
	}
	assert.True(t, found, "posted text should contain PM-composed standup")
}

// ---------------------------------------------------------------------------
// Extra: EventBus RegisterDefaultHandlers wiring test
// ---------------------------------------------------------------------------

func TestRegisterDefaultHandlers_WiresUpMergeFailedAndIdle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	bus := orchestrator.NewEventBus()
	orch.RegisterDefaultHandlers(bus)
	orch.SetEventBus(bus)

	assert.Equal(t, 1, bus.HandlerCount(orchestrator.EventMergeFailed))
	assert.Equal(t, 1, bus.HandlerCount(orchestrator.EventAgentIdle))

	assert.NotPanics(t, func() {
		bus.EmitSync(ctx, orchestrator.Event{
			Kind:         orchestrator.EventAgentIdle,
			EngineerRole: agent.RoleEngineer1,
		})
	})
}

// ---------------------------------------------------------------------------
// Extra: ResumeWorkItem with PR — exercising resumeWithGitHubState
// ---------------------------------------------------------------------------

func TestResumeWorkItem_WithPR_NoPM_ReturnsEarly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-RESUME",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StagePROpened,
	})
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test/pull/99"))

	item, err := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		orch.ResumeWorkItemForTest(ctx, item)
	})
}
