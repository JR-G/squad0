package orchestrator

import (
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

// ArgumentMap captures structured positions and concerns from a
// team discussion. Produces a formatted prompt section that gives
// the implementation session a clear understanding of what was
// debated and decided — not raw messages.
type ArgumentMap struct {
	Positions map[agent.Role]string
	Concerns  []string
	Evidence  []string
	Decision  string
}

// NewArgumentMap creates an empty argument map.
func NewArgumentMap() *ArgumentMap {
	return &ArgumentMap{
		Positions: make(map[agent.Role]string),
	}
}

// AddPosition records an agent's stated position.
func (am *ArgumentMap) AddPosition(role agent.Role, text string) {
	am.Positions[role] = text
}

// AddConcern records an unresolved concern.
func (am *ArgumentMap) AddConcern(text string) {
	am.Concerns = append(am.Concerns, text)
}

// AddEvidence records supporting evidence for a position.
func (am *ArgumentMap) AddEvidence(text string) {
	am.Evidence = append(am.Evidence, text)
}

// SetDecision records the final decision.
func (am *ArgumentMap) SetDecision(decision string) {
	am.Decision = decision
}

// IsEmpty returns true if no positions or concerns have been recorded.
func (am *ArgumentMap) IsEmpty() bool {
	return len(am.Positions) == 0 && len(am.Concerns) == 0
}

// Format produces a structured prompt section for the implementation
// session. Replaces raw message quoting with a clear summary.
func (am *ArgumentMap) Format(roster map[agent.Role]string) string {
	if am.IsEmpty() {
		return ""
	}

	var builder strings.Builder

	builder.WriteString("## Discussion Summary\n\n")
	am.formatPositions(&builder, roster)
	am.formatEvidence(&builder)
	am.formatConcerns(&builder)
	am.formatDecision(&builder)
	builder.WriteString("Incorporate this into your implementation. Address unresolved concerns if possible.\n")
	return builder.String()
}

func (am *ArgumentMap) formatPositions(builder *strings.Builder, roster map[agent.Role]string) {
	if len(am.Positions) == 0 {
		return
	}
	builder.WriteString("*Positions:*\n")
	for role, position := range am.Positions {
		name := roster[role]
		if name == "" {
			name = string(role)
		}
		fmt.Fprintf(builder, "- %s: %s\n", name, position)
	}
	builder.WriteString("\n")
}

func (am *ArgumentMap) formatEvidence(builder *strings.Builder) {
	if len(am.Evidence) == 0 {
		return
	}
	builder.WriteString("*Evidence:*\n")
	for _, evidence := range am.Evidence {
		fmt.Fprintf(builder, "- %s\n", evidence)
	}
	builder.WriteString("\n")
}

func (am *ArgumentMap) formatConcerns(builder *strings.Builder) {
	if len(am.Concerns) == 0 {
		return
	}
	builder.WriteString("*Unresolved Concerns:*\n")
	for _, concern := range am.Concerns {
		fmt.Fprintf(builder, "- %s\n", concern)
	}
	builder.WriteString("\n")
}

func (am *ArgumentMap) formatDecision(builder *strings.Builder) {
	if am.Decision == "" {
		return
	}
	fmt.Fprintf(builder, "*Decision:* %s\n\n", am.Decision)
}

// ClassifyMessage tries to categorise a message as a position,
// concern, or evidence. Used by the thread tracker to build the
// argument map during discussions.
func ClassifyMessage(text string, _ agent.Role) (category, content string) {
	lower := strings.ToLower(text)

	if containsConcernSignal(lower) {
		return "concern", text
	}

	if containsEvidenceSignal(lower) {
		return "evidence", text
	}

	if containsPositionSignal(lower) {
		return "position", text
	}

	// If the message is substantial enough, treat it as a position.
	if len(text) > 40 {
		return "position", text
	}

	return "", ""
}

var positionSignals = []string{
	"i think", "my approach", "i'd suggest", "we should",
	"let's go with", "the way i see it", "i propose",
}

var argConcernSignals = []string{
	"concerned about", "worried about", "what happens when",
	"the risk is", "might break", "edge case",
}

var evidenceSignals = []string{
	"because", "evidence", "data shows", "last time we",
	"the reason is", "proven by",
}

func containsPositionSignal(lower string) bool {
	for _, signal := range positionSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

func containsConcernSignal(lower string) bool {
	for _, signal := range argConcernSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

func containsEvidenceSignal(lower string) bool {
	for _, signal := range evidenceSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}
