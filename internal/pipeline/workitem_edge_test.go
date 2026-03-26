package pipeline_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkItemStore_DB_ReturnsUnderlyingDB(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))

	raw := store.DB()

	assert.NotNil(t, raw)
	assert.Equal(t, db, raw)
}
