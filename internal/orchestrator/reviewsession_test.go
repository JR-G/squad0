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

func TestExtractPRURL_FindsURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "standard PR URL",
			text:     "Opened PR at https://github.com/test-org/test-repo/pull/42",
			expected: "https://github.com/test-org/test-repo/pull/42",
		},
		{
			name:     "URL in middle of text",
			text:     "Created https://github.com/test-org/test-repo/pull/7 for review",
			expected: "https://github.com/test-org/test-repo/pull/7",
		},
		{
			name:     "no PR URL",
			text:     "No pull request was created.",
			expected: "",
		},
		{
			name:     "empty text",
			text:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := orchestrator.ExtractPRURL(tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPRNumber_ReturnsNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		prURL    string
		expected string
	}{
		{
			name:     "standard URL",
			prURL:    "https://github.com/test-org/test-repo/pull/42",
			expected: "42",
		},
		{
			name:     "single digit",
			prURL:    "https://github.com/test-org/test-repo/pull/7",
			expected: "7",
		},
		{
			name:     "no slash",
			prURL:    "noslash",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := orchestrator.ExtractPRNumber(tt.prURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStartReview_AssignsReviewer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"LGTM, code looks clean."}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmAgent := setupPMAgent(t, pmRunner)
	reviewerAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, db)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:       pmAgent,
		agent.RoleReviewer: reviewerAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	// Start a review — this spawns a goroutine.
	orch.StartReviewForTest(ctx, "https://github.com/test-org/test-repo/pull/42", "JAM-7")
	orch.Wait()

	// Reviewer should have been called with the PR number.
	require.NotEmpty(t, reviewRunner.calls)
	assert.Contains(t, reviewRunner.calls[0].stdin, "gh pr diff 42")
}

func TestStartReview_NoReviewer_DoesNotPanic(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	// No reviewer agent in the map.
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assert.NotPanics(t, func() {
		orch.StartReviewForTest(context.Background(), "https://github.com/test-org/test-repo/pull/1", "T-1")
	})
}

func TestStartReview_ReviewerError_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{
			agent.RolePM:       pmAgent,
			agent.RoleReviewer: buildAgent(t, reviewRunner, agent.RoleReviewer, db),
		},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	orch.StartReviewForTest(ctx, "https://github.com/test-org/test-repo/pull/1", "T-1")
	orch.Wait()

	// Reviewer should be back to idle after failure.
	checkIn, getErr := checkIns.GetByAgent(ctx, agent.RoleReviewer)
	require.NoError(t, getErr)
	assert.Equal(t, coordination.StatusIdle, checkIn.Status)
}

func TestBuildReviewPrompt_ContainsPRNumber(t *testing.T) {
	t.Parallel()

	prompt := orchestrator.BuildReviewPrompt(
		"https://github.com/test-org/test-repo/pull/42",
		"JAM-7",
	)

	assert.Contains(t, prompt, "JAM-7")
	assert.Contains(t, prompt, "gh pr diff 42")
	assert.Contains(t, prompt, "gh pr view 42")
	assert.Contains(t, prompt, "gh pr review 42")
	assert.Contains(t, prompt, "gh pr comment 42")
}

func TestBuildReviewPrompt_ContainsPRCommentStep(t *testing.T) {
	t.Parallel()

	prompt := orchestrator.BuildReviewPrompt(
		"https://github.com/test-org/test-repo/pull/10",
		"JAM-1",
	)

	assert.Contains(t, prompt, "gh pr comment 10 --body")
	assert.Contains(t, prompt, "numbered items for each issue")
	assert.Contains(t, prompt, "SHORT summary only")
}

func TestBuildReviewPrompt_VerifyRetryInstruction(t *testing.T) {
	t.Parallel()

	prompt := orchestrator.BuildReviewPrompt(
		"https://github.com/test-org/test-repo/pull/5",
		"JAM-2",
	)

	assert.Contains(t, prompt, "reviewDecision is still empty after your review, try again")
}
