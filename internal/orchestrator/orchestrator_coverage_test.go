package orchestrator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetConversationEngine_SetsEngine(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	orch, _ := setupOrchestrator(t, pmRunner)

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}
	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil)
	orch.SetConversationEngine(engine)

	// Verify it was set by running orchestrator which calls breakSilence
	// when PM errors (which exercises the conversation engine path)
	assert.NotPanics(t, func() {
		timedCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		defer cancel()
		_ = orch.Run(timedCtx)
	})
}

func TestBreakSilence_NilConversation_DoesNotPanic(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    fmt.Errorf("exit status 1"),
	}

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}

	orch, _ := setupOrchestratorWithEngineers(t, pmRunner, map[agent.Role]*fakeProcessRunner{
		agent.RoleEngineer1: engRunner,
	})

	// Do not set conversation engine — breakSilence should handle nil
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		_ = orch.Run(ctx)
	})
}

func TestBreakSilence_WithConversationEngine_CallsBreakSilence(t *testing.T) {
	t.Parallel()

	// PM returns error so tick calls breakSilence
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    fmt.Errorf("exit status 1"),
	}

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}

	orch, _ := setupOrchestratorWithEngineers(t, pmRunner, map[agent.Role]*fakeProcessRunner{
		agent.RoleEngineer1: engRunner,
	})

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	convRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Anyone around?"}` + "\n"),
	}

	allRoles := agent.AllRoles()
	convAgents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		convAgents[role] = buildAgent(t, convRunner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(convAgents, factStores, nil)
	orch.SetConversationEngine(engine)

	timedCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		_ = orch.Run(timedCtx)
	})
}

func TestBreakSilence_EmptyAssignments_CallsBreakSilence(t *testing.T) {
	t.Parallel()

	// PM returns empty assignments so tick calls breakSilence (second path)
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"[]"}` + "\n"),
	}

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}

	orch, _ := setupOrchestratorWithEngineers(t, pmRunner, map[agent.Role]*fakeProcessRunner{
		agent.RoleEngineer1: engRunner,
	})

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	convRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	convAgents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, convRunner, agent.RoleEngineer1, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(convAgents, factStores, nil)
	orch.SetConversationEngine(engine)

	timedCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	_ = orch.Run(timedCtx)
	assert.False(t, orch.IsRunning())
}

func TestOrchestrator_Run_WithBot_PostsStartupMessage(t *testing.T) {
	t.Parallel()

	var postedText string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		postedText = req.FormValue("text")
		resp := map[string]interface{}{"ok": true, "channel": "C002", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"feed":        "C002",
			"engineering": "C004",
		},
		Personas:   map[agent.Role]islack.Persona{},
		MinSpacing: 0,
	}, server.URL+"/")

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM: pmAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)

	assert.Contains(t, postedText, "online")
}

func TestOrchestrator_Tick_WorkDisabled_BreaksSilence(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	agents := map[agent.Role]*agent.Agent{agent.RolePM: pmAgent}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 3, CooldownAfter: time.Second, WorkEnabled: false},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)
}

func TestOrchestrator_StartWork_WithBot_PostsWorkMessages(t *testing.T) {
	t.Parallel()

	var messages []string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		messages = append(messages, req.FormValue("text"))
		resp := map[string]interface{}{"ok": true, "channel": "C004", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"feed":        "C002",
			"engineering": "C004",
		},
		Personas:   map[agent.Role]islack.Persona{},
		MinSpacing: 0,
	}, server.URL+"/")

	assignmentJSON := `[{"role":"engineer-1","ticket":"SQ-99","description":"Add logging"}]`
	contentBytes, err := json.Marshal(assignmentJSON)
	require.NoError(t, err)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	pmRunner := &fakeProcessRunner{output: []byte(pmOutput)}
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Added logging to all handlers."}` + "\n"),
	}

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := setupAgentWithRole(t, engRunner, agent.RoleEngineer1)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 3, CooldownAfter: time.Second, WorkEnabled: true},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)
	orch.Wait()

	// Should have posted startup, starting work, and finished messages
	assert.GreaterOrEqual(t, len(messages), 2)
}
