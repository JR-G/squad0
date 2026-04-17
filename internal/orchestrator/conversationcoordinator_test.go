package orchestrator_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestConversationCoordinator_NilCoordinator_NameForRole_ReturnsRoleSlug(t *testing.T) {
	t.Parallel()

	var coord *orchestrator.ConversationCoordinator
	assert.Equal(t, "engineer-1", coord.NameForRole(agent.RoleEngineer1))
}

func TestConversationCoordinator_NoRoster_NameForRole_ReturnsRoleSlug(t *testing.T) {
	t.Parallel()

	coord := orchestrator.NewConversationCoordinator(nil)
	assert.Equal(t, "engineer-1", coord.NameForRole(agent.RoleEngineer1))
}

func TestConversationCoordinator_RosterMatchingSlug_ReturnsRoleSlug(t *testing.T) {
	t.Parallel()

	coord := orchestrator.NewConversationCoordinator(nil)
	coord.SetRoster(map[agent.Role]string{agent.RoleEngineer1: "engineer-1"})

	// When the roster value equals the slug, no rename happened —
	// surface the slug rather than the redundant entry.
	assert.Equal(t, "engineer-1", coord.NameForRole(agent.RoleEngineer1))
}

func TestConversationCoordinator_NamedRole_ReturnsName(t *testing.T) {
	t.Parallel()

	coord := orchestrator.NewConversationCoordinator(nil)
	coord.SetRoster(map[agent.Role]string{agent.RoleEngineer1: "Mara"})

	assert.Equal(t, "Mara", coord.NameForRole(agent.RoleEngineer1))
}

func TestConversationCoordinator_UnknownRole_ReturnsRoleSlug(t *testing.T) {
	t.Parallel()

	coord := orchestrator.NewConversationCoordinator(nil)
	coord.SetRoster(map[agent.Role]string{agent.RoleEngineer1: "Mara"})

	assert.Equal(t, "designer", coord.NameForRole(agent.RoleDesigner))
}

func TestConversationCoordinator_NilBot_PostIsNoop(t *testing.T) {
	t.Parallel()

	coord := orchestrator.NewConversationCoordinator(nil)

	assert.NotPanics(t, func() {
		coord.Post(context.Background(), "engineering", "hi", agent.RoleEngineer1)
		coord.Announce(context.Background(), "feed", "hello", agent.RoleEngineer1)
	})
}

func TestConversationCoordinator_NilCoordinator_AllMethodsAreNoop(t *testing.T) {
	t.Parallel()

	var coord *orchestrator.ConversationCoordinator

	assert.NotPanics(t, func() {
		coord.Post(context.Background(), "engineering", "hi", agent.RoleEngineer1)
		coord.Announce(context.Background(), "feed", "hello", agent.RoleEngineer1)
	})
}
