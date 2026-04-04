package routing_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/routing"
	"github.com/stretchr/testify/assert"
)

func TestClassifyDepth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		complexity routing.Complexity
		expected   routing.Depth
	}{
		{"trivial skips discussion", routing.Trivial, routing.DepthNone},
		{"standard gets light discussion", routing.Standard, routing.DepthLight},
		{"complex gets full discussion", routing.Complex, routing.DepthFull},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, routing.ClassifyDepth(tt.complexity))
		})
	}
}

func TestDepth_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "none", routing.DepthNone.String())
	assert.Equal(t, "light", routing.DepthLight.String())
	assert.Equal(t, "full", routing.DepthFull.String())
	assert.Equal(t, "unknown", routing.Depth(99).String())
}
