package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

// Assignment represents a ticket assigned to an agent by the PM.
type Assignment struct {
	Role        agent.Role
	Ticket      string
	Description string
	WorkingDir  string
}

// Assigner uses the PM agent to decide ticket assignments for idle
// engineers.
type Assigner struct {
	pmAgent *agent.Agent
	teamKey string
}

// NewAssigner creates an Assigner backed by the given PM agent.
func NewAssigner(pmAgent *agent.Agent, teamKey string) *Assigner {
	return &Assigner{pmAgent: pmAgent, teamKey: teamKey}
}

// RequestAssignments asks the PM to review the Linear board and assign
// tickets to the given idle engineers.
func (assigner *Assigner) RequestAssignments(ctx context.Context, idleEngineers []agent.Role) ([]Assignment, error) {
	prompt := buildAssignmentPrompt(idleEngineers, assigner.teamKey)

	result, err := assigner.pmAgent.ExecuteTask(ctx, prompt, nil, "")
	if err != nil {
		return nil, fmt.Errorf("PM assignment session failed: %w", err)
	}

	assignments, err := parseAssignments(result.Transcript, idleEngineers)
	if err != nil {
		return nil, fmt.Errorf("parsing PM assignments: %w", err)
	}

	return assignments, nil
}

func buildAssignmentPrompt(idleEngineers []agent.Role, teamKey string) string {
	var builder strings.Builder

	builder.WriteString("You are the PM. Your job is to check the Linear board and assign work.\n\n")

	fmt.Fprintf(&builder, "Use the Linear MCP tools to find issues in the %s team that are ready to be worked on.\n", teamKey)
	builder.WriteString("Look for issues with status 'Todo' or 'Ready' or 'Backlog' that haven't been assigned.\n\n")

	builder.WriteString("Available engineers:\n")
	for _, role := range idleEngineers {
		fmt.Fprintf(&builder, "- %s\n", role)
	}

	builder.WriteString("\nBased on the tickets you find, assign them to engineers.\n")
	builder.WriteString("Respond with ONLY a JSON array — no explanation, no markdown, no code fences.\n")
	builder.WriteString("Format: [{\"role\": \"engineer-1\", \"ticket\": \"JAM-42\", \"description\": \"Brief task description\"}]\n")
	builder.WriteString("If no tickets are ready, respond with: []\n")

	return builder.String()
}

func parseAssignments(transcript string, validRoles []agent.Role) ([]Assignment, error) {
	jsonStr := extractJSON(transcript)
	if jsonStr == "" {
		return nil, nil
	}

	var raw []struct {
		Role        string `json:"role"`
		Ticket      string `json:"ticket"`
		Description string `json:"description"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("unmarshalling assignments: %w", err)
	}

	validSet := make(map[agent.Role]bool, len(validRoles))
	for _, role := range validRoles {
		validSet[role] = true
	}

	assignments := make([]Assignment, 0, len(raw))
	for _, entry := range raw {
		role := agent.Role(entry.Role)
		if !validSet[role] {
			continue
		}

		assignments = append(assignments, Assignment{
			Role:        role,
			Ticket:      entry.Ticket,
			Description: entry.Description,
		})
	}

	return assignments, nil
}

func extractJSON(text string) string {
	start := strings.Index(text, "[")
	if start == -1 {
		return ""
	}

	end := strings.LastIndex(text, "]")
	if end == -1 {
		return ""
	}

	return text[start : end+1]
}
