package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestMemDB(t *testing.T) *memory.DB {
	t.Helper()
	db, err := memory.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func buildTestAgent(t *testing.T, runner *fakeProcessRunner, role agent.Role, personalityDir string, memDB *memory.DB) *agent.Agent {
	t.Helper()

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

func TestQuickChat_ReturnsTranscript(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"Sounds good to me."}` + "\n")}
	agentInstance, _ := setupAgentTest(t)
	_ = agentInstance

	personalityDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(personalityDir, "engineer-1.md"), []byte("You are thorough."), 0o644))

	db := openTestMemDB(t)
	builtAgent := buildTestAgent(t, runner, agent.RoleEngineer1, personalityDir, db)

	result, err := builtAgent.QuickChat(context.Background(), "What do you think?")

	require.NoError(t, err)
	assert.Contains(t, result, "Sounds good")
}

func TestQuickChat_MissingPersonality_StillWorks(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"Sure."}` + "\n")}

	emptyDir := t.TempDir()
	db := openTestMemDB(t)
	builtAgent := buildTestAgent(t, runner, agent.RoleEngineer1, emptyDir, db)

	result, err := builtAgent.QuickChat(context.Background(), "Hello")

	require.NoError(t, err)
	assert.Contains(t, result, "Sure")
}

func TestQuickChat_SessionError_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","result":"failed"}` + "\n"),
		err:    fmt.Errorf("exit status 1"),
	}

	personalityDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(personalityDir, "engineer-1.md"), []byte("You are thorough."), 0o644))

	db := openTestMemDB(t)
	builtAgent := buildTestAgent(t, runner, agent.RoleEngineer1, personalityDir, db)

	_, err := builtAgent.QuickChat(context.Background(), "test")

	require.Error(t, err)
}

type fakeBridge struct {
	response string
	err      error
	calls    int
}

func (b *fakeBridge) Chat(_ context.Context, _ string) (string, error) {
	b.calls++
	return b.response, b.err
}

func TestQuickChat_WithBridge_RoutesThrough(t *testing.T) {
	t.Parallel()

	bridge := &fakeBridge{response: "bridge response"}
	a := agent.NewAgent(agent.RoleEngineer1, "test", nil, nil, nil, nil, nil, nil)
	a.SetBridge(bridge)

	result, err := a.QuickChat(context.Background(), "test prompt")
	require.NoError(t, err)
	assert.Equal(t, "bridge response", result)
	assert.Equal(t, 1, bridge.calls)
}

func TestQuickChat_WithBridge_Error_ReturnsError(t *testing.T) {
	t.Parallel()

	bridge := &fakeBridge{err: fmt.Errorf("bridge error")}
	a := agent.NewAgent(agent.RoleEngineer1, "test", nil, nil, nil, nil, nil, nil)
	a.SetBridge(bridge)

	_, err := a.QuickChat(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bridge error")
}
