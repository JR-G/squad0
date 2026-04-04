package routing_test

import (
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/routing"
	"github.com/stretchr/testify/assert"
)

func TestComplexityClassifier_Classify(t *testing.T) {
	t.Parallel()

	classifier := routing.NewComplexityClassifier("haiku", "sonnet", "opus")

	tests := []struct {
		name        string
		title       string
		description string
		labels      []string
		expected    routing.Complexity
	}{
		{"chore label is trivial", "update deps", "bump versions", []string{"chore"}, routing.Trivial},
		{"docs label is trivial", "fix readme", "typo in docs", []string{"docs"}, routing.Trivial},
		{"style label is trivial", "formatting", "gofumpt", []string{"style"}, routing.Trivial},
		{"short description is trivial", "fix typo", "one word", nil, routing.Trivial},
		{"architecture label is complex", "refactor auth", "redesign the auth module", []string{"architecture"}, routing.Complex},
		{"security label is complex", "fix xss", "sanitise inputs", []string{"security"}, routing.Complex},
		{"migration label is complex", "db migration", "alter tables", []string{"migration"}, routing.Complex},
		{"long description is complex", "big feature", strings.Repeat("x", 1100), nil, routing.Complex},
		{"normal ticket is standard", "add submit button", "Add a submit button to the user registration form. The button should validate all required fields before submission and display inline error messages. Include loading state while the API call is in progress. Follow the existing button component patterns.", nil, routing.Standard},
		{"mixed labels prefer complex", "update", "change things", []string{"chore", "architecture"}, routing.Complex},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := classifier.Classify(tt.title, tt.description, tt.labels)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestComplexityClassifier_ModelFor(t *testing.T) {
	t.Parallel()

	classifier := routing.NewComplexityClassifier("haiku", "sonnet", "opus")

	assert.Equal(t, "haiku", classifier.ModelFor(routing.Trivial))
	assert.Equal(t, "sonnet", classifier.ModelFor(routing.Standard))
	assert.Equal(t, "opus", classifier.ModelFor(routing.Complex))
}

func TestComplexity_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "trivial", routing.Trivial.String())
	assert.Equal(t, "standard", routing.Standard.String())
	assert.Equal(t, "complex", routing.Complex.String())
	assert.Equal(t, "unknown", routing.Complexity(99).String())
}

func TestComplexityClassifier_ModelFor_UnknownComplexity_FallsBackToStandard(t *testing.T) {
	t.Parallel()

	classifier := routing.NewComplexityClassifier("haiku", "sonnet", "opus")
	assert.Equal(t, "sonnet", classifier.ModelFor(routing.Complexity(99)))
}
