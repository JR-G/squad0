package orchestrator_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCommitments_ValidLines(t *testing.T) {
	t.Parallel()

	raw := `tech-lead|Use repository pattern|pattern_present|Repository
pm|Skip migration for now|pattern_absent|CREATE TABLE
engineer-1|Add error handler|file_exists|internal/errors.go`

	commitments := orchestrator.ParseCommitments(raw)

	require.Len(t, commitments, 3)
	assert.Equal(t, "cm-1", commitments[0].ID)
	assert.Equal(t, "tech-lead", commitments[0].Source)
	assert.Equal(t, "Use repository pattern", commitments[0].Description)
	assert.Equal(t, "pattern_present", commitments[0].CheckType)
	assert.Equal(t, "Repository", commitments[0].CheckTarget)

	assert.Equal(t, "pattern_absent", commitments[1].CheckType)
	assert.Equal(t, "file_exists", commitments[2].CheckType)
}

func TestParseCommitments_NONE_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, orchestrator.ParseCommitments("NONE"))
}

func TestParseCommitments_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, orchestrator.ParseCommitments(""))
}

func TestParseCommitments_InvalidCheckType_Skipped(t *testing.T) {
	t.Parallel()

	raw := "tech-lead|Do something|invalid_type|target"
	assert.Empty(t, orchestrator.ParseCommitments(raw))
}

func TestParseCommitments_TooFewParts_Skipped(t *testing.T) {
	t.Parallel()

	raw := "tech-lead|incomplete"
	assert.Empty(t, orchestrator.ParseCommitments(raw))
}

func TestParseCommitments_MixedValid_FiltersInvalid(t *testing.T) {
	t.Parallel()

	raw := `tech-lead|Valid one|pattern_present|Repository
this is not a commitment
pm|Valid two|file_exists|handler.go`

	commitments := orchestrator.ParseCommitments(raw)
	require.Len(t, commitments, 2)
	assert.Equal(t, "cm-1", commitments[0].ID)
	assert.Equal(t, "cm-2", commitments[1].ID)
}

func TestCheckCommitmentsWithDiff_AllTypes(t *testing.T) {
	t.Parallel()

	commitments := []orchestrator.Commitment{
		{ID: "cm-1", CheckType: "pattern_present", CheckTarget: "Repository"},
		{ID: "cm-2", CheckType: "pattern_absent", CheckTarget: "CREATE TABLE"},
		{ID: "cm-3", CheckType: "file_exists", CheckTarget: "internal/errors.go"},
	}

	result := orchestrator.CheckCommitmentsWithDiff(
		commitments,
		"internal/errors.go\ninternal/handler.go\n",
		"+type UserRepository struct {\n-old code\n",
	)

	assert.True(t, result[0].Verified, "Repository pattern should be found")
	assert.True(t, result[1].Verified, "CREATE TABLE should be absent")
	assert.True(t, result[2].Verified, "errors.go should exist in diff")
}

func TestCheckCommitmentsWithDiff_PatternAbsent_FailsWhenPresent(t *testing.T) {
	t.Parallel()

	commitments := []orchestrator.Commitment{
		{ID: "cm-1", CheckType: "pattern_absent", CheckTarget: "raw SQL"},
	}

	result := orchestrator.CheckCommitmentsWithDiff(commitments, "", "+raw SQL query here")
	assert.False(t, result[0].Verified, "pattern_absent should fail when pattern is present")
}

func TestFormatCommitmentsForPrompt_FormatsCorrectly(t *testing.T) {
	t.Parallel()

	commitments := []orchestrator.Commitment{
		{ID: "cm-1", Source: "tech-lead", Description: "Use repository pattern"},
		{ID: "cm-2", Source: "pm", Description: "Skip migration"},
	}

	result := orchestrator.FormatCommitmentsForPrompt(commitments)
	assert.Contains(t, result, "Team Commitments")
	assert.Contains(t, result, "[cm-1]")
	assert.Contains(t, result, "repository pattern")
	assert.Contains(t, result, "from tech-lead")
}

func TestFormatCommitmentsForPrompt_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, orchestrator.FormatCommitmentsForPrompt(nil))
}

func TestFormatCommitmentReport_AllVerified(t *testing.T) {
	t.Parallel()

	commitments := []orchestrator.Commitment{
		{ID: "cm-1", Verified: true},
		{ID: "cm-2", Verified: true},
	}

	result := orchestrator.FormatCommitmentReport(commitments)
	assert.Contains(t, result, "All 2")
}

func TestFormatCommitmentReport_SomeUnverified(t *testing.T) {
	t.Parallel()

	commitments := []orchestrator.Commitment{
		{ID: "cm-1", Verified: true, Description: "Use repo pattern"},
		{ID: "cm-2", Verified: false, Description: "Add handler", CheckTarget: "handler.go"},
	}

	result := orchestrator.FormatCommitmentReport(commitments)
	assert.Contains(t, result, "1/2")
	assert.Contains(t, result, "UNVERIFIED")
	assert.Contains(t, result, "cm-2")
}

func TestFormatCommitmentReport_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, orchestrator.FormatCommitmentReport(nil))
}

func TestCheckCommitmentsWithDiff_UnknownCheckType_NotVerified(t *testing.T) {
	t.Parallel()

	commitments := []orchestrator.Commitment{
		{ID: "cm-1", CheckType: "unknown_type", CheckTarget: "anything"},
	}
	result := orchestrator.CheckCommitmentsWithDiff(commitments, "file.go", "content")
	assert.False(t, result[0].Verified)
}
