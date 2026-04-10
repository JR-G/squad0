package orchestrator

import (
	"fmt"
	"strings"
)

// Commitment is a concrete, verifiable agreement extracted from a
// pre-implementation discussion.
type Commitment struct {
	ID          string // "cm-1", "cm-2"
	Source      string // who said it: "tech-lead", "engineer-1"
	Description string // "Use repository pattern for data access"
	CheckType   string // "pattern_present", "pattern_absent", "file_exists"
	CheckTarget string // file glob or grep pattern
	Verified    bool
}

// ParseCommitments parses pipe-delimited commitment lines.
func ParseCommitments(raw string) []Commitment {
	lines := strings.Split(raw, "\n")
	commitments := make([]Commitment, 0, len(lines))
	idx := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.ToUpper(line) == "NONE" {
			continue
		}

		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}

		checkType := strings.TrimSpace(parts[2])
		if !validCheckType(checkType) {
			continue
		}

		idx++
		commitments = append(commitments, Commitment{
			ID:          fmt.Sprintf("cm-%d", idx),
			Source:      strings.TrimSpace(parts[0]),
			Description: strings.TrimSpace(parts[1]),
			CheckType:   checkType,
			CheckTarget: strings.TrimSpace(parts[3]),
		})
	}

	return commitments
}

func validCheckType(checkType string) bool {
	switch checkType {
	case "pattern_present", "pattern_absent", "file_exists":
		return true
	default:
		return false
	}
}

// CheckCommitmentsWithDiff verifies commitments against diff data.
func CheckCommitmentsWithDiff(commitments []Commitment, diffFiles, diffContent string) []Commitment {
	result := make([]Commitment, len(commitments))
	copy(result, commitments)

	for idx := range result {
		result[idx].Verified = checkCommitment(result[idx], diffFiles, diffContent)
	}

	return result
}

func checkCommitment(cm Commitment, diffFiles, diffContent string) bool {
	switch cm.CheckType {
	case "file_exists":
		return strings.Contains(diffFiles, cm.CheckTarget)
	case "pattern_present":
		return strings.Contains(diffContent, cm.CheckTarget)
	case "pattern_absent":
		return !strings.Contains(diffContent, cm.CheckTarget)
	default:
		return false
	}
}

// FormatCommitmentsForPrompt formats commitments as a section to
// append to the implementation prompt.
func FormatCommitmentsForPrompt(commitments []Commitment) string {
	if len(commitments) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("\n## Team Commitments\n\n")
	builder.WriteString("The team agreed on these specific points. Your implementation must honour them:\n\n")

	for _, cm := range commitments {
		fmt.Fprintf(&builder, "- [%s] %s (from %s)\n", cm.ID, cm.Description, cm.Source)
	}

	return builder.String()
}

// FormatCommitmentReport formats verification results for posting
// to Slack.
func FormatCommitmentReport(commitments []Commitment) string {
	if len(commitments) == 0 {
		return ""
	}

	verified := 0
	for _, cm := range commitments {
		if cm.Verified {
			verified++
		}
	}

	if verified == len(commitments) {
		return fmt.Sprintf("All %d discussion commitments verified in the diff.", len(commitments))
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "%d/%d commitments verified.\n", verified, len(commitments))

	for _, cm := range commitments {
		if cm.Verified {
			continue
		}
		fmt.Fprintf(&builder, "UNVERIFIED: %s — %s (%s not found in diff)\n", cm.ID, cm.Description, cm.CheckTarget)
	}

	return builder.String()
}
