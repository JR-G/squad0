package orchestrator_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestExtractName_MyNameIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		transcript string
		expected   string
	}{
		{
			name:       "simple my name is",
			transcript: "My name is Ada. I'm excited to join the team.",
			expected:   "Ada",
		},
		{
			name:       "i'm format",
			transcript: "Hi everyone! I'm Rex, and I specialise in architecture.",
			expected:   "Rex",
		},
		{
			name:       "call me format",
			transcript: "Call me Nova. I'll be managing the board.",
			expected:   "Nova",
		},
		{
			name:       "i am format",
			transcript: "I am Iris. I focus on user experience.",
			expected:   "Iris",
		},
		{
			name:       "name with period",
			transcript: "My name is Kai. Let's get to work.",
			expected:   "Kai",
		},
		{
			name:       "name with comma",
			transcript: "My name is Sage, and I love clean code.",
			expected:   "Sage",
		},
		{
			name:       "no recognisable pattern",
			transcript: "Hello team, looking forward to working together.",
			expected:   "",
		},
		{
			name:       "empty transcript",
			transcript: "",
			expected:   "",
		},
		{
			name:       "case insensitive",
			transcript: "MY NAME IS ATLAS. I handle infrastructure.",
			expected:   "ATLAS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := orchestrator.ExtractName(tt.transcript)
			assert.Equal(t, tt.expected, result)
		})
	}
}
