package orchestrator_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSchedulerTestBot(t *testing.T, handler http.Handler) *slack.Bot {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return slack.NewBotWithURL(slack.BotConfig{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
		Channels: map[string]string{
			"standup":     "C001",
			"feed":        "C002",
			"triage":      "C003",
			"engineering": "C004",
		},
		Personas: map[agent.Role]slack.Persona{
			agent.RolePM: {Role: agent.RolePM, Name: "Nova"},
		},
		MinSpacing: 0,
	}, server.URL+"/")
}

func TestScheduler_RunStandup_NilBot_DoesNotPanic(t *testing.T) {
	t.Parallel()

	sched := newTestScheduler()
	ctx := context.Background()

	// runStandup is called indirectly via Run, but we can trigger it
	// by reaching the retro threshold which calls runRetro (also nil-safe)
	// and by running completions. The nil-bot guard in runStandup is the
	// path we need. We trigger it via the Run loop with a short standup.
	roles := []agent.Role{agent.RoleEngineer1}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 30 * time.Minute,
	})

	nilBotSched := orchestrator.NewScheduler(nil, monitor, nil, orchestrator.SchedulerConfig{
		StandupInterval:   10 * time.Millisecond,
		HealthInterval:    time.Hour,
		RetroAfterTickets: 100,
	})

	ctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()

	err := nilBotSched.Run(ctx)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	_ = sched // keep reference to avoid unused
}

func TestScheduler_RunStandup_WithBot_PostsToStandup(t *testing.T) {
	t.Parallel()

	var postedChannel string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		postedChannel = req.FormValue("channel")

		resp := map[string]interface{}{"ok": true, "channel": postedChannel, "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newSchedulerTestBot(t, handler)
	roles := []agent.Role{agent.RoleEngineer1}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 30 * time.Minute,
	})

	sched := orchestrator.NewScheduler(bot, monitor, nil, orchestrator.SchedulerConfig{
		StandupInterval:   10 * time.Millisecond,
		HealthInterval:    time.Hour,
		RetroAfterTickets: 100,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_ = sched.Run(ctx)

	assert.Equal(t, "C001", postedChannel)
}

func TestScheduler_RunHealthCheck_NilAlerter_DoesNotPanic(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 30 * time.Minute,
	})

	sched := orchestrator.NewScheduler(nil, monitor, nil, orchestrator.SchedulerConfig{
		StandupInterval:   time.Hour,
		HealthInterval:    10 * time.Millisecond,
		RetroAfterTickets: 100,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := sched.Run(ctx)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestScheduler_RunHealthCheck_WithAlerter_CallsCheckAndAlert(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime:       30 * time.Minute,
		MaxConsecutiveErrors: 2,
	})

	monitor.RecordError(agent.RoleEngineer1, "err1")
	monitor.RecordError(agent.RoleEngineer1, "err2")

	var msgCount int
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		msgCount++
		resp := map[string]interface{}{"ok": true, "channel": "C003", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newSchedulerTestBot(t, handler)
	alerter := health.NewAlerter(monitor, bot, "triage")

	sched := orchestrator.NewScheduler(bot, monitor, alerter, orchestrator.SchedulerConfig{
		StandupInterval:   time.Hour,
		HealthInterval:    10 * time.Millisecond,
		RetroAfterTickets: 100,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_ = sched.Run(ctx)

	assert.Greater(t, msgCount, 0)
}

func TestScheduler_RunRetro_NilBot_DoesNotPanic(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 30 * time.Minute,
	})

	sched := orchestrator.NewScheduler(nil, monitor, nil, orchestrator.SchedulerConfig{
		StandupInterval:   time.Hour,
		HealthInterval:    time.Hour,
		RetroAfterTickets: 2,
	})

	ctx := context.Background()
	sched.RecordCompletion(ctx)
	sched.RecordCompletion(ctx)

	// If we got here without panic, the nil bot guard works
	assert.Equal(t, 0, sched.CompletedCount())
}

func TestScheduler_RunRetro_WithBot_PostsToFeed(t *testing.T) {
	t.Parallel()

	var postedChannel string
	var postedText string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		postedChannel = req.FormValue("channel")
		postedText = req.FormValue("text")

		resp := map[string]interface{}{"ok": true, "channel": "C002", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newSchedulerTestBot(t, handler)
	roles := []agent.Role{agent.RoleEngineer1}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime: 30 * time.Minute,
	})

	sched := orchestrator.NewScheduler(bot, monitor, nil, orchestrator.SchedulerConfig{
		StandupInterval:   time.Hour,
		HealthInterval:    time.Hour,
		RetroAfterTickets: 2,
	})

	ctx := context.Background()
	sched.RecordCompletion(ctx)
	sched.RecordCompletion(ctx)

	assert.Equal(t, 0, sched.CompletedCount())
	assert.Equal(t, "C002", postedChannel)
	assert.Contains(t, postedText, "Retro")
}

func TestScheduler_RunLoop_StandupAndHealth_BothFire(t *testing.T) {
	t.Parallel()

	var msgCount int
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		msgCount++
		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newSchedulerTestBot(t, handler)
	roles := []agent.Role{agent.RoleEngineer1}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime:       10 * time.Millisecond,
		MaxConsecutiveErrors: 1,
	})

	monitor.RecordSessionStart(agent.RoleEngineer1)
	time.Sleep(15 * time.Millisecond)

	alerter := health.NewAlerter(monitor, bot, "triage")

	sched := orchestrator.NewScheduler(bot, monitor, alerter, orchestrator.SchedulerConfig{
		StandupInterval:   10 * time.Millisecond,
		HealthInterval:    10 * time.Millisecond,
		RetroAfterTickets: 100,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := sched.Run(ctx)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Greater(t, msgCount, 0)
}

func TestScheduler_RunHealthCheck_AlerterError_DoesNotPanic(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime:       30 * time.Minute,
		MaxConsecutiveErrors: 1,
	})
	monitor.RecordError(agent.RoleEngineer1, "boom")

	// Create a bot that always errors on PostMessage
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"ok": false, "error": "channel_not_found"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newSchedulerTestBot(t, handler)
	alerter := health.NewAlerter(monitor, bot, "triage")

	sched := orchestrator.NewScheduler(bot, monitor, alerter, orchestrator.SchedulerConfig{
		StandupInterval:   time.Hour,
		HealthInterval:    10 * time.Millisecond,
		RetroAfterTickets: 100,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// Should not panic — health check logs errors but continues
	err := sched.Run(ctx)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
