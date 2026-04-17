package linear_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JR-G/squad0/internal/integrations/linear"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recorder struct {
	captured string
}

func (rec *recorder) serve(response string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.captured = string(body)
		_, _ = w.Write([]byte(response))
	}
}

func newClient(t *testing.T, handler http.HandlerFunc) *linear.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return linear.NewClient("test-key").WithAPIURL(server.URL)
}

func TestClient_GetIssue_Success_ParsesFullResponse(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	client := newClient(t, rec.serve(`{"data":{"issue":{"id":"uuid-1","identifier":"JAM-7","title":"Fix bug","description":"desc","priority":2,"url":"https://linear.app/x/issue/JAM-7","state":{"id":"s1","name":"Todo","type":"unstarted"},"team":{"id":"t1","key":"JAM","name":"Squad0"},"labels":{"nodes":[{"name":"bug"},{"name":"urgent"}]}}}}`))

	issue, err := client.GetIssue(context.Background(), "JAM-7")
	require.NoError(t, err)
	assert.Equal(t, "uuid-1", issue.ID)
	assert.Equal(t, "JAM-7", issue.Identifier)
	assert.Equal(t, "Todo", issue.State.Name)
	assert.Equal(t, "JAM", issue.Team.Key)
	assert.Equal(t, []string{"bug", "urgent"}, issue.Labels)
	assert.Contains(t, rec.captured, "JAM-7")
}

func TestClient_GetIssue_NotFound_ReturnsError(t *testing.T) {
	t.Parallel()

	client := newClient(t, (&recorder{}).serve(`{"data":{"issue":null}}`))

	_, err := client.GetIssue(context.Background(), "JAM-MISSING")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestClient_GetIssue_GraphQLErrors_Returned(t *testing.T) {
	t.Parallel()

	client := newClient(t, (&recorder{}).serve(`{"errors":[{"message":"bad auth"}]}`))

	_, err := client.GetIssue(context.Background(), "JAM-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad auth")
}

func TestClient_GetIssue_HTTP500_ReturnsError(t *testing.T) {
	t.Parallel()

	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	_, err := client.GetIssue(context.Background(), "JAM-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestClient_ListIssues_WithStateFilter_EmbedsStatesInQuery(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	client := newClient(t, rec.serve(`{"data":{"team":{"issues":{"nodes":[{"id":"u","identifier":"JAM-1","title":"t","description":"d","priority":3,"url":"u","state":{"id":"s","name":"Todo","type":"unstarted"},"team":{"id":"t"},"labels":{"nodes":[]}}]}}}}`))

	issues, err := client.ListIssues(context.Background(), linear.ListIssuesFilter{
		TeamID: "team-1", States: []string{"unstarted", "backlog"}, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "JAM-1", issues[0].Identifier)
	assert.Contains(t, rec.captured, "unstarted")
	assert.Contains(t, rec.captured, "backlog")
	assert.Contains(t, rec.captured, "first:10")
}

func TestClient_ListIssues_DefaultLimit_50(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	client := newClient(t, rec.serve(`{"data":{"team":{"issues":{"nodes":[]}}}}`))

	_, err := client.ListIssues(context.Background(), linear.ListIssuesFilter{TeamID: "team-1"})
	require.NoError(t, err)
	assert.Contains(t, rec.captured, "first:50")
}

func TestClient_ListIssues_MalformedJSON_Errors(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`oops`))
	_, err := client.ListIssues(context.Background(), linear.ListIssuesFilter{TeamID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func TestClient_ListIssues_HTTP401_Errors(t *testing.T) {
	t.Parallel()
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	})
	_, err := client.ListIssues(context.Background(), linear.ListIssuesFilter{TeamID: "x"})
	require.Error(t, err)
}

func TestClient_GetIssue_MalformedJSON_Errors(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`oops`))
	_, err := client.GetIssue(context.Background(), "JAM-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func TestClient_SaveIssue_MalformedJSON_Errors(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`oops`))
	err := client.SaveIssue(context.Background(), "JAM-1", linear.SaveIssueUpdate{StateID: "s"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func TestClient_SaveComment_MalformedJSON_Errors(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`oops`))
	err := client.SaveComment(context.Background(), "JAM-1", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func TestClient_SaveComment_HTTP500_Errors(t *testing.T) {
	t.Parallel()
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	})
	err := client.SaveComment(context.Background(), "JAM-1", "x")
	require.Error(t, err)
}

func TestClient_ListTeams_MalformedJSON_Errors(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`oops`))
	_, err := client.ListTeams(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func TestClient_ListIssues_GraphQLError_Returned(t *testing.T) {
	t.Parallel()

	client := newClient(t, (&recorder{}).serve(`{"errors":[{"message":"no team"}]}`))

	_, err := client.ListIssues(context.Background(), linear.ListIssuesFilter{TeamID: "bad"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no team")
}

func TestClient_ListTeams_GraphQLError_Returned(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`{"errors":[{"message":"auth failure"}]}`))
	_, err := client.ListTeams(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth failure")
}

func TestClient_ListTeams_HTTPError_Returned(t *testing.T) {
	t.Parallel()
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server down", http.StatusInternalServerError)
	})
	_, err := client.ListTeams(context.Background())
	require.Error(t, err)
}

func TestClient_ListTeams_Success(t *testing.T) {
	t.Parallel()

	client := newClient(t, (&recorder{}).serve(`{"data":{"teams":{"nodes":[{"id":"t1","key":"JAM","name":"Squad0"},{"id":"t2","key":"OPS","name":"Ops"}]}}}`))

	teams, err := client.ListTeams(context.Background())
	require.NoError(t, err)
	require.Len(t, teams, 2)
	assert.Equal(t, "JAM", teams[0].Key)
}

func TestClient_ListIssueStatuses_GraphQLError_Returned(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`{"errors":[{"message":"no team"}]}`))
	_, err := client.ListIssueStatuses(context.Background(), "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no team")
}

