package orchestrator_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestParseOpenPRs_ValidJSON(t *testing.T) {
	t.Parallel()

	output := `[{"headRefName":"feat/jam-12","number":6,"url":"https://github.com/JR-G/makebook/pull/6"},{"headRefName":"feat/jam-9","number":10,"url":"https://github.com/JR-G/makebook/pull/10"}]`

	prs := orchestrator.ParseOpenPRsForTest(output)
	assert.Len(t, prs, 2)
}

func TestParseOpenPRs_NoBranch_Skipped(t *testing.T) {
	t.Parallel()

	output := `[{"headRefName":"main","number":1,"url":"https://github.com/test/repo/pull/1"}]`
	prs := orchestrator.ParseOpenPRsForTest(output)
	assert.Empty(t, prs)
}

func TestParseOpenPRs_Empty(t *testing.T) {
	t.Parallel()

	prs := orchestrator.ParseOpenPRsForTest("[]")
	assert.Empty(t, prs)
}

func TestExtractJSONField(t *testing.T) {
	t.Parallel()

	line := `{"headRefName":"feat/jam-12","url":"https://github.com/test/pull/6"}`
	assert.Equal(t, "feat/jam-12", orchestrator.ExtractJSONFieldForTest(line, "headRefName"))
	assert.Equal(t, "https://github.com/test/pull/6", orchestrator.ExtractJSONFieldForTest(line, "url"))
	assert.Empty(t, orchestrator.ExtractJSONFieldForTest(line, "missing"))
}
