package orchestrator_test

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
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProcessRunner struct {
	output []byte
	err    error
	calls  []fakeCall
}

type fakeCall struct {
	stdin string
	name  string
	args  []string
}

func (runner *fakeProcessRunner) Run(_ context.Context, stdin, name string, args ...string) ([]byte, error) {
	runner.calls = append(runner.calls, fakeCall{stdin: stdin, name: name, args: args})
	return runner.output, runner.err
}

func setupPMAgent(t *testing.T, runner *fakeProcessRunner) *agent.Agent {
	t.Helper()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	personalityDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(personalityDir, "pm.md"), []byte("You are the PM."), 0o644))

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
	session := agent.NewSession(runner)

	return agent.NewAgent(agent.RolePM, "claude-haiku-4-5-20251001", session, loader, retriever, db, episodeStore, embedder)
}

func TestAssigner_RequestAssignments_ParsesValidJSON(t *testing.T) {
	t.Parallel()

	assignmentJSON := `[{"role":"engineer-1","ticket":"SQ-42","description":"Fix auth bug"},{"role":"engineer-2","ticket":"SQ-43","description":"Add pagination"}]`
	contentBytes, _ := json.Marshal(assignmentJSON)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	runner := &fakeProcessRunner{output: []byte(pmOutput)}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	result, err := assigner.RequestAssignments(context.Background(), []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2})

	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, agent.RoleEngineer1, result[0].Role)
	assert.Equal(t, "SQ-42", result[0].Ticket)
	assert.Equal(t, agent.RoleEngineer2, result[1].Role)
}

func TestAssigner_RequestAssignments_EmptyArray_ReturnsNil(t *testing.T) {
	t.Parallel()

	pmOutput := `{"type":"result","result":"[]"}` + "\n"
	runner := &fakeProcessRunner{output: []byte(pmOutput)}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	result, err := assigner.RequestAssignments(context.Background(), []agent.Role{agent.RoleEngineer1})

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestAssigner_RequestAssignments_FiltersInvalidRoles(t *testing.T) {
	t.Parallel()

	assignmentJSON := `[{"role":"engineer-1","ticket":"SQ-42","description":"Valid"},{"role":"engineer-99","ticket":"SQ-43","description":"Invalid role"}]`
	contentBytes, _ := json.Marshal(assignmentJSON)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	runner := &fakeProcessRunner{output: []byte(pmOutput)}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	result, err := assigner.RequestAssignments(context.Background(), []agent.Role{agent.RoleEngineer1})

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, agent.RoleEngineer1, result[0].Role)
}

func TestAssigner_RequestAssignments_NoJSON_ReturnsNil(t *testing.T) {
	t.Parallel()

	pmOutput := `{"type":"result","result":"No tickets ready right now."}` + "\n"
	runner := &fakeProcessRunner{output: []byte(pmOutput)}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	result, err := assigner.RequestAssignments(context.Background(), []agent.Role{agent.RoleEngineer1})

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestAssigner_RequestAssignments_PMError_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"failed"}` + "\n"),
		err:    fmt.Errorf("exit status 1"),
	}
	pmAgent := setupPMAgent(t, runner)
	assigner := orchestrator.NewAssigner(pmAgent)

	_, err := assigner.RequestAssignments(context.Background(), []agent.Role{agent.RoleEngineer1})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PM assignment session failed")
}
