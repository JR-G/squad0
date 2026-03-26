package orchestrator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_Standup_WithPMAndPipeline_PMComposesStandup(t *testing.T) {
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

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	bot := newSchedulerStandupBot(t, server.URL)

	roles := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 30 * time.Minute,
	})

	// PM composes the standup.
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"*Morning standup*\nEveryone is idle."}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

	// Set up pipeline with one open item.
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))
	_, err = pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-100",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-100",
	})
	require.NoError(t, err)

	sched := orchestrator.NewScheduler(bot, monitor, nil, orchestrator.SchedulerConfig{
		StandupInterval:   10 * time.Millisecond,
		HealthInterval:    time.Hour,
		RetroAfterTickets: 100,
	})
	sched.SetPipeline(pipeStore)
	sched.SetAgents(map[agent.Role]*agent.Agent{agent.RolePM: pmAgent})
	sched.SetRoster(map[agent.Role]string{
		agent.RolePM:        "Nova",
		agent.RoleEngineer1: "Mara",
		agent.RoleEngineer2: "Kael",
		agent.RoleEngineer3: "Zeph",
	})

	timedCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()

	_ = sched.Run(timedCtx)

	// PM should have been called to compose the standup.
	pmRunner.mu.Lock()
	pmCalls := len(pmRunner.calls)
	pmRunner.mu.Unlock()
	assert.GreaterOrEqual(t, pmCalls, 1, "PM should compose standup")

	// Slack should have received a message.
	mu.Lock()
	defer mu.Unlock()
	assert.NotEmpty(t, postedTexts)
}

func TestScheduler_Standup_NoPM_FallsBackToHealth(t *testing.T) {
	t.Parallel()

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

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	bot := newSchedulerStandupBot(t, server.URL)

	roles := []agent.Role{agent.RoleEngineer1}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 30 * time.Minute,
	})

	sched := orchestrator.NewScheduler(bot, monitor, nil, orchestrator.SchedulerConfig{
		StandupInterval:   10 * time.Millisecond,
		HealthInterval:    time.Hour,
		RetroAfterTickets: 100,
	})
	// No SetAgents — PM not available.

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_ = sched.Run(ctx)

	// Should still post health summary as fallback.
	mu.Lock()
	defer mu.Unlock()
	assert.NotEmpty(t, postedTexts)
}

func TestScheduler_Standup_PMPassResponse_FallsBackToHealth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// PM returns PASS — should fall back to health summary.
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

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

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	bot := newSchedulerStandupBot(t, server.URL)

	roles := []agent.Role{agent.RoleEngineer1}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 30 * time.Minute,
	})

	sched := orchestrator.NewScheduler(bot, monitor, nil, orchestrator.SchedulerConfig{
		StandupInterval:   10 * time.Millisecond,
		HealthInterval:    time.Hour,
		RetroAfterTickets: 100,
	})
	sched.SetAgents(map[agent.Role]*agent.Agent{agent.RolePM: pmAgent})

	timedCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()

	_ = sched.Run(timedCtx)

	mu.Lock()
	defer mu.Unlock()
	assert.NotEmpty(t, postedTexts, "should still post fallback summary")
}

func TestScheduler_Standup_PMPromptContainsAgentNames(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	bot := newSchedulerStandupBot(t, server.URL)

	roles := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 30 * time.Minute,
	})

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Everyone is idle."}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

	sched := orchestrator.NewScheduler(bot, monitor, nil, orchestrator.SchedulerConfig{
		StandupInterval:   10 * time.Millisecond,
		HealthInterval:    time.Hour,
		RetroAfterTickets: 100,
	})
	sched.SetAgents(map[agent.Role]*agent.Agent{agent.RolePM: pmAgent})
	sched.SetRoster(map[agent.Role]string{
		agent.RoleEngineer1: "Mara",
		agent.RoleEngineer2: "Kael",
	})

	timedCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()

	_ = sched.Run(timedCtx)

	// Check that the PM's prompt contained agent names.
	pmRunner.mu.Lock()
	calls := make([]fakeCall, len(pmRunner.calls))
	copy(calls, pmRunner.calls)
	pmRunner.mu.Unlock()

	found := false
	for _, call := range calls {
		if strings.Contains(call.stdin, "Mara") && strings.Contains(call.stdin, "Kael") {
			found = true
			break
		}
	}
	assert.True(t, found, "PM prompt should contain agent names from roster")
}

func TestScheduler_SetPipeline_NilSafe(t *testing.T) {
	t.Parallel()

	sched := newTestScheduler()
	assert.NotPanics(t, func() {
		sched.SetPipeline(nil)
	})
}

func TestScheduler_SetAgents_NilSafe(t *testing.T) {
	t.Parallel()

	sched := newTestScheduler()
	assert.NotPanics(t, func() {
		sched.SetAgents(nil)
	})
}

func TestScheduler_SetRoster_NilSafe(t *testing.T) {
	t.Parallel()

	sched := newTestScheduler()
	assert.NotPanics(t, func() {
		sched.SetRoster(nil)
	})
}

func newSchedulerStandupBot(t *testing.T, serverURL string) *islack.Bot {
	t.Helper()

	return islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
		Channels: map[string]string{
			"standup":     "C001",
			"feed":        "C002",
			"triage":      "C003",
			"engineering": "C004",
		},
		Personas: map[agent.Role]islack.Persona{
			agent.RolePM: {Role: agent.RolePM, Name: "Nova"},
		},
		MinSpacing: 0,
	}, serverURL+"/")
}
