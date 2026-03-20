package agent_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dbBreakingRunner drops the episodes_fts table after the process runs,
// so that the session succeeds but episode storage fails.
type dbBreakingRunner struct {
	fakeProcessRunner
	rawDB *sql.DB
}

func (runner *dbBreakingRunner) Run(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	output, err := runner.fakeProcessRunner.Run(ctx, stdin, name, args...)

	_, _ = runner.rawDB.Exec(`DROP TABLE episodes_fts`)

	return output, err
}

func TestAgent_ExecuteTask_StoreEpisodeError_WhenSessionSucceeds_ReturnsError(t *testing.T) {
	t.Parallel()

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

	// Use a runner that drops the episodes_fts table after it runs,
	// so that prompt assembly succeeds but episode storage fails.
	breakingRunner := &dbBreakingRunner{
		fakeProcessRunner: fakeProcessRunner{
			output: []byte(`{"type":"result","content":"done"}` + "\n"),
		},
		rawDB: db.RawDB(),
	}
	session := agent.NewSession(breakingRunner)

	agentInstance := agent.NewAgent(
		agent.RoleEngineer1, "claude-sonnet-4-6", session, loader,
		retriever, db, episodeStore, embedder,
	)

	_, err = agentInstance.ExecuteTask(ctx, "test task", nil, "/tmp/worktree")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing episode")
}

func TestAgent_ExecuteTask_PersonalityLoadError_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	loader := agent.NewPersonalityLoader("/nonexistent/dir")

	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	embedder := memory.NewEmbedder("http://localhost:1", "test-model")
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)

	runner := &fakeProcessRunner{output: []byte("{}\n")}
	session := agent.NewSession(runner)

	agentInstance := agent.NewAgent(
		agent.RoleEngineer1, "claude-sonnet-4-6", session, loader,
		retriever, db, episodeStore, embedder,
	)

	_, err = agentInstance.ExecuteTask(ctx, "task", nil, "/tmp")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "assembling prompt")
}

func TestAgent_ExecuteTask_MemoryRetrievalError_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)

	personalityDir := t.TempDir()
	err = os.WriteFile(
		filepath.Join(personalityDir, "engineer-1.md"),
		[]byte("You are an engineer."),
		0o644,
	)
	require.NoError(t, err)

	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)

	failEmbedder := memory.NewEmbedder("http://localhost:1", "fail-model")
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, failEmbedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)
	loader := agent.NewPersonalityLoader(personalityDir)

	require.NoError(t, db.Close())

	runner := &fakeProcessRunner{output: []byte("{}\n")}
	session := agent.NewSession(runner)

	agentInstance := agent.NewAgent(
		agent.RoleEngineer1, "claude-sonnet-4-6", session, loader,
		retriever, db, episodeStore, failEmbedder,
	)

	_, err = agentInstance.ExecuteTask(ctx, "task", nil, "/tmp")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "assembling prompt")
}

func TestRetrieveMemoryContext_Error_WrapsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)

	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)

	failEmbedder := memory.NewEmbedder("http://localhost:1", "fail-model")
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, failEmbedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)

	require.NoError(t, db.Close())

	_, err = agent.RetrieveMemoryContext(ctx, retriever, "task", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "retrieving memory context")
}

func TestWriteFactsSection_WithFacts_WritesSection(t *testing.T) {
	t.Parallel()

	memCtx := memory.RetrievalContext{
		Facts: []memory.Fact{
			{Content: "fact one", Type: memory.FactWarning, Confidence: 0.9},
			{Content: "fact two", Type: memory.FactObservation, Confidence: 0.5},
		},
	}

	result := agent.AssemblePrompt("personality", memCtx, "task")

	assert.Contains(t, result, "## Known Facts")
	assert.Contains(t, result, "fact one")
	assert.Contains(t, result, "fact two")
}

func TestExecProcessRunner_Run_SuccessfulCommand_ReturnsOutput(t *testing.T) {
	t.Parallel()

	runner := agent.ExecProcessRunner{}
	output, err := runner.Run(context.Background(), "", "echo", "hello")

	require.NoError(t, err)
	assert.Contains(t, string(output), "hello")
}

func TestExecProcessRunner_Run_FailingCommand_ReturnsOutputAndError(t *testing.T) {
	t.Parallel()

	runner := agent.ExecProcessRunner{}
	output, err := runner.Run(context.Background(), "", "sh", "-c", "echo fail && exit 1")

	require.Error(t, err)
	assert.Contains(t, string(output), "fail")
	assert.Contains(t, err.Error(), "running sh")
}

func TestExtractExitError_ExitError_ReturnsExitCode(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sh", "-c", "exit 42")
	err := cmd.Run()

	code := agent.ExtractExitError(err)

	assert.Equal(t, 42, code)
}

func TestParseStreamOutput_BlankLines_Ignored(t *testing.T) {
	t.Parallel()

	output := "\n\n" +
		`{"type":"result","content":"ok"}` + "\n" +
		"   \n"
	runner := &fakeProcessRunner{output: []byte(output)}
	session := agent.NewSession(runner)

	result, err := session.Run(context.Background(), agent.SessionConfig{
		Role: agent.RoleEngineer1, Model: "claude-sonnet-4-6", Prompt: "task",
	})

	require.NoError(t, err)
	assert.Len(t, result.Messages, 1)
}

func TestAgent_ExecuteTask_SessionError_StillStoresEpisode(t *testing.T) {
	t.Parallel()

	agentInstance, runner := setupAgentTest(t)
	runner.output = []byte(`{"type":"result","content":"partial work"}` + "\n")
	runner.err = fmt.Errorf("session crashed")

	result, err := agentInstance.ExecuteTask(
		context.Background(),
		"Risky refactor",
		nil,
		"/tmp/worktree",
	)

	require.Error(t, err)
	assert.NotEmpty(t, result.RawOutput)
}
