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

func TestFactStores_ReturnsMap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(nil, factStores, nil, nil)

	result := engine.FactStores()

	require.NotNil(t, result)
	assert.Contains(t, result, agent.RoleEngineer1)
}

func TestPropagateIfSignificant_HighConfidence_WithProjectSignal_Propagates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	projectDB, projErr := memory.Open(ctx, ":memory:")
	require.NoError(t, projErr)
	t.Cleanup(func() { _ = projectDB.Close() })

	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(db),
	}

	projectFactStore := memory.NewFactStore(projectDB)
	engine := orchestrator.NewConversationEngine(nil, factStores, nil, nil)
	engine.SetProjectFactStore(projectFactStore)

	// High confidence + project signal => should propagate.
	belief := memory.Belief{
		Content:       "the auth module boundary should validate all inputs",
		Confidence:    0.8,
		Confirmations: 3,
	}

	engine.PropagateIfSignificantForTest(ctx, agent.RoleEngineer1, belief)

	// Should now appear in the project fact store.
	beliefs, beliefErr := projectFactStore.TopBeliefs(ctx, 10)
	require.NoError(t, beliefErr)
	assert.Len(t, beliefs, 1)
	assert.Contains(t, beliefs[0].Content, "auth module")
}

func TestPropagateIfSignificant_LowConfidence_DoesNotPropagate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	projectDB, projErr := memory.Open(ctx, ":memory:")
	require.NoError(t, projErr)
	t.Cleanup(func() { _ = projectDB.Close() })

	projectFactStore := memory.NewFactStore(projectDB)
	engine := orchestrator.NewConversationEngine(nil, nil, nil, nil)
	engine.SetProjectFactStore(projectFactStore)

	// Low confidence + single confirmation => should NOT propagate.
	belief := memory.Belief{
		Content:       "the auth module might need work",
		Confidence:    0.3,
		Confirmations: 1,
	}

	engine.PropagateIfSignificantForTest(ctx, agent.RoleEngineer1, belief)

	beliefs, beliefErr := projectFactStore.TopBeliefs(ctx, 10)
	require.NoError(t, beliefErr)
	assert.Empty(t, beliefs, "low confidence should not propagate")
}

func TestPropagateIfSignificant_HighConfidence_NoProjectSignal_DoesNotPropagate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	projectDB, projErr := memory.Open(ctx, ":memory:")
	require.NoError(t, projErr)
	t.Cleanup(func() { _ = projectDB.Close() })

	projectFactStore := memory.NewFactStore(projectDB)
	engine := orchestrator.NewConversationEngine(nil, nil, nil, nil)
	engine.SetProjectFactStore(projectFactStore)

	// High confidence but no project signals (no module, pattern, etc.).
	belief := memory.Belief{
		Content:       "I like pizza for lunch",
		Confidence:    0.9,
		Confirmations: 5,
	}

	engine.PropagateIfSignificantForTest(ctx, agent.RoleEngineer1, belief)

	beliefs, beliefErr := projectFactStore.TopBeliefs(ctx, 10)
	require.NoError(t, beliefErr)
	assert.Empty(t, beliefs, "non-project beliefs should not propagate")
}

func TestPropagateIfSignificant_NilProjectStore_DoesNotPanic(t *testing.T) {
	t.Parallel()

	engine := orchestrator.NewConversationEngine(nil, nil, nil, nil)
	// Do NOT set a project fact store.

	belief := memory.Belief{
		Content:       "the auth module is critical",
		Confidence:    0.9,
		Confirmations: 5,
	}

	assert.NotPanics(t, func() {
		engine.PropagateIfSignificantForTest(context.Background(), agent.RoleEngineer1, belief)
	})
}

func TestPropagateIfSignificant_MultipleConfirmations_Propagates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	projectDB, projErr := memory.Open(ctx, ":memory:")
	require.NoError(t, projErr)
	t.Cleanup(func() { _ = projectDB.Close() })

	projectFactStore := memory.NewFactStore(projectDB)
	engine := orchestrator.NewConversationEngine(nil, nil, nil, nil)
	engine.SetProjectFactStore(projectFactStore)

	// Low confidence but many confirmations (>= 2) + project signal => propagate.
	belief := memory.Belief{
		Content:       "the test pattern for handlers is consistent",
		Confidence:    0.4,
		Confirmations: 3,
	}

	engine.PropagateIfSignificantForTest(ctx, agent.RoleEngineer1, belief)

	beliefs, beliefErr := projectFactStore.TopBeliefs(ctx, 10)
	require.NoError(t, beliefErr)
	assert.Len(t, beliefs, 1)
}
