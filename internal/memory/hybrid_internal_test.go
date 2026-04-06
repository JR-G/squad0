package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormaliseKeywordScore_PositiveMax(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0.5, normaliseKeywordScore(5, 10))
	assert.Equal(t, 1.0, normaliseKeywordScore(10, 10))
}
