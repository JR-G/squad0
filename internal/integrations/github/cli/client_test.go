package cli_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ghcli "github.com/JR-G/squad0/internal/integrations/github/cli"
	"github.com/JR-G/squad0/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	responses map[string][]byte
	err       error
	calls     [][]string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{responses: make(map[string][]byte)}
}

func (r *fakeRunner) On(args string, output []byte) {
	r.responses[args] = output
}

func (r *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, args)
	if r.err != nil {
		return nil, r.err
	}
	output, ok := r.responses[strings.Join(args, " ")]
	if !ok {
		return []byte("{}"), nil
	}
	return output, nil
}

func TestClient_State_ParsesAllFields(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("pr view https://example/pr/1 --json state,reviewDecision,mergeable,headRefName,baseRefName,updatedAt,url",
		[]byte(`{"url":"https://example/pr/1","state":"OPEN","reviewDecision":"APPROVED","mergeable":"MERGEABLE","headRefName":"feat/x","baseRefName":"main","updatedAt":"2026-04-15T18:10:47Z"}`))

	client := ghcli.NewClientWithRunner(runner)
	state, err := client.State(context.Background(), "https://example/pr/1")

	require.NoError(t, err)
	assert.Equal(t, "OPEN", state.State)
	assert.Equal(t, "APPROVED", state.ReviewDecision)
	assert.Equal(t, "MERGEABLE", state.Mergeable)
	assert.Equal(t, "feat/x", state.HeadRefName)
	assert.Equal(t, "main", state.BaseRefName)
	assert.False(t, state.UpdatedAt.IsZero())
}

func TestClient_State_GhFails_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{err: errors.New("gh: not authenticated")}

	_, err := ghcli.NewClientWithRunner(runner).State(context.Background(), "https://example/pr/1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestClient_State_BadJSON_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("pr view https://example/pr/1 --json state,reviewDecision,mergeable,headRefName,baseRefName,updatedAt,url",
		[]byte(`not json`))

	_, err := ghcli.NewClientWithRunner(runner).State(context.Background(), "https://example/pr/1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func TestClient_Reviews_ParsesAuthorAndState(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("pr view https://e/pr/2 --json reviews",
		[]byte(`{"reviews":[{"author":{"login":"alice"},"state":"APPROVED","body":"lgtm","submittedAt":"2026-04-15T18:00:00Z"}]}`))

	reviews, err := ghcli.NewClientWithRunner(runner).Reviews(context.Background(), "https://e/pr/2")

	require.NoError(t, err)
	require.Len(t, reviews, 1)
	assert.Equal(t, "alice", reviews[0].AuthorLogin)
	assert.Equal(t, "APPROVED", reviews[0].State)
	assert.Equal(t, "lgtm", reviews[0].Body)
	assert.False(t, reviews[0].SubmittedAt.IsZero())
}

func TestClient_Reviews_BadJSON_ReturnsError(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("pr view https://e/pr/3 --json reviews", []byte(`x`))

	_, err := ghcli.NewClientWithRunner(runner).Reviews(context.Background(), "https://e/pr/3")

	require.Error(t, err)
}

func TestClient_Comments_ParsesAuthorAndBody(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("pr view https://e/pr/4 --json comments",
		[]byte(`{"comments":[{"author":{"login":"bob"},"body":"interesting","createdAt":"2026-04-15T19:00:00Z"}]}`))

	comments, err := ghcli.NewClientWithRunner(runner).Comments(context.Background(), "https://e/pr/4")

	require.NoError(t, err)
	require.Len(t, comments, 1)
	assert.Equal(t, "bob", comments[0].AuthorLogin)
	assert.Equal(t, "interesting", comments[0].Body)
}

func TestClient_Commits_ParsesShaAndDate(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("pr view https://e/pr/5 --json commits",
		[]byte(`{"commits":[{"oid":"abc123","committedDate":"2026-04-15T18:10:47Z"}]}`))

	commits, err := ghcli.NewClientWithRunner(runner).Commits(context.Background(), "https://e/pr/5")

	require.NoError(t, err)
	require.Len(t, commits, 1)
	assert.Equal(t, "abc123", commits[0].SHA)
	assert.Equal(t, time.Date(2026, 4, 15, 18, 10, 47, 0, time.UTC), commits[0].CommittedDate)
}

func TestClient_List_FiltersAndLimit_PassedToGh(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("pr list --json url,state,title,number --state open --limit 10",
		[]byte(`[{"url":"https://e/pr/1","state":"OPEN","title":"first","number":1}]`))

	listings, err := ghcli.NewClientWithRunner(runner).List(context.Background(),
		ports.PRListFilter{State: "open", Limit: 10})

	require.NoError(t, err)
	require.Len(t, listings, 1)
	assert.Equal(t, "first", listings[0].Title)
	assert.Equal(t, 1, listings[0].Number)
}

func TestClient_List_NoFilters_OmitsFlags(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("pr list --json url,state,title,number", []byte(`[]`))

	_, err := ghcli.NewClientWithRunner(runner).List(context.Background(), ports.PRListFilter{})

	require.NoError(t, err)
}

func TestClient_Comment_PassesBody(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("pr comment https://e/pr/1 --body looks good", []byte(""))

	err := ghcli.NewClientWithRunner(runner).Comment(context.Background(), "https://e/pr/1", "looks good")

	require.NoError(t, err)
}

func TestClient_Merge_UsesSquash(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.On("pr merge https://e/pr/1 --squash", []byte(""))

	err := ghcli.NewClientWithRunner(runner).Merge(context.Background(), "https://e/pr/1")

	require.NoError(t, err)
	require.Len(t, runner.calls, 1)
	assert.Contains(t, runner.calls[0], "--squash")
}

func TestClient_NewClient_BindsRepoDir(t *testing.T) {
	t.Parallel()

	client := ghcli.NewClient("/tmp/some/repo")

	// Construction smoke test — the real exec won't fire in this
	// test (no `gh` PR exists at the URL) but the client must build.
	assert.NotNil(t, client)
}

// Sanity-check that the execRunner reports a meaningful error when
// `gh` is invoked with bogus args. Hits the real exec path so the
// production runner is exercised end-to-end.
func TestClient_NewClient_ExecRunner_BogusArgs_ReturnsError(t *testing.T) {
	t.Parallel()

	client := ghcli.NewClient(t.TempDir())

	_, err := client.State(context.Background(), "https://invalid-pr-url/that-cant-resolve")

	require.Error(t, err)
}

// Ensure the adapter satisfies the port. Compile-time guard.
var _ ports.PullRequestHost = (*ghcli.Client)(nil)
