package runtime_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/runtime"
	"github.com/stretchr/testify/assert"
)

func TestSessionName_FormatsCorrectly(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "squad0-engineer-1", runtime.SessionName("engineer-1"))
	assert.Equal(t, "squad0-pm", runtime.SessionName("pm"))
}
