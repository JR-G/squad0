package tui_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/tui"
	"github.com/stretchr/testify/assert"
)

func TestBanner_ContainsSquad0(t *testing.T) {
	t.Parallel()
	assert.Contains(t, tui.Banner(), "Squad0")
}

func TestStepDone_ContainsCheckmark(t *testing.T) {
	t.Parallel()
	result := tui.StepDone("loaded config")
	assert.Contains(t, result, "✓")
	assert.Contains(t, result, "loaded config")
}

func TestStepFail_ContainsCross(t *testing.T) {
	t.Parallel()
	result := tui.StepFail("database error")
	assert.Contains(t, result, "✗")
	assert.Contains(t, result, "database error")
}

func TestStepPending_ContainsDot(t *testing.T) {
	t.Parallel()
	result := tui.StepPending("waiting")
	assert.Contains(t, result, "○")
}

func TestStepWarn_ContainsExclamation(t *testing.T) {
	t.Parallel()
	result := tui.StepWarn("caution")
	assert.Contains(t, result, "!")
	assert.Contains(t, result, "caution")
}

func TestSection_ContainsTitle(t *testing.T) {
	t.Parallel()
	result := tui.Section("Agents")
	assert.Contains(t, result, "Agents")
}
