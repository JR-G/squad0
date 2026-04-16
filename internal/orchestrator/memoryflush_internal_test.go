package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type retryRunner struct {
	mu    sync.Mutex
	calls int
	err   error
	out   []byte
}

func (runner *retryRunner) Run(_ context.Context, _ /* stdin */, _ /* workingDir */, _ /* name */ string, _ ...string) ([]byte, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.calls++
	return runner.out, runner.err
}

func TestParseExtractedLearnings_NoJSONObject_ReturnsNil(t *testing.T) {
	t.Parallel()

	got := parseExtractedLearnings("nothing useful in this transcript")

	assert.Nil(t, got)
}

func TestParseExtractedLearnings_InvalidJSON_ReturnsNil(t *testing.T) {
	t.Parallel()

	got := parseExtractedLearnings("noise {not actually json} more noise")

	assert.Nil(t, got)
}

func TestParseExtractedLearnings_ValidJSON_ReturnsLearnings(t *testing.T) {
	t.Parallel()

	transcript := `here you go: {"facts": [], "beliefs": [], "entities": []} done`

	got := parseExtractedLearnings(transcript)

	require.NotNil(t, got)
	assert.Empty(t, got.Facts)
	assert.Empty(t, got.Beliefs)
	assert.Empty(t, got.Entities)
}

func TestIsRetryableExtractionError_Nil_ReturnsFalse(t *testing.T) {
	t.Parallel()

	assert.False(t, isRetryableExtractionError(nil))
}

func TestIsRetryableExtractionError_ContextCancelled_ReturnsFalse(t *testing.T) {
	t.Parallel()

	assert.False(t, isRetryableExtractionError(context.Canceled))
	assert.False(t, isRetryableExtractionError(context.DeadlineExceeded))
}

func TestIsRetryableExtractionError_OtherError_ReturnsTrue(t *testing.T) {
	t.Parallel()

	assert.True(t, isRetryableExtractionError(errors.New("rate limit")))
	assert.True(t, isRetryableExtractionError(errors.New("connection refused")))
}

func buildExtractorAgent(t *testing.T, runner *retryRunner) *agent.Agent {
	t.Helper()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	personalityDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(personalityDir, "pm.md"), []byte("you are pm"), 0o644))

	embedderServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string][]float32{"embedding": {0.1, 0.2}})
	}))
	t.Cleanup(embedderServer.Close)

	embedder := memory.NewEmbedder(embedderServer.URL, "test")
	episodeStore := memory.NewEpisodeStore(db)
	loader := agent.NewPersonalityLoader(personalityDir)
	session := agent.NewSession(runner)

	return agent.NewAgent(agent.RolePM, "test-model", session, loader, nil, db, episodeStore, embedder)
}

func TestExtractLearnings_AllAttemptsFail_ReturnsNil(t *testing.T) {
	t.Parallel()

	original := extractionRetryDelay
	extractionRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { extractionRetryDelay = original })

	runner := &retryRunner{err: errors.New("rate limited")}
	extractor := buildExtractorAgent(t, runner)

	got := extractLearnings(context.Background(), extractor, "JAM-1", "transcript")

	assert.Nil(t, got)
	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	assert.Equal(t, extractionMaxAttempts, calls, "should attempt all retries before giving up")
}

func TestExtractLearnings_CancelledContext_ReturnsNilImmediately(t *testing.T) {
	t.Parallel()

	runner := &retryRunner{err: errors.New("transient")}
	extractor := buildExtractorAgent(t, runner)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := extractLearnings(ctx, extractor, "JAM-1", "transcript")

	assert.Nil(t, got)
}
