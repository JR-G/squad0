// Package linear is a thin GraphQL client for the Linear API. It
// backs both the orchestrator's direct state-transition path and the
// squad0-linear MCP server, which together let squad0 avoid the
// OAuth-managed claude.ai Linear connector entirely.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultAPIURL = "https://api.linear.app/graphql"

// Client talks to Linear's GraphQL endpoint with a static API key.
// Static keys never expire — that's the whole point of this package:
// the managed claude.ai connector's OAuth tokens go stale without
// warning and take every squad0 session down with them.
type Client struct {
	apiKey     string
	apiURL     string
	httpClient *http.Client
}

// NewClient returns a Client backed by http.DefaultClient.
func NewClient(apiKey string) *Client {
	return &Client{apiKey: apiKey, apiURL: defaultAPIURL, httpClient: http.DefaultClient}
}

// WithAPIURL overrides the endpoint (used in tests).
func (client *Client) WithAPIURL(url string) *Client {
	client.apiURL = url
	return client
}

// WithHTTPClient overrides the HTTP client (used in tests).
func (client *Client) WithHTTPClient(httpClient *http.Client) *Client {
	client.httpClient = httpClient
	return client
}

// Raw runs an arbitrary GraphQL query or mutation and returns the
// unparsed response body. Callers unmarshal into whatever shape they
// expect.
func (client *Client) Raw(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	payload := map[string]interface{}{"query": query}
	if len(variables) > 0 {
		payload["variables"] = variables
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", client.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", client.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linear returned %d: %s", resp.StatusCode, string(out))
	}

	return out, nil
}

// Issue represents a Linear issue as returned by GetIssue / ListIssues.
type Issue struct {
	ID          string   `json:"id"`
	Identifier  string   `json:"identifier"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    int      `json:"priority"`
	State       State    `json:"state"`
	Team        Team     `json:"team"`
	Labels      []string `json:"labels,omitempty"`
	URL         string   `json:"url"`
}

// State represents a workflow state.
type State struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// Team represents a Linear team.
type Team struct {
	ID   string `json:"id"`
	Key  string `json:"key,omitempty"`
	Name string `json:"name,omitempty"`
}

// Comment represents a comment on an issue.
type Comment struct {
	ID   string `json:"id"`
	Body string `json:"body"`
}

// GetIssue fetches a single issue by identifier (e.g. "JAM-12") or UUID.
func (client *Client) GetIssue(ctx context.Context, identifier string) (Issue, error) {
	escaped := escapeString(identifier)
	query := fmt.Sprintf(`{ issue(id: %q) { id identifier title description priority url state { id name type } team { id key name } labels { nodes { name } } } }`, escaped)

	raw, err := client.Raw(ctx, query, nil)
	if err != nil {
		return Issue{}, err
	}

	var resp struct {
		Data struct {
			Issue struct {
				ID          string `json:"id"`
				Identifier  string `json:"identifier"`
				Title       string `json:"title"`
				Description string `json:"description"`
				Priority    int    `json:"priority"`
				URL         string `json:"url"`
				State       State  `json:"state"`
				Team        Team   `json:"team"`
				Labels      struct {
					Nodes []struct {
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"labels"`
			} `json:"issue"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return Issue{}, fmt.Errorf("parsing issue response: %w", err)
	}
	if len(resp.Errors) > 0 {
		return Issue{}, graphQLErrors(resp.Errors)
	}
	if resp.Data.Issue.ID == "" {
		return Issue{}, fmt.Errorf("issue %q not found", identifier)
	}

	labels := make([]string, 0, len(resp.Data.Issue.Labels.Nodes))
	for _, node := range resp.Data.Issue.Labels.Nodes {
		labels = append(labels, node.Name)
	}

	return Issue{
		ID:          resp.Data.Issue.ID,
		Identifier:  resp.Data.Issue.Identifier,
		Title:       resp.Data.Issue.Title,
		Description: resp.Data.Issue.Description,
		Priority:    resp.Data.Issue.Priority,
		URL:         resp.Data.Issue.URL,
		State:       resp.Data.Issue.State,
		Team:        resp.Data.Issue.Team,
		Labels:      labels,
	}, nil
}

// ListIssuesFilter narrows ListIssues results.
type ListIssuesFilter struct {
	TeamID string
	States []string // workflow state type names, e.g. ["unstarted", "backlog"]
	Limit  int
}

// ListIssues returns issues matching the filter.
func (client *Client) ListIssues(ctx context.Context, filter ListIssuesFilter) ([]Issue, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}

	stateFilter := ""
	if len(filter.States) > 0 {
		quoted := make([]string, len(filter.States))
		for i, state := range filter.States {
			quoted[i] = fmt.Sprintf(`\"%s\"`, state)
		}
		stateFilter = fmt.Sprintf(`, filter:{state:{type:{in:[%s]}}}`, strings.Join(quoted, ","))
	}

	query := fmt.Sprintf(
		`{ team(id:%q) { issues(first:%d%s) { nodes { id identifier title description priority url state { id name type } team { id key name } labels { nodes { name } } } } } }`,
		escapeString(filter.TeamID), filter.Limit, stateFilter,
	)

	raw, err := client.Raw(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Team struct {
				Issues struct {
					Nodes []struct {
						ID          string `json:"id"`
						Identifier  string `json:"identifier"`
						Title       string `json:"title"`
						Description string `json:"description"`
						Priority    int    `json:"priority"`
						URL         string `json:"url"`
						State       State  `json:"state"`
						Team        Team   `json:"team"`
						Labels      struct {
							Nodes []struct {
								Name string `json:"name"`
							} `json:"nodes"`
						} `json:"labels"`
					} `json:"nodes"`
				} `json:"issues"`
			} `json:"team"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parsing issues response: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, graphQLErrors(resp.Errors)
	}

	out := make([]Issue, 0, len(resp.Data.Team.Issues.Nodes))
	for _, node := range resp.Data.Team.Issues.Nodes {
		labels := make([]string, 0, len(node.Labels.Nodes))
		for _, label := range node.Labels.Nodes {
			labels = append(labels, label.Name)
		}
		out = append(out, Issue{
			ID:          node.ID,
			Identifier:  node.Identifier,
			Title:       node.Title,
			Description: node.Description,
			Priority:    node.Priority,
			URL:         node.URL,
			State:       node.State,
			Team:        node.Team,
			Labels:      labels,
		})
	}
	return out, nil
}

// ListTeams returns the workspaces teams visible to the API key.
func (client *Client) ListTeams(ctx context.Context) ([]Team, error) {
	raw, err := client.Raw(ctx, `{ teams { nodes { id key name } } }`, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Teams struct {
				Nodes []Team `json:"nodes"`
			} `json:"teams"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parsing teams response: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, graphQLErrors(resp.Errors)
	}
	return resp.Data.Teams.Nodes, nil
}

