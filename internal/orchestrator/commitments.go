package orchestrator

import (
	"fmt"
	"regexp"
	"strings"
)

// Decision is a binding agreement extracted from a "DECISION: …" line
// in a team discussion. Unlike structured Commitments, Decisions are
// freeform English so they cannot be verified against a diff directly —
// the reviewer checks them qualitatively when they read the PR body.
type Decision struct {
	Source  string // display name of the agent who made the call
	Content string // text that followed "DECISION:"
}

// decisionLine matches lines like:
//
//	DECISION: ship Monday
//	decision: use factory pattern
//
// The sender prefix "Name: " before the DECISION marker is handled
// separately by ExtractDecisionsFromTranscript so it can attribute the
// source. Case-insensitive match on the keyword.
var decisionLine = regexp.MustCompile(`(?i)(?:^|[\s>])decision:\s*(.+)$`)

// ExtractDecisionsFromTranscript scans a collected discussion transcript
// (one "Name: message" entry per line) and returns every DECISION that
// was made, attributed to the speaker. Pure parse, no LLM calls.
func ExtractDecisionsFromTranscript(transcript string) []Decision {
	if transcript == "" {
		return nil
	}

	lines := strings.Split(transcript, "\n")
	decisions := make([]Decision, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		// Lines from collectDiscussion look like "> Name: text".
		// Strip the leading "> " if present.
		line = strings.TrimPrefix(line, "> ")

		source, body := splitSenderPrefix(line)
		match := decisionLine.FindStringSubmatch(body)
		if len(match) < 2 {
			continue
		}

		content := strings.TrimSpace(match[1])
		if content == "" {
			continue
		}

		decisions = append(decisions, Decision{
			Source:  source,
			Content: content,
		})
	}
	return decisions
}

// splitSenderPrefix separates a "Name: text" line into the sender
// name and the remaining body. Falls back to ("", line) when no
// prefix is present — including the case where the "name" would
// actually be a keyword like "DECISION", which isn't a speaker.
func splitSenderPrefix(line string) (sender, body string) {
	idx := strings.Index(line, ": ")
	if idx <= 0 || idx > 40 {
		return "", line
	}
	candidate := strings.TrimSpace(line[:idx])
	if strings.EqualFold(candidate, "decision") {
		return "", line
	}
	return candidate, strings.TrimSpace(line[idx+2:])
}

// FormatDecisionsForPrompt renders a bulleted list of decisions as a
// prompt section. Returns empty string when there are no decisions so
// callers can concatenate unconditionally.
func FormatDecisionsForPrompt(decisions []Decision) string {
	if len(decisions) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("\n## Binding Decisions From Discussion\n\n")
	builder.WriteString("The team reached these decisions during planning. Your implementation must honour them — no substitutes, no shortcuts:\n\n")

	for _, decision := range decisions {
		if decision.Source != "" {
			fmt.Fprintf(&builder, "- %s (decided by %s)\n", decision.Content, decision.Source)
			continue
		}
		fmt.Fprintf(&builder, "- %s\n", decision.Content)
	}

	builder.WriteString("\nWhen you open the PR, include a '## Decisions Honoured' section in the PR description that maps each decision above to the code that implements it.\n")
	return builder.String()
}

// FormatDecisionsForReview renders the decisions list for the reviewer
// so they can verify each one was honoured in the PR. Slightly
// different framing to the engineer-facing FormatDecisionsForPrompt.
func FormatDecisionsForReview(decisions []Decision) string {
	if len(decisions) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("\n## Decisions To Verify\n\n")
	builder.WriteString("These decisions were made during the discussion before implementation. Check the PR body and the code — flag any that were ignored or implemented differently:\n\n")

	for _, decision := range decisions {
		if decision.Source != "" {
			fmt.Fprintf(&builder, "- %s (from %s)\n", decision.Content, decision.Source)
			continue
		}
		fmt.Fprintf(&builder, "- %s\n", decision.Content)
	}

	return builder.String()
}

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
