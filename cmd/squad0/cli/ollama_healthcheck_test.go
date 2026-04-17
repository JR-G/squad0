package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckOllamaHealth_ReachableAndHealthy_ReportsDone(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string][]float32{"embedding": {0.1, 0.2}})
	}))
	t.Cleanup(server.Close)

	var out bytes.Buffer
	checkOllamaHealth(context.Background(), server.URL, "test-model", &out)

	assert.Contains(t, out.String(), "Ollama embedder ready")
	assert.Contains(t, out.String(), "test-model")
}

func TestCheckOllamaHealth_Unreachable_ReportsWarn(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	checkOllamaHealth(context.Background(), "http://127.0.0.1:1", "nomic-embed-text", &out)

	assert.Contains(t, out.String(), "unreachable")
	assert.Contains(t, out.String(), "nomic-embed-text")
}

func TestCheckOllamaHealth_EmptyConfig_ReportsDisabled(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	checkOllamaHealth(context.Background(), "", "", &out)

	assert.Contains(t, out.String(), "not configured")
}

func TestCheckOllamaHealth_EmptyEmbedding_ReportsWarn(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string][]float32{"embedding": {}})
	}))
	t.Cleanup(server.Close)

	var out bytes.Buffer
	checkOllamaHealth(context.Background(), server.URL, "test-model", &out)

	assert.Contains(t, out.String(), "unreachable")
}
