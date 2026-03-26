package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

// findingsKeywords are signals that an agent discovered something
// noteworthy during implementation. When detected in a transcript,
// the findings are persisted to the Linear ticket as a comment.
var findingsKeywords = []string{
	"discovered",
	"found",
	"unexpected",
	"gotcha",
	"warning",
	"blocker",
}

// ContainsFindings checks whether a transcript contains keywords
// that indicate the agent discovered something significant.
func ContainsFindings(transcript string) bool {
	lower := strings.ToLower(transcript)
	for _, keyword := range findingsKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// ExtractFindings pulls sentences containing findings keywords from
// the transcript. Returns at most 500 characters.
func ExtractFindings(transcript string) string {
	lower := strings.ToLower(transcript)
	sentences := splitSentences(transcript)

	var relevant []string
	for _, sentence := range sentences {
		sentenceLower := strings.ToLower(sentence)
		for _, keyword := range findingsKeywords {
			if strings.Contains(sentenceLower, keyword) {
				relevant = append(relevant, strings.TrimSpace(sentence))
				break
			}
		}
	}

	if len(relevant) == 0 {
		return ""
	}

	_ = lower // used for the keyword search above
	result := strings.Join(relevant, " ")

	return agent.TruncateSummary(result, 500)
}

// PersistFindings posts a summary of findings to the Linear ticket
// via a PM DirectSession. If the transcript contains no findings
// keywords, this is a no-op.
func (orch *Orchestrator) PersistFindings(ctx context.Context, ticket, transcript string) {
	if !ContainsFindings(transcript) {
		return
	}

	pmAgent := orch.agents[agent.RolePM]
	if pmAgent == nil {
		return
	}

	findings := ExtractFindings(transcript)
	if findings == "" {
		return
	}

	prompt := fmt.Sprintf(
		`Post a comment on Linear ticket %s summarising what was discovered during implementation.
Use the save_comment tool with issue identifier "%s" and body: "%s"`,
		ticket, ticket, findings)

	_, err := pmAgent.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("failed to persist findings for %s: %v", ticket, err)
	}
}

// splitSentences breaks text into sentences on period, exclamation,
// or newline boundaries.
func splitSentences(text string) []string {
	// Replace newlines with periods for splitting.
	normalised := strings.ReplaceAll(text, "\n", ". ")
	normalised = strings.ReplaceAll(normalised, "!  ", "! ")

	var sentences []string
	for _, delim := range []string{". ", "! "} {
		parts := strings.Split(normalised, delim)
		if len(parts) > 1 {
			sentences = parts
			break
		}
	}

	if len(sentences) == 0 {
		sentences = []string{normalised}
	}

	return sentences
}
