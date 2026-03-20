package health_test

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func slackOKHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"ok":      true,
			"channel": "C001",
			"ts":      "1234567890.123456",
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})
}

func newHealthTestBot(t *testing.T, handler http.Handler) *slack.Bot {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return slack.NewBotWithURL(slack.BotConfig{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
		Channels: map[string]string{
			"triage":      "C001",
			"engineering": "C002",
			"standup":     "C003",
		},
		Personas: map[agent.Role]slack.Persona{
			agent.RolePM: {Role: agent.RolePM, Name: "Nova"},
		},
		MinSpacing: 0,
	}, server.URL+"/")
}

func TestCheckAndAlert_NoUnhealthyAgents_ReturnsZero(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()
	bot := newHealthTestBot(t, slackOKHandler())
	alerter := health.NewAlerter(mon, bot, "triage")

	count, err := alerter.CheckAndAlert(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCheckAndAlert_FailingAgent_PostsAlert(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()
	for idx := 0; idx < 3; idx++ {
		mon.RecordError(agent.RoleEngineer1, "connection timeout")
	}

	var postedMessages []string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		postedMessages = append(postedMessages, req.FormValue("text"))

		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newHealthTestBot(t, handler)
	alerter := health.NewAlerter(mon, bot, "triage")

	count, err := alerter.CheckAndAlert(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.Len(t, postedMessages, 1)
	assert.Contains(t, postedMessages[0], "engineer-1")
	assert.Contains(t, postedMessages[0], "failing")
}

func TestCheckAndAlert_SlackError_ReturnsPartialCount(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime:       30 * time.Minute,
		MaxConsecutiveErrors: 2,
	})

	for idx := 0; idx < 2; idx++ {
		mon.RecordError(agent.RoleEngineer1, "error 1")
		mon.RecordError(agent.RoleEngineer2, "error 2")
	}

	callCount := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		callCount++
		resp := map[string]interface{}{"ok": false, "error": "channel_not_found"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newHealthTestBot(t, handler)
	alerter := health.NewAlerter(mon, bot, "triage")

	count, err := alerter.CheckAndAlert(context.Background())

	require.Error(t, err)
	assert.Equal(t, 0, count)
	assert.Contains(t, err.Error(), "posting alert for")
}

func TestCheckAndAlert_MultipleUnhealthy_PostsAll(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2}
	mon := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime:       10 * time.Millisecond,
		MaxConsecutiveErrors: 5,
	})

	mon.RecordSessionStart(agent.RoleEngineer1)
	mon.RecordSessionStart(agent.RoleEngineer2)
	time.Sleep(15 * time.Millisecond)

	var msgCount int
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		msgCount++
		resp := map[string]interface{}{"ok": true, "channel": "C001", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	bot := newHealthTestBot(t, handler)
	alerter := health.NewAlerter(mon, bot, "triage")

	count, err := alerter.CheckAndAlert(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, 2, msgCount)
}

func TestFormatAlert_SlowState_IncludesSlowMessage(t *testing.T) {
	t.Parallel()

	states := []health.AgentHealth{
		{Role: agent.RoleEngineer1, State: health.StateSlow},
	}

	summary := health.FormatHealthSummary(states)

	assert.Contains(t, summary, "SLOW")
	assert.Contains(t, summary, "engineer-1")
}

func TestFormatAlert_StuckState_IncludesStuckMessage(t *testing.T) {
	t.Parallel()

	states := []health.AgentHealth{
		{Role: agent.RoleEngineer1, State: health.StateStuck},
	}

	summary := health.FormatHealthSummary(states)

	assert.Contains(t, summary, "STUCK")
}

func TestFormatHealthSummary_HealthyWithNoError_OmitsErrorInfo(t *testing.T) {
	t.Parallel()

	states := []health.AgentHealth{
		{Role: agent.RolePM, State: health.StateHealthy, ErrorCount: 0},
	}

	summary := health.FormatHealthSummary(states)

	assert.Contains(t, summary, "OK")
	assert.NotContains(t, summary, "errors")
	assert.NotContains(t, summary, "last error")
}

func TestFormatHealthSummary_IdleState_ShowsIdleIcon(t *testing.T) {
	t.Parallel()

	states := []health.AgentHealth{
		{Role: agent.RoleDesigner, State: health.StateIdle},
	}

	summary := health.FormatHealthSummary(states)

	assert.Contains(t, summary, "IDLE")
	assert.Contains(t, summary, "designer")
}

func TestFormatHealthSummary_ErrorCountWithoutLastError(t *testing.T) {
	t.Parallel()

	states := []health.AgentHealth{
		{Role: agent.RoleEngineer1, State: health.StateFailing, ErrorCount: 5},
	}

	summary := health.FormatHealthSummary(states)

	assert.Contains(t, summary, "5 errors")
	assert.NotContains(t, summary, "last error")
}

func TestFormatHealthSummary_LastErrorWithoutErrorCount(t *testing.T) {
	t.Parallel()

	states := []health.AgentHealth{
		{Role: agent.RoleEngineer1, State: health.StateHealthy, LastError: "oops"},
	}

	summary := health.FormatHealthSummary(states)

	assert.Contains(t, summary, "last error: oops")
}

func TestNewAlerter_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	mon := newTestMonitor()
	alerter := health.NewAlerter(mon, nil, "triage")

	assert.NotNil(t, alerter)
}
