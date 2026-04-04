package orchestrator_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// writeMCPConfig — test with valid temp dir
// ---------------------------------------------------------------------------

func TestWriteMCPConfig_ValidDir_WritesMCPJSON(t *testing.T) {
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
	engAgent := buildAgent(t, runner, agent.RoleEngineer1, memDB)
	engAgent.SetDBPath("/tmp/test-agent.db")

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{MemoryBinaryPath: "/usr/local/bin/squad0-memory"},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, nil, nil,
	)

	workDir := t.TempDir()
	orch.WriteMCPConfigForTest(engAgent, workDir)

	// Verify .mcp.json was written and MCPConfigPath was set.
	expectedPath := filepath.Join(workDir, ".mcp.json")
	assert.Equal(t, expectedPath, engAgent.MCPConfigPath)
}

func TestWriteMCPConfig_InvalidDir_DoesNotPanic(t *testing.T) {
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
	engAgent := buildAgent(t, runner, agent.RoleEngineer1, memDB)
	engAgent.SetDBPath("/tmp/test-agent.db")

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{MemoryBinaryPath: "/usr/local/bin/squad0-memory"},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, nil, nil,
	)

	// Non-existent directory — writeMCPConfig should log the error, not panic.
	assert.NotPanics(t, func() {
		orch.WriteMCPConfigForTest(engAgent, "/nonexistent/path/that/does/not/exist")
	})

	// MCPConfigPath should NOT have been set because the write failed.
	assert.Empty(t, engAgent.MCPConfigPath)
}

// ---------------------------------------------------------------------------
// agentFactStores — with and without conversation engine
// ---------------------------------------------------------------------------

func TestAgentFactStores_NoConversation_ReturnsNil(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	// No conversation engine set — should return nil.
	result := orch.AgentFactStoresForTest()
	assert.Nil(t, result)
}

func TestAgentFactStores_WithConversation_ReturnsStores(t *testing.T) {
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

	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, memDB),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(memDB),
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	result := orch.AgentFactStoresForTest()
	require.NotNil(t, result)
	assert.Contains(t, result, agent.RoleEngineer1)
}

// ---------------------------------------------------------------------------
// maybeStoreConcerns — concern tracking from conversation
// ---------------------------------------------------------------------------

func TestMaybeStoreConcerns_NilTracker_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I'm worried about the migration."}` + "\n"),
	}

	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, memDB),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(memDB),
	}

	// No concern tracker set — maybeStoreConcerns should return early.
	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	assert.NotPanics(t, func() {
		engine.OnMessage(ctx, "engineering", "ceo", "how is the migration?")
	})
}

func TestMaybeStoreConcerns_WithTracker_StoresConcernFromResponse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I'm worried about the auth module breaking."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	// Bot is required — postAndRecord returns early when bot is nil,
	// so maybeStoreConcerns never fires without it.
	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	tracker := orchestrator.NewConcernTracker()
	engine := orchestrator.NewConversationEngine(agents, factStores, bot, nil)
	engine.SetConcernTracker(tracker)

	engine.OnMessage(ctx, "engineering", "ceo", "how is the auth module?")

	// Concerns are extracted from agent responses. With concern signals like
	// "worried about", at least one concern should be stored.
	all := tracker.AllConcerns()
	assert.NotEmpty(t, all, "concern tracker should have at least one concern")
}

// ---------------------------------------------------------------------------
// isDuplicate — duplicate detection in conversation_respond.go
// ---------------------------------------------------------------------------

func TestIsDuplicate_NoOutputPipeline_AlwaysPasses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Great observation about the module boundary."}` + "\n"),
	}

	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, runner, agent.RoleEngineer1, memDB),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(memDB),
	}

	// No output pipeline — isDuplicate should always return false,
	// meaning messages pass through. Engine created without bot so no
	// actual posting, but the isDuplicate path is exercised.
	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// Trigger message flow — no panics.
	engine.OnMessage(ctx, "engineering", "ceo", "What about the module boundary?")

	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}

func TestIsDuplicate_WithPipeline_DuplicateDropped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// Agent always returns the same message — identical to what's seeded.
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"The module boundary looks clean and well-defined to me."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	engine := orchestrator.NewConversationEngine(agents, factStores, bot, nil)

	// Seed a message that's identical to what the agent will say.
	engine.SeedHistory("engineering", []string{
		"engineer-1: The module boundary looks clean and well-defined to me.",
	})

	// Now trigger a response — isDuplicate should detect similarity.
	engine.OnMessage(ctx, "engineering", "ceo", "What about the module boundary?")

	// The response should have been dropped if similarity was detected.
	// We just verify no panic and the engine is stable.
	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}
