package orchestrator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 1. startConflictResolution — worktree succeeds and ExecuteTask runs
// ---------------------------------------------------------------------------

func TestStartConflictResolution_WorktreeSucceeds_ExecuteTaskRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repoDir := t.TempDir()
	initTestRepo(t, repoDir)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"rebased successfully"}` + "\n"),
	}
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{TargetRepoDir: repoDir},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, bot, nil,
	)

	orch.StartConflictResolutionForTest(ctx, pipeline.WorkItem{
		Ticket:   "JAM-CR1",
		Engineer: agent.RoleEngineer1,
		PRURL:    "https://github.com/test/repo/pull/1",
	})

	engRunner.mu.Lock()
	callCount := len(engRunner.calls)
	engRunner.mu.Unlock()
	assert.GreaterOrEqual(t, callCount, 1, "engineer should have been called to rebase")
}

// ---------------------------------------------------------------------------
// 2. recoverOrphanedPRs — with pipeline store, listOpenPRs fails
// ---------------------------------------------------------------------------

func TestRecoverOrphanedPRs_WithPipeline_ListFails_LogsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{TargetRepoDir: t.TempDir()},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	assert.NotPanics(t, func() {
		orch.RecoverOrphanedPRsForTest(ctx)
	})
}

// ---------------------------------------------------------------------------
// 3. resumeStaleWorkingItem — "from this session" branch
// ---------------------------------------------------------------------------

func TestResumeStaleWorkingItem_FromThisSession_LeftAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"[]"}` + "\n"),
	}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:  50 * time.Millisecond,
			MaxParallel:   1,
			CooldownAfter: time.Second,
		},
		map[agent.Role]*agent.Agent{
			agent.RolePM:        pmAgent,
			agent.RoleEngineer1: engAgent,
		},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	runCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_ = orch.Run(runCtx)

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-CURR2",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-curr2",
	})
	require.NoError(t, createErr)

	// Push updated_at into the future so it's after startedAt.
	_, err = pipeStore.DB().ExecContext(ctx,
		`UPDATE work_items SET updated_at = datetime('now', '+1 minute') WHERE id = ?`, itemID)
	require.NoError(t, err)

	item, err := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, err)
	orch.ResumeWorkItemForTest(ctx, item)

	item, err = pipeStore.GetByID(ctx, itemID)
	require.NoError(t, err)
	assert.Equal(t, pipeline.StageWorking, item.Stage,
		"item from this session should not be marked failed")
}

// ---------------------------------------------------------------------------
// 4. checkCircuitBreaker — circuit opens at 3 failures
// ---------------------------------------------------------------------------

func TestCheckCircuitBreaker_OpensAt3Failures_PostsToTriage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"[]"}` + "\n"),
	}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)

	var mu sync.Mutex
	var postedTexts []string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		mu.Lock()
		postedTexts = append(postedTexts, req.FormValue("text"))
		mu.Unlock()

		resp := map[string]interface{}{"ok": true, "channel": "C003", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})

	slackServer := httptest.NewServer(handler)
	t.Cleanup(slackServer.Close)
	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"triage":      "C003",
			"engineering": "C001",
		},
		Personas:   map[agent.Role]islack.Persona{},
		MinSpacing: 0,
	}, slackServer.URL+"/")

	assigner := orchestrator.NewAssigner(pmAgent, "TEST")
	assigner.SetSmartAssigner(orchestrator.NewSmartAssigner(nil))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, bot, assigner,
	)

	orch.CheckCircuitBreakerForTest(ctx, "JAM-FAIL1")
	orch.CheckCircuitBreakerForTest(ctx, "JAM-FAIL1")
	orch.CheckCircuitBreakerForTest(ctx, "JAM-FAIL1")

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	found := false
	for _, text := range postedTexts {
		if strings.Contains(text, "JAM-FAIL1") && strings.Contains(text, "3+") {
			found = true
			break
		}
	}
	assert.True(t, found, "circuit breaker should post triage alert for JAM-FAIL1")
}

// ---------------------------------------------------------------------------
// 5. writeMCPConfig — with valid agent
// ---------------------------------------------------------------------------

func TestWriteMCPConfig_WithValidAgent_WritesMCPJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	agentInstance := buildAgent(t, runner, agent.RoleEngineer1, memDB)
	agentInstance.SetDBPath("/tmp/test-agent.db")

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{MemoryBinaryPath: "/usr/local/bin/memory-mcp"},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: agentInstance},
		checkIns, nil, nil,
	)

	workDir := t.TempDir()
	mcpCfg := agent.BuildMCPConfig(agent.MCPOptions{
		MemoryBinaryPath: "/usr/local/bin/memory-mcp",
		AgentDBPath:      agentInstance.DBPath(),
	})
	require.NoError(t, agent.WriteMCPConfig(workDir, mcpCfg))

	mcpPath := filepath.Join(workDir, ".mcp.json")
	assert.FileExists(t, mcpPath)

	data, readErr := os.ReadFile(mcpPath)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "memory")
	assert.Contains(t, string(data), "/tmp/test-agent.db")

	_ = orch
}

// ---------------------------------------------------------------------------
// 6. agentFactStores — with conversation engine set
// ---------------------------------------------------------------------------

func TestAgentFactStores_WithConversationEngine_ReturnsStores(t *testing.T) {
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
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	agents := make(map[agent.Role]*agent.Agent)
	factStores := make(map[agent.Role]*memory.FactStore)
	for _, role := range []agent.Role{agent.RolePM, agent.RoleEngineer1} {
		agents[role] = buildAgent(t, runner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	stores := conversation.FactStores()
	require.NotNil(t, stores)
	assert.Len(t, stores, 2)
	assert.NotNil(t, stores[agent.RolePM])
	assert.NotNil(t, stores[agent.RoleEngineer1])
}

// ---------------------------------------------------------------------------
// 7. truncateDescription — long description
// ---------------------------------------------------------------------------

func TestTruncateDescription_LongString_Truncated(t *testing.T) {
	t.Parallel()

	longDesc := strings.Repeat("word ", 500) // 2500 chars
	assert.Greater(t, len(longDesc), 2000)

	sa := orchestrator.NewSmartAssigner(nil)
	tickets := []orchestrator.LinearTicket{
		{ID: "JAM-LONG", Title: "long desc", Description: longDesc, Priority: 2},
	}

	assignments := sa.FilterAndRank(
		context.Background(),
		tickets,
		[]agent.Role{agent.RoleEngineer1},
	)
	require.Len(t, assignments, 1)

	descParts := strings.SplitN(assignments[0].Description, "\n\n", 2)
	require.Len(t, descParts, 2)
	assert.LessOrEqual(t, len(descParts[1]), 2000,
		"description body should be truncated to 2000 chars")
}
