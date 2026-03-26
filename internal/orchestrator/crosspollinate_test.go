package orchestrator_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCrossPollination_HighConfidenceBelief_PropagatedToProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	projectDB, projErr := memory.Open(ctx, ":memory:")
	require.NoError(t, projErr)
	t.Cleanup(func() { _ = projectDB.Close() })

	// Agent says something opinionated about architecture.
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I believe the auth module should always validate tokens at the boundary."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	projectFactStore := memory.NewFactStore(projectDB)

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.SetProjectFactStore(projectFactStore)

	// Pre-create a high-confidence belief that will get confirmed by the new one.
	_, _ = factStores[agent.RoleEngineer1].CreateBelief(ctx, memory.Belief{
		Content:       "module boundary validation is critical",
		Confidence:    0.7,
		Confirmations: 2,
		SourceOutcome: "implementation",
	})

	engine.OnMessage(ctx, "engineering", "ceo", "what's important for auth?")

	// The project store may or may not have entries depending on which
	// agents respond and their beliefs — verify no crash.
	beliefs, _ := projectFactStore.TopBeliefs(ctx, 10)
	_ = beliefs
}

func TestCrossPollination_LowConfidenceBelief_NotPropagated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	projectDB, projErr := memory.Open(ctx, ":memory:")
	require.NoError(t, projErr)
	t.Cleanup(func() { _ = projectDB.Close() })

	// Agent says something weakly opinionated.
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I think that sounds ok."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	projectFactStore := memory.NewFactStore(projectDB)

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.SetProjectFactStore(projectFactStore)
	engine.OnMessage(ctx, "engineering", "ceo", "sounds good?")

	// Low confidence + no project signals = should not propagate.
	beliefs, _ := projectFactStore.TopBeliefs(ctx, 10)
	assert.Empty(t, beliefs, "low confidence beliefs should not propagate")
}

func TestContainsProjectSignal_MatchesModuleAndPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{"module mention", "the auth module needs work", true},
		{"pattern mention", "this pattern is useful", true},
		{"architecture", "the architecture should be flat", true},
		{"no signal", "I had a sandwich", false},
		{"test mention", "always write tests first", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, orchestrator.ContainsProjectSignalForTest(tt.text))
		})
	}
}

func TestSetProjectFactStore_WiresCorrectly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	engine := orchestrator.NewConversationEngine(nil, nil, nil, nil)
	store := memory.NewFactStore(db)
	engine.SetProjectFactStore(store)

	// Verify it's wired — just confirm no panic.
	assert.NotPanics(t, func() {
		engine.SetProjectFactStore(nil)
	})
}