func TestClient_ListIssueStatuses_MalformedJSON_Returned(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`not json`))
	_, err := client.ListIssueStatuses(context.Background(), "t")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func TestClient_ListIssueStatuses_Success(t *testing.T) {
	t.Parallel()

	client := newClient(t, (&recorder{}).serve(`{"data":{"team":{"states":{"nodes":[{"id":"s1","name":"Todo","type":"unstarted"},{"id":"s2","name":"Done","type":"completed"}]}}}}`))

	states, err := client.ListIssueStatuses(context.Background(), "team-1")
	require.NoError(t, err)
	require.Len(t, states, 2)
	assert.Equal(t, "Done", states[1].Name)
}

func TestClient_SaveIssue_StateID_Success(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	client := newClient(t, rec.serve(`{"data":{"issueUpdate":{"success":true}}}`))

	err := client.SaveIssue(context.Background(), "JAM-1", linear.SaveIssueUpdate{StateID: "s-done"})
	require.NoError(t, err)
	assert.Contains(t, rec.captured, `stateId:\"s-done\"`)
}

func TestClient_SaveIssue_AllFields_Captured(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	client := newClient(t, rec.serve(`{"data":{"issueUpdate":{"success":true}}}`))

	priority := 2
	err := client.SaveIssue(context.Background(), "JAM-1", linear.SaveIssueUpdate{
		StateID: "s", Title: "new title", Description: "desc\n\"quoted\"", Priority: &priority,
	})
	require.NoError(t, err)
	assert.Contains(t, rec.captured, "new title")
	assert.Contains(t, rec.captured, "priority:2")
	assert.NotContains(t, rec.captured, "desc\n\"quoted\"", "raw unescaped content must not leak")
}

func TestClient_SaveIssue_NoFields_ReturnsError(t *testing.T) {
	t.Parallel()
	client := linear.NewClient("key")
	err := client.SaveIssue(context.Background(), "JAM-1", linear.SaveIssueUpdate{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no fields")
}

func TestClient_SaveIssue_SuccessFalse_ReturnsError(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`{"data":{"issueUpdate":{"success":false}}}`))
	err := client.SaveIssue(context.Background(), "JAM-1", linear.SaveIssueUpdate{StateID: "s"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "success=false")
}

func TestClient_SaveIssue_GraphQLError_Returned(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`{"errors":[{"message":"no permission"}]}`))
	err := client.SaveIssue(context.Background(), "JAM-1", linear.SaveIssueUpdate{StateID: "s"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no permission")
}

func TestClient_SaveComment_Success(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	client := newClient(t, rec.serve(`{"data":{"commentCreate":{"success":true,"comment":{"id":"c1"}}}}`))

	err := client.SaveComment(context.Background(), "JAM-1", "looks good")
	require.NoError(t, err)
	assert.Contains(t, rec.captured, "looks good")
}

func TestClient_SaveComment_SuccessFalse_ReturnsError(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`{"data":{"commentCreate":{"success":false}}}`))
	err := client.SaveComment(context.Background(), "JAM-1", "x")
	require.Error(t, err)
}

func TestClient_SaveComment_GraphQLError_Returned(t *testing.T) {
	t.Parallel()
	client := newClient(t, (&recorder{}).serve(`{"errors":[{"message":"bad issue"}]}`))
	err := client.SaveComment(context.Background(), "bad", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad issue")
}

func TestClient_Raw_WithVariables_EncodedIntoRequest(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	client := newClient(t, rec.serve(`{"data":{}}`))

	_, err := client.Raw(context.Background(), "query($id:ID!){issue(id:$id){id}}", map[string]interface{}{"id": "JAM-1"})
	require.NoError(t, err)
	var envelope struct {
		Variables map[string]interface{} `json:"variables"`
	}
	require.NoError(t, json.Unmarshal([]byte(rec.captured), &envelope))
	assert.Equal(t, "JAM-1", envelope.Variables["id"])
}

func TestClient_Raw_InvalidURL_ReturnsError(t *testing.T) {
	t.Parallel()
	client := linear.NewClient("key").WithAPIURL("http://ex\nample.com")
	_, err := client.Raw(context.Background(), "{teams{nodes{id}}}", nil)
	require.Error(t, err)
}

func TestClient_Raw_HTTPClient_OverrideUsed(t *testing.T) {
	t.Parallel()
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)
	client := linear.NewClient("key").WithAPIURL(srv.URL).WithHTTPClient(http.DefaultClient)
	_, _ = client.Raw(context.Background(), "{x}", nil)
	assert.True(t, called)
}

func TestClient_EscapeStringInQueries(t *testing.T) {
	t.Parallel()
	// Comment bodies with quotes, backslashes, and newlines must all
	// be escaped before landing in the GraphQL literal.
	rec := &recorder{}
	client := newClient(t, rec.serve(`{"data":{"commentCreate":{"success":true}}}`))

	body := "line1\nhe said \"hi\" and \\slash"
	err := client.SaveComment(context.Background(), "iss", body)
	require.NoError(t, err)

	// Raw newlines and unescaped quotes would break the JSON payload.
	assert.NotContains(t, rec.captured, "\n")
	assert.NotContains(t, rec.captured, body, "raw body must not appear unescaped")
}
