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

func setupTechLeadOrch(t *testing.T, tlRunner *fakeProcessRunner) *orchestrator.Orchestrator {
	t.Helper()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	tlAgent := setupAgentWithRole(t, tlRunner, agent.RoleTechLead)
	tlAgent.SetMemoryStores(memory.NewGraphStore(nil), memory.NewFactStore(nil))

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:       pmAgent,
		agent.RoleTechLead: tlAgent,
	}

	return orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
}

func TestTechLeadDiscussionReview_NoTechLead_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assert.NotPanics(t, func() {
		orch.TechLeadDiscussionReview(ctx, "engineering", "my plan", "JAM-1")
	})
}

func TestTechLeadDiscussionReview_WithTechLead_PostsReview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tlRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"The approach looks sound but consider the module boundary."}` + "\n"),
	}
	orch := setupTechLeadOrch(t, tlRunner)

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	chatRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	for _, role := range allRoles {
		agents[role] = buildAgent(t, chatRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, bot, nil)
	orch.SetConversationEngine(conversation)

	conversation.OnMessage(ctx, "engineering", "ceo", "here's the plan")

	orch.TechLeadDiscussionReview(ctx, "engineering", "I'll add auth middleware", "JAM-42")

	require.NotEmpty(t, tlRunner.calls)
	assert.Contains(t, tlRunner.calls[0].stdin, "JAM-42")
}

func TestStoreArchitectureDecision_NoTechLead_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	assert.NotPanics(t, func() {
		orch.StoreArchitectureDecision(ctx, "use interfaces", "JAM-1")
	})
}

func TestStoreArchitectureDecision_WithFactStore_StoresBelief(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	tlRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"APPROVED"}` + "\n"),
	}
	tlAgent := buildAgent(t, tlRunner, agent.RoleTechLead, memDB)

	factStore := memory.NewFactStore(memDB)
	graphStore := memory.NewGraphStore(memDB)
	tlAgent.SetMemoryStores(graphStore, factStore)

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RoleTechLead: tlAgent},
		checkIns, nil, nil,
	)

	orch.StoreArchitectureDecision(ctx, "prefer interfaces at boundaries", "JAM-42")

	beliefs, err := factStore.TopBeliefs(ctx, 5)
	require.NoError(t, err)
	assert.NotEmpty(t, beliefs)
	assert.Contains(t, beliefs[0].Content, "prefer interfaces at boundaries")
}

func TestRunConversationalArchReview_NoTechLead_ReturnsApproved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	outcome := orch.RunConversationalArchReview(ctx, "https://github.com/test-org/test-repo/pull/1", "T-1", agent.RoleEngineer1)
	assert.Equal(t, orchestrator.ReviewApproved, outcome)
}

func TestRunConversationalArchReview_Approved_StoresDecision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	tlRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Architecture should use the repository pattern. APPROVED"}` + "\n"),
	}
	tlAgent := buildAgent(t, tlRunner, agent.RoleTechLead, memDB)

	factStore := memory.NewFactStore(memDB)
	graphStore := memory.NewGraphStore(memDB)
	tlAgent.SetMemoryStores(graphStore, factStore)

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RoleTechLead: tlAgent},
		checkIns, nil, nil,
	)

	outcome := orch.RunConversationalArchReview(ctx, "https://github.com/test-org/test-repo/pull/5", "JAM-10", agent.RoleEngineer1)
	assert.Equal(t, orchestrator.ReviewApproved, outcome)
}

func TestTechLeadDiscussionReview_PassResponse_DoesNotPost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tlRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}
	orch := setupTechLeadOrch(t, tlRunner)

	assert.NotPanics(t, func() {
		orch.TechLeadDiscussionReview(ctx, "engineering", "plan", "JAM-1")
	})
}

func TestTechLeadDiscussionReview_Error_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tlRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}
	orch := setupTechLeadOrch(t, tlRunner)

	assert.NotPanics(t, func() {
		orch.TechLeadDiscussionReview(ctx, "engineering", "plan", "JAM-1")
	})
}
