package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainsFindings_WithDiscovered(t *testing.T) {
	t.Parallel()

	assert.True(t, orchestrator.ContainsFindings("I discovered that the auth module has a race condition"))
}

func TestContainsFindings_WithFound(t *testing.T) {
	t.Parallel()

	assert.True(t, orchestrator.ContainsFindings("Found an issue with the database connection pool"))
}

func TestContainsFindings_WithUnexpected(t *testing.T) {
	t.Parallel()

	assert.True(t, orchestrator.ContainsFindings("There was an unexpected dependency on the logging module"))
}

func TestContainsFindings_WithGotcha(t *testing.T) {
	t.Parallel()

	assert.True(t, orchestrator.ContainsFindings("The gotcha here is that Stripe retries silently"))
}

func TestContainsFindings_WithWarning(t *testing.T) {
	t.Parallel()

	assert.True(t, orchestrator.ContainsFindings("Warning: the config file is not validated on startup"))
}

func TestContainsFindings_WithBlocker(t *testing.T) {
	t.Parallel()

	assert.True(t, orchestrator.ContainsFindings("This is a blocker for the release"))
}

func TestContainsFindings_NoKeywords_ReturnsFalse(t *testing.T) {
	t.Parallel()

	assert.False(t, orchestrator.ContainsFindings("Implemented the feature, all tests pass, opened PR"))
}

func TestContainsFindings_CaseInsensitive(t *testing.T) {
	t.Parallel()

	assert.True(t, orchestrator.ContainsFindings("DISCOVERED a major bug"))
	assert.True(t, orchestrator.ContainsFindings("WARNING: deprecated API"))
}

func TestExtractFindings_PullsRelevantSentences(t *testing.T) {
	t.Parallel()

	transcript := "Implemented the auth module. Discovered that the token refresh has a race condition. All tests pass. Found a gotcha with the session timeout."

	findings := orchestrator.ExtractFindings(transcript)

	assert.Contains(t, findings, "Discovered that the token refresh has a race condition")
	assert.Contains(t, findings, "gotcha with the session timeout")
	assert.NotContains(t, findings, "All tests pass")
}

func TestExtractFindings_NoKeywords_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	transcript := "Implemented the feature and opened a PR"

	findings := orchestrator.ExtractFindings(transcript)

	assert.Empty(t, findings)
}

func TestExtractFindings_TruncatesLongOutput(t *testing.T) {
	t.Parallel()

	// Build a very long transcript with findings keywords scattered throughout.
	long := ""
	for range 50 {
		long += "I discovered yet another issue with the error handling in this module. "
	}

	findings := orchestrator.ExtractFindings(long)

	assert.LessOrEqual(t, len(findings), 500)
}

func TestExtractFindings_NewlineSentences(t *testing.T) {
	t.Parallel()

	transcript := "First line of work\nDiscovered a bug in the parser\nContinued implementation"

	findings := orchestrator.ExtractFindings(transcript)

	assert.Contains(t, findings, "Discovered a bug in the parser")
}

func TestPersistFindings_NoKeywords_NoPMCall(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmAgent := setupPMAgent(t, pmRunner)
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	// Transcript has no findings keywords — PM should NOT be called.
	orch.PersistFindings(ctx, "JAM-1", "Implemented the feature and opened a PR")

	pmRunner.mu.Lock()
	callCount := len(pmRunner.calls)
	pmRunner.mu.Unlock()
	assert.Equal(t, 0, callCount)
}

func TestPersistFindings_WithKeywords_CallsPM(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmAgent := setupPMAgent(t, pmRunner)
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	// Transcript has findings keywords — PM should be called.
	orch.PersistFindings(ctx, "JAM-42", "Discovered a race condition in the auth module. Warning: the retry logic is fragile.")

	pmRunner.mu.Lock()
	callCount := len(pmRunner.calls)
	pmRunner.mu.Unlock()
	assert.Equal(t, 1, callCount)

	// Verify the prompt mentions the ticket and findings.
	pmRunner.mu.Lock()
	stdin := pmRunner.calls[0].stdin
	pmRunner.mu.Unlock()
	assert.Contains(t, stdin, "JAM-42")
	assert.Contains(t, stdin, "save_comment")
}

func TestPersistFindings_NoPMAgent_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	assert.NotPanics(t, func() {
		orch.PersistFindings(ctx, "JAM-1", "Found a critical bug in the parser")
	})
}
