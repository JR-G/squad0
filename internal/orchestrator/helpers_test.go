package orchestrator_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/require"
)

func newTestSlackBot(serverURL string) *islack.Bot {
	return islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"engineering": "C001",
			"feed":        "C002",
			"commands":    "C003",
		},
		Personas: map[agent.Role]islack.Persona{},
	}, serverURL+"/")
}

func openMemoryDB(ctx context.Context) (*memory.DB, error) {
	return memory.Open(ctx, ":memory:")
}

func buildAgent(t *testing.T, runner *fakeProcessRunner, role agent.Role, memDB *memory.DB) *agent.Agent {
	t.Helper()

	personalityDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(personalityDir, role.PersonalityFile()),
		[]byte("You are "+string(role)+"."),
		0o644,
	))

	graphStore := memory.NewGraphStore(memDB)
	factStore := memory.NewFactStore(memDB)
	episodeStore := memory.NewEpisodeStore(memDB)
	ftsStore := memory.NewFTSStore(memDB)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		resp := map[string][]float32{"embedding": {0.1, 0.2, 0.3}}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	t.Cleanup(server.Close)

	embedder := memory.NewEmbedder(server.URL, "test-model")
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	loader := agent.NewPersonalityLoader(personalityDir)
	session := agent.NewSession(runner)

	return agent.NewAgent(role, "test-model", session, loader, retriever, memDB, episodeStore, embedder)
}
