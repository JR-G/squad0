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
}

// NewAssigner creates an Assigner backed by the given PM agent.
func NewAssigner(pmAgent *agent.Agent) *Assigner {
	return &Assigner{pmAgent: pmAgent}
}

// RequestAssignments asks the PM to review the Linear board and assign
// tickets to the given idle engineers. Returns the PM's assignment
// decisions.
func (assigner *Assigner) RequestAssignments(ctx context.Context, idleEngineers []agent.Role) ([]Assignment, error) {
	prompt := buildAssignmentPrompt(idleEngineers)

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

func buildAssignmentPrompt(idleEngineers []agent.Role) string {
	var builder strings.Builder

	builder.WriteString("Review the Linear board for tickets with status 'Ready'.\n\n")
	builder.WriteString("Available engineers:\n")

	for _, role := range idleEngineers {
		fmt.Fprintf(&builder, "- %s\n", role)
	}

	builder.WriteString("\nAssign tickets to these engineers based on their strengths.\n")
	builder.WriteString("Respond with a JSON array of assignments:\n")
	builder.WriteString(`[{"role": "engineer-1", "ticket": "SQ-42", "description": "Brief task description"}]`)
	builder.WriteString("\n\nOnly assign tickets that are ready. If no tickets are ready, return an empty array: []\n")

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
