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

func TestBuildSeanceContext_NoPriorWork_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := memory.NewEpisodeStore(db)

	result := orchestrator.BuildSeanceContext(ctx, store, "JAM-42", agent.RoleEngineer1)

	assert.Empty(t, result)
}

func TestBuildSeanceContext_NilStore_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	result := orchestrator.BuildSeanceContext(context.Background(), nil, "JAM-42", agent.RoleEngineer1)

	assert.Empty(t, result)
}

func TestBuildSeanceContext_PriorWork_IncludesContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := memory.NewEpisodeStore(db)

	// Engineer-2 previously worked on this ticket.
	_, err = store.CreateEpisode(ctx, memory.Episode{
		Agent:   string(agent.RoleEngineer2),
		Ticket:  "JAM-42",
		Summary: "Found that the auth module needs retry logic on token refresh",
		Outcome: memory.OutcomePartial,
	})
	require.NoError(t, err)

	// Engineer-1 is now picking it up.
	result := orchestrator.BuildSeanceContext(ctx, store, "JAM-42", agent.RoleEngineer1)

	assert.Contains(t, result, "Previous Work")
	assert.Contains(t, result, "engineer-2")
	assert.Contains(t, result, "retry logic")
}

func TestBuildSeanceContext_OwnWork_Excluded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := memory.NewEpisodeStore(db)

	// Engineer-1's own prior episode should be excluded.
	_, err = store.CreateEpisode(ctx, memory.Episode{
		Agent:   string(agent.RoleEngineer1),
		Ticket:  "JAM-42",
		Summary: "My own work",
		Outcome: memory.OutcomeFailure,
	})
	require.NoError(t, err)

	result := orchestrator.BuildSeanceContext(ctx, store, "JAM-42", agent.RoleEngineer1)

	assert.Empty(t, result)
}

func TestEpisodesByTicket_ReturnsMatchingEpisodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := memory.NewEpisodeStore(db)

	_, _ = store.CreateEpisode(ctx, memory.Episode{Agent: "eng-1", Ticket: "JAM-42", Summary: "first", Outcome: "success"})
	_, _ = store.CreateEpisode(ctx, memory.Episode{Agent: "eng-2", Ticket: "JAM-42", Summary: "second", Outcome: "failure"})
	_, _ = store.CreateEpisode(ctx, memory.Episode{Agent: "eng-1", Ticket: "JAM-99", Summary: "other", Outcome: "success"})

	episodes, err := store.EpisodesByTicket(ctx, "JAM-42")

	require.NoError(t, err)
	assert.Len(t, episodes, 2)
}

func TestClassifyReviewOutcome_Approved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		transcript string
		expected   orchestrator.ReviewOutcome
	}{
		{"explicit approved", "APPROVED", orchestrator.ReviewApproved},
		{"LGTM", "Looks good to me. LGTM.", orchestrator.ReviewApproved},
		{"changes requested", "CHANGES_REQUESTED\nPlease fix the nil check", orchestrator.ReviewChangesRequested},
		{"please fix", "PLEASE FIX the auth handler", orchestrator.ReviewChangesRequested},
		{"unclear defaults to approved", "I looked at the code.", orchestrator.ReviewApproved},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := orchestrator.ClassifyReviewOutcome(tt.transcript)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildFixUpPrompt_ContainsPRInfo(t *testing.T) {
	t.Parallel()

	prompt := orchestrator.BuildFixUpPrompt(
		"https://github.com/test-org/test-repo/pull/42",
		"JAM-17",
	)

	assert.Contains(t, prompt, "JAM-17")
	assert.Contains(t, prompt, "gh pr view 42 --comments")
	assert.Contains(t, prompt, "gh pr diff 42")
}
