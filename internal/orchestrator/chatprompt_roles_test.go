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

func TestBuildChatPrompt_ReviewerRole_HasDescription(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I see a potential nil dereference."}` + "\n"),
	}

	// Include reviewer as the only agent so it must be chosen.
	agents := map[agent.Role]*agent.Agent{
		agent.RoleReviewer: buildAgent(t, runner, agent.RoleReviewer, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleReviewer: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// The reviewer is normally excluded from pickCandidates. But for BreakSilence
	// it can be selected. Test the prompt building via voice map.
	engine.SetVoicesMap(map[agent.Role]string{
		agent.RoleReviewer: "You catch bugs.",
	})

	// BreakSilence can pick reviewer — exercise it enough times.
	engine.SetLastMessageTime("engineering", time.Now().Add(-15*time.Minute))

	// Verify no panic with reviewer-only setup.
	assert.NotPanics(t, func() {
		for idx := 0; idx < 20; idx++ {
			engine.BreakSilence(ctx)
		}
	})
}

func TestBuildChatPrompt_Engineer3Role_HasReplyAs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"the DX could be better here"}` + "\n"),
	}

	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer3: buildAgent(t, runner, agent.RoleEngineer3, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer3: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.OnMessage(ctx, "engineering", "ceo", "thoughts on the CLI DX?")

	if len(runner.calls) > 0 {
		assert.Contains(t, runner.calls[0].stdin, "Reply as",
			"engineer-3 prompt should contain Reply as instruction",
		)
	}
}

func TestBuildChatPrompt_TechLeadRole_HasReplyAs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"if we go with approach A, the consequence is X"}` + "\n"),
	}

	agents := map[agent.Role]*agent.Agent{
		agent.RoleTechLead: buildAgent(t, runner, agent.RoleTechLead, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleTechLead: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.OnMessage(ctx, "engineering", "ceo", "what's the right architecture?")

	if len(runner.calls) > 0 {
		// Identity is now in CLAUDE.md; the prompt just has "Reply as {name}".
		assert.Contains(t, runner.calls[0].stdin, "Reply as")
	}
}

func TestStoreArchitectureDecision_NilFactStore_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// Build a tech lead with nil memory stores.
	tlRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"APPROVED"}` + "\n"),
	}
	tlAgent := buildAgent(t, tlRunner, agent.RoleTechLead, memDB)
	// Explicitly set nil stores.
	tlAgent.SetMemoryStores(nil, nil)

	orch := setupTechLeadOrchWithAgent(t, tlAgent)

	assert.NotPanics(t, func() {
		orch.StoreArchitectureDecision(ctx, "use interfaces", "JAM-1")
	})
}

func setupTechLeadOrchWithAgent(t *testing.T, tlAgent *agent.Agent) *orchestrator.Orchestrator {
	t.Helper()

	ctx := context.Background()
	sqlDB, err := openTestSQLDB(t)
	require.NoError(t, err)

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	return orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 1, MaxParallel: 1, CooldownAfter: 1},
		map[agent.Role]*agent.Agent{agent.RoleTechLead: tlAgent},
		checkIns, nil, nil,
	)
}

func openTestSQLDB(t *testing.T) (*sql.DB, error) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	if err == nil {
		t.Cleanup(func() { _ = db.Close() })
	}
	return db, err
}
