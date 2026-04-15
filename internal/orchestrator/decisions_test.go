package orchestrator_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractDecisionsFromTranscript_SingleDecision(t *testing.T) {
	t.Parallel()

	transcript := "" +
		"> Callum: i think we should use the repository pattern\n" +
		"> Sable: agreed\n" +
		"> Morgan: DECISION: Use repository pattern across data access\n"

	decisions := orchestrator.ExtractDecisionsFromTranscript(transcript)

	require.Len(t, decisions, 1)
	assert.Equal(t, "Morgan", decisions[0].Source)
	assert.Equal(t, "Use repository pattern across data access", decisions[0].Content)
}

func TestExtractDecisionsFromTranscript_MultipleDecisions(t *testing.T) {
	t.Parallel()

	transcript := "" +
		"> Morgan: DECISION: AppDependencies factory all the way\n" +
		"> Sable: DECISION: no app.locals\n"

	decisions := orchestrator.ExtractDecisionsFromTranscript(transcript)

	require.Len(t, decisions, 2)
	assert.Equal(t, "Morgan", decisions[0].Source)
	assert.Equal(t, "AppDependencies factory all the way", decisions[0].Content)
	assert.Equal(t, "Sable", decisions[1].Source)
	assert.Equal(t, "no app.locals", decisions[1].Content)
}

func TestExtractDecisionsFromTranscript_CaseInsensitive(t *testing.T) {
	t.Parallel()

	transcript := "> Morgan: decision: ship Wednesday"
	decisions := orchestrator.ExtractDecisionsFromTranscript(transcript)

	require.Len(t, decisions, 1)
	assert.Equal(t, "ship Wednesday", decisions[0].Content)
}

func TestExtractDecisionsFromTranscript_NoPrefix_FallsBackToUnknownSource(t *testing.T) {
	t.Parallel()

	// A line without the "> Name: " prefix — decision is still
	// extracted but attributed to no one.
	transcript := "DECISION: deploy to staging first"
	decisions := orchestrator.ExtractDecisionsFromTranscript(transcript)

	require.Len(t, decisions, 1)
	assert.Equal(t, "", decisions[0].Source)
	assert.Equal(t, "deploy to staging first", decisions[0].Content)
}

func TestExtractDecisionsFromTranscript_NoDecisions_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	transcript := "" +
		"> Callum: here's my plan\n" +
		"> Sable: looks fine\n"

	decisions := orchestrator.ExtractDecisionsFromTranscript(transcript)
	assert.Empty(t, decisions)
}

func TestExtractDecisionsFromTranscript_EmptyInput_ReturnsNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, orchestrator.ExtractDecisionsFromTranscript(""))
}

func TestExtractDecisionsFromTranscript_DecisionMidMessage_Ignored(t *testing.T) {
	t.Parallel()

	// "decision" not at line start is not a DECISION marker.
	transcript := "> Morgan: I'll make a decision about this later"
	decisions := orchestrator.ExtractDecisionsFromTranscript(transcript)
	assert.Empty(t, decisions)
}

func TestFormatDecisionsForPrompt_RendersBindingSection(t *testing.T) {
	t.Parallel()

	decisions := []orchestrator.Decision{
		{Source: "Morgan", Content: "Use factory pattern"},
		{Source: "Sable", Content: "No app.locals"},
	}

	output := orchestrator.FormatDecisionsForPrompt(decisions)

	assert.Contains(t, output, "## Binding Decisions From Discussion")
	assert.Contains(t, output, "Use factory pattern (decided by Morgan)")
	assert.Contains(t, output, "No app.locals (decided by Sable)")
	assert.Contains(t, output, "Decisions Honoured")
}

func TestFormatDecisionsForPrompt_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", orchestrator.FormatDecisionsForPrompt(nil))
}

func TestFormatDecisionsForPrompt_NoSource_OmitsAttribution(t *testing.T) {
	t.Parallel()

	decisions := []orchestrator.Decision{
		{Content: "ship Wednesday"},
	}

	output := orchestrator.FormatDecisionsForPrompt(decisions)
	assert.Contains(t, output, "- ship Wednesday\n")
	assert.NotContains(t, output, "decided by")
}

func TestFormatDecisionsForReview_RendersVerifySection(t *testing.T) {
	t.Parallel()

	decisions := []orchestrator.Decision{
		{Source: "Morgan", Content: "Use factory pattern"},
	}

	output := orchestrator.FormatDecisionsForReview(decisions)

	assert.Contains(t, output, "## Decisions To Verify")
	assert.Contains(t, output, "Use factory pattern (from Morgan)")
}

func TestFormatDecisionsForReview_Empty_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", orchestrator.FormatDecisionsForReview(nil))
}
