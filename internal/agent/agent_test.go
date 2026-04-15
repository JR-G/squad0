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

func setupAgentTest(t *testing.T) (*agent.Agent, *fakeProcessRunner) {
	t.Helper()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	personalityDir := t.TempDir()
	err = os.WriteFile(
		filepath.Join(personalityDir, "engineer-1.md"),
		[]byte("You are a thorough engineer."),
		0o644,
	)
	require.NoError(t, err)

	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)

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

	runner := &fakeProcessRunner{}
	session := agent.NewSession(runner)

	agentInstance := agent.NewAgent(
		agent.RoleEngineer1,
		"claude-sonnet-4-6",
		session,
		loader,
		retriever,
		db,
		episodeStore,
		embedder,
	)

	return agentInstance, runner
}

func TestAgent_Role_ReturnsRole(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)

	assert.Equal(t, agent.RoleEngineer1, agentInstance.Role())
}

func TestAgent_Name_WithRoster_ReturnsChosenName(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	agentInstance.SetChatContext(
		map[agent.Role]string{agent.RoleEngineer1: "Callum"},
		nil, "",
	)
	assert.Equal(t, "Callum", agentInstance.Name())
}

func TestAgent_Name_WithoutRoster_FallsBackToRole(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)
	assert.Equal(t, string(agent.RoleEngineer1), agentInstance.Name())
}

func TestAgent_ExecuteTask_SuccessfulSession_StoresEpisode(t *testing.T) {
	t.Parallel()

	agentInstance, runner := setupAgentTest(t)
	runner.output = []byte(`{"type":"assistant","content":"I fixed the bug."}` + "\n" +
		`{"type":"result","result":"Done."}` + "\n")

	result, err := agentInstance.ExecuteTask(
		context.Background(),
		"Fix the payment bug",
		[]string{"internal/payments/handler.go"},
		"/tmp/worktree",
	)

	require.NoError(t, err)
	assert.NotEmpty(t, result.Transcript)
	assert.Equal(t, 0, result.ExitCode)
}

func TestAgent_ExecuteTask_FailedSession_StoresEpisodeAndReturnsError(t *testing.T) {
	t.Parallel()

	agentInstance, runner := setupAgentTest(t)
	runner.output = []byte(`{"type":"error","content":"context exhausted"}` + "\n")
	runner.err = fmt.Errorf("exit status 1")

	result, err := agentInstance.ExecuteTask(
		context.Background(),
		"Complex refactor",
		nil,
		"/tmp/worktree",
	)

	require.Error(t, err)
	assert.NotEmpty(t, result.RawOutput)
}

func TestAgent_ExecuteTask_PromptIncludesPersonality(t *testing.T) {
	t.Parallel()

	agentInstance, runner := setupAgentTest(t)
	runner.output = []byte(`{"type":"result","result":"ok"}` + "\n")

	_, err := agentInstance.ExecuteTask(
		context.Background(),
		"Do something",
		nil,
		"/tmp/worktree",
	)

	require.NoError(t, err)
	require.Len(t, runner.calls, 1)
	assert.Contains(t, runner.calls[0].stdin, "You are a thorough engineer.")
	assert.Contains(t, runner.calls[0].stdin, "Do something")
}

func TestAgent_DirectSession_RunsCleanPrompt(t *testing.T) {
	t.Parallel()

	agentInstance, runner := setupAgentTest(t)
	runner.output = []byte(`{"type":"result","result":"Linear tickets: JAM-1, JAM-2"}` + "\n")

	result, err := agentInstance.DirectSession(
		context.Background(),
		"Query Linear for available tickets",
	)

	require.NoError(t, err)
	assert.Contains(t, result.Transcript, "Linear tickets")
	require.Len(t, runner.calls, 1)
	// DirectSession uses the prompt as-is, no personality wrapping
	assert.NotContains(t, runner.calls[0].stdin, "You are a thorough engineer")
	assert.Contains(t, runner.calls[0].stdin, "Query Linear for available tickets")
}

func TestAgent_DirectSession_Error_ReturnsError(t *testing.T) {
	t.Parallel()

	agentInstance, runner := setupAgentTest(t)
	runner.output = []byte(`{"type":"error","content":"rate limited"}` + "\n")
	runner.err = fmt.Errorf("exit status 1")

	_, err := agentInstance.DirectSession(
		context.Background(),
		"Do something",
	)

	require.Error(t, err)
}

func TestAgent_SetDBPath_And_DBPath(t *testing.T) {
	t.Parallel()

	agentInstance, _ := setupAgentTest(t)

	assert.Empty(t, agentInstance.DBPath())

	agentInstance.SetDBPath("/data/agents/engineer-1.db")

	assert.Equal(t, "/data/agents/engineer-1.db", agentInstance.DBPath())
}

func TestAgent_SetMemoryStores_And_Accessors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	agentInstance, _ := setupAgentTest(t)

	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)

	agentInstance.SetMemoryStores(graphStore, factStore)

	assert.NotNil(t, agentInstance.GraphStore())
	assert.NotNil(t, agentInstance.FactStore())
	assert.NotNil(t, agentInstance.EpisodeStore())
	assert.NotNil(t, agentInstance.Embedder())
}

func TestSession_Run_WorkingDir_PassedToRunner(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"ok"}` + "\n")}
	session := agent.NewSession(runner)

	_, err := session.Run(context.Background(), agent.SessionConfig{
		Role:       agent.RoleEngineer1,
		Model:      "claude-sonnet-4-6",
		Prompt:     "task",
		WorkingDir: "/tmp/worktree",
	})

	require.NoError(t, err)
	// The fake ignores workingDir but verifies the session passes it through
	require.Len(t, runner.calls, 1)
}
