package coordination_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckFileConflicts_NoOverlap_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking,
		FilesTouching: []string{"auth.go"},
	})

	conflicts, err := coordination.CheckFileConflicts(ctx, store, agent.RoleEngineer2, []string{"payments.go"})

	require.NoError(t, err)
	assert.Empty(t, conflicts)
}

func TestCheckFileConflicts_WithOverlap_ReturnsConflict(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking,
		FilesTouching: []string{"handler.go", "middleware.go"},
	})

	conflicts, err := coordination.CheckFileConflicts(ctx, store, agent.RoleEngineer2, []string{"handler.go", "utils.go"})

	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	assert.Equal(t, "handler.go", conflicts[0].File)
	assert.Equal(t, agent.RoleEngineer1, conflicts[0].HeldBy)
	assert.Equal(t, agent.RoleEngineer2, conflicts[0].RequestedBy)
}

func TestCheckFileConflicts_IgnoresIdleAgents(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusIdle,
		FilesTouching: []string{"handler.go"},
	})

	conflicts, err := coordination.CheckFileConflicts(ctx, store, agent.RoleEngineer2, []string{"handler.go"})

	require.NoError(t, err)
	assert.Empty(t, conflicts)
}

func TestCheckFileConflicts_IgnoresSelf(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking,
		FilesTouching: []string{"handler.go"},
	})

	conflicts, err := coordination.CheckFileConflicts(ctx, store, agent.RoleEngineer1, []string{"handler.go"})

	require.NoError(t, err)
	assert.Empty(t, conflicts)
}

func TestCheckFileConflicts_MultipleConflicts(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking,
		FilesTouching: []string{"a.go", "b.go"},
	})
	_ = store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer3, Status: coordination.StatusWorking,
		FilesTouching: []string{"b.go", "c.go"},
	})

	conflicts, err := coordination.CheckFileConflicts(ctx, store, agent.RoleEngineer2, []string{"a.go", "b.go", "c.go"})

	require.NoError(t, err)
	assert.Len(t, conflicts, 4)
}