// ListIssueStatuses returns the workflow states for a team.
func (client *Client) ListIssueStatuses(ctx context.Context, teamID string) ([]State, error) {
	query := fmt.Sprintf(`{ team(id:%q) { states { nodes { id name type } } } }`, escapeString(teamID))
	raw, err := client.Raw(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Team struct {
				States struct {
					Nodes []State `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parsing states response: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, graphQLErrors(resp.Errors)
	}
	return resp.Data.Team.States.Nodes, nil
}

// SaveIssueUpdate captures the fields SaveIssue can change.
type SaveIssueUpdate struct {
	StateID     string
	Title       string
	Description string
	Priority    *int
}

// SaveIssue applies updates to an issue and returns success.
func (client *Client) SaveIssue(ctx context.Context, identifier string, update SaveIssueUpdate) error {
	inputs := make([]string, 0, 4)
	if update.StateID != "" {
		inputs = append(inputs, fmt.Sprintf("stateId:%q", escapeString(update.StateID)))
	}
	if update.Title != "" {
		inputs = append(inputs, fmt.Sprintf("title:%q", escapeString(update.Title)))
	}
	if update.Description != "" {
		inputs = append(inputs, fmt.Sprintf("description:%q", escapeString(update.Description)))
	}
	if update.Priority != nil {
		inputs = append(inputs, fmt.Sprintf("priority:%d", *update.Priority))
	}
	if len(inputs) == 0 {
		return fmt.Errorf("no fields to update")
	}

	mutation := fmt.Sprintf(
		`mutation { issueUpdate(id:%q, input:{%s}) { success } }`,
		escapeString(identifier), strings.Join(inputs, ","),
	)

	raw, err := client.Raw(ctx, mutation, nil)
	if err != nil {
		return err
	}

	var resp struct {
		Data struct {
			IssueUpdate struct {
				Success bool `json:"success"`
			} `json:"issueUpdate"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parsing issueUpdate response: %w", err)
	}
	if len(resp.Errors) > 0 {
		return graphQLErrors(resp.Errors)
	}
	if !resp.Data.IssueUpdate.Success {
		return fmt.Errorf("issueUpdate returned success=false")
	}
	return nil
}

// SaveComment posts a comment on the issue.
func (client *Client) SaveComment(ctx context.Context, issueID, body string) error {
	mutation := fmt.Sprintf(
		`mutation { commentCreate(input:{issueId:%q, body:%q}) { success comment { id } } }`,
		escapeString(issueID), escapeString(body),
	)

	raw, err := client.Raw(ctx, mutation, nil)
	if err != nil {
		return err
	}

	var resp struct {
		Data struct {
			CommentCreate struct {
				Success bool    `json:"success"`
				Comment Comment `json:"comment"`
			} `json:"commentCreate"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parsing commentCreate response: %w", err)
	}
	if len(resp.Errors) > 0 {
		return graphQLErrors(resp.Errors)
	}
	if !resp.Data.CommentCreate.Success {
		return fmt.Errorf("commentCreate returned success=false")
	}
	return nil
}

type graphQLError struct {
	Message string `json:"message"`
}

func graphQLErrors(errs []graphQLError) error {
	parts := make([]string, len(errs))
	for i, gqlErr := range errs {
		parts[i] = gqlErr.Message
	}
	return fmt.Errorf("linear graphql: %s", strings.Join(parts, "; "))
}

// escapeString escapes a string for embedding in a GraphQL string literal.
func escapeString(input string) string {
	input = strings.ReplaceAll(input, `\`, `\\`)
	input = strings.ReplaceAll(input, `"`, `\"`)
	input = strings.ReplaceAll(input, "\n", `\n`)
	input = strings.ReplaceAll(input, "\r", `\r`)
	input = strings.ReplaceAll(input, "\t", `\t`)
	return input
}
