package orchestrator_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReviewBody_ExtractsBlockersAndSuggestions(t *testing.T) {
	t.Parallel()

	body := "1. [blocker] Missing error handling in internal/auth.go line 42\n" +
		"2. [suggestion] Consider using a constants file for magic strings\n" +
		"3. [blocker] deploy.ts never cleans up on failure\n" +
		"4. This is just a regular line — should be ignored"

	comments := orchestrator.ParseReviewBody(body)

	require.Len(t, comments, 3)
	assert.Equal(t, "rc-1", comments[0].ID)
	assert.Equal(t, "blocker", comments[0].Severity)
	assert.Contains(t, comments[0].Body, "Missing error handling")
	assert.Equal(t, "internal/auth.go", comments[0].Path)

	assert.Equal(t, "suggestion", comments[1].Severity)

	assert.Equal(t, "blocker", comments[2].Severity)
	assert.Equal(t, "deploy.ts", comments[2].Path)
}

func TestParseReviewBody_EmptyBody_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	comments := orchestrator.ParseReviewBody("")
	assert.Empty(t, comments)
}

func TestParseReviewBody_NoTaggedLines_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	body := "Looks good overall.\nNice work on the error handling.\nShip it."
	comments := orchestrator.ParseReviewBody(body)
	assert.Empty(t, comments)
}

func TestParseReviewBody_BlockerWithoutTag_MatchesKeyword(t *testing.T) {
	t.Parallel()

	body := "- blocker: the SQL injection in query.go needs fixing"
	comments := orchestrator.ParseReviewBody(body)

	require.Len(t, comments, 1)
	assert.Equal(t, "blocker", comments[0].Severity)
	assert.Equal(t, "query.go", comments[0].Path)
}

func TestFormatFixUpChecklist_FormatsBlockers(t *testing.T) {
	t.Parallel()

	comments := []orchestrator.ReviewComment{
		{ID: "rc-1", Severity: "blocker", Body: "Missing error handling in auth.go"},
		{ID: "rc-2", Severity: "suggestion", Body: "Use constants"},
	}

	checklist := orchestrator.FormatFixUpChecklist(comments)
	assert.Contains(t, checklist, "[rc-1]")
	assert.Contains(t, checklist, "BLOCKER")
	assert.Contains(t, checklist, "[rc-2]")
	assert.Contains(t, checklist, "suggestion")
	assert.Contains(t, checklist, "Address ALL blockers")
}

func TestFormatFixUpChecklist_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, orchestrator.FormatFixUpChecklist(nil))
}

func TestFormatReReviewChecklist_ShowsVerificationStatus(t *testing.T) {
	t.Parallel()

	comments := []orchestrator.ReviewComment{
		{ID: "rc-1", Severity: "blocker", Body: "Fix auth.go", Resolved: true},
		{ID: "rc-2", Severity: "blocker", Body: "Fix deploy.go", Resolved: false},
	}

	checklist := orchestrator.FormatReReviewChecklist(comments)
	assert.Contains(t, checklist, "rc-1")
	assert.Contains(t, checklist, "ADDRESSED")
	assert.Contains(t, checklist, "rc-2")
	assert.Contains(t, checklist, "NOT ADDRESSED")
}

func TestFormatReReviewChecklist_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, orchestrator.FormatReReviewChecklist(nil))
}

func TestCheckCommentsAddressed_MatchesFilePaths(t *testing.T) {
	t.Parallel()

	comments := []orchestrator.ReviewComment{
		{ID: "rc-1", Path: "internal/auth.go", Body: "fix auth"},
		{ID: "rc-2", Path: "internal/deploy.go", Body: "fix deploy"},
		{ID: "rc-3", Path: "", Body: "general comment"},
	}

	// Simulate: auth.go was changed, deploy.go was not.
	result := orchestrator.CheckCommentsAddressedWithDiff(comments, "internal/auth.go\ninternal/handler.go\n")

	assert.True(t, result[0].Resolved, "auth.go should be resolved")
	assert.False(t, result[1].Resolved, "deploy.go should NOT be resolved")
	assert.False(t, result[2].Resolved, "no path — cannot verify")
}

func TestParseReviewBody_ColonLineNumber_ExtractsPath(t *testing.T) {
	t.Parallel()

	comments := orchestrator.ParseReviewBody("1. [blocker] Fix auth.go:42 error handling")
	require.Len(t, comments, 1)
	assert.Equal(t, "auth.go", comments[0].Path)
}

func TestParseReviewBody_SlashPath_ExtractsPath(t *testing.T) {
	t.Parallel()

	comments := orchestrator.ParseReviewBody("1. [blocker] Fix internal/services/deploy.go immediately")
	require.Len(t, comments, 1)
	assert.Equal(t, "internal/services/deploy.go", comments[0].Path)
}

func TestParseReviewBody_TSFile_ExtractsPath(t *testing.T) {
	t.Parallel()

	comments := orchestrator.ParseReviewBody("1. [blocker] Fix src/index.ts error handling")
	require.Len(t, comments, 1)
	assert.Equal(t, "src/index.ts", comments[0].Path)
}

func TestSummariseVerification_AllResolved(t *testing.T) {
	t.Parallel()
	comments := []orchestrator.ReviewComment{
		{Severity: "blocker", Resolved: true},
		{Severity: "blocker", Resolved: true},
	}
	assert.Contains(t, orchestrator.SummariseVerification(comments), "2/2")
}

func TestSummariseVerification_SomeUnresolved(t *testing.T) {
	t.Parallel()
	comments := []orchestrator.ReviewComment{
		{Severity: "blocker", Resolved: true},
		{Severity: "blocker", Resolved: false},
	}
	assert.Contains(t, orchestrator.SummariseVerification(comments), "1/2")
}

func TestSummariseVerification_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, orchestrator.SummariseVerification(nil))
}

func TestSummariseVerification_NoBlockers_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	comments := []orchestrator.ReviewComment{{Severity: "suggestion", Resolved: false}}
	assert.Empty(t, orchestrator.SummariseVerification(comments))
}

func TestCheckCommentsAddressedWithDiff_EmptyComments_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, orchestrator.CheckCommentsAddressedWithDiff(nil, "some diff"))
}

func TestFormatReReviewChecklist_MixedStatus_ShowsAll(t *testing.T) {
	t.Parallel()

	comments := []orchestrator.ReviewComment{
		{ID: "rc-1", Severity: "blocker", Body: "Fix auth", Resolved: true},
		{ID: "rc-2", Severity: "blocker", Body: "Fix deploy", Resolved: false},
		{ID: "rc-3", Severity: "suggestion", Body: "Consider constants", Resolved: false},
	}

	checklist := orchestrator.FormatReReviewChecklist(comments)
	assert.Contains(t, checklist, "ADDRESSED")
	assert.Contains(t, checklist, "NOT ADDRESSED")
}
