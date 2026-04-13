package memory_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLearningsJSON_StrictForm_AllFields(t *testing.T) {
	t.Parallel()

	data := `{
		"facts": [
			{"entity_name": "auth", "entity_type": "module", "content": "handles JWT", "fact_type": "observation"}
		],
		"beliefs": [
			{"content": "always validate tokens server-side"}
		],
		"entities": [
			{"name": "auth", "type": "module"}
		]
	}`

	result, err := memory.ParseLearningsJSON(data)
	require.NoError(t, err)
	require.Len(t, result.Facts, 1)
	assert.Equal(t, "auth", result.Facts[0].EntityName)
	require.Len(t, result.Beliefs, 1)
	assert.Equal(t, "always validate tokens server-side", result.Beliefs[0].Content)
	require.Len(t, result.Entities, 1)
	assert.Equal(t, "auth", result.Entities[0].Name)
}

func TestParseLearningsJSON_BeliefsAsString_Normalised(t *testing.T) {
	t.Parallel()

	data := `{"facts": [], "beliefs": "session tokens should never be stored in localStorage", "entities": []}`

	result, err := memory.ParseLearningsJSON(data)
	require.NoError(t, err)
	require.Len(t, result.Beliefs, 1)
	assert.Equal(t, "session tokens should never be stored in localStorage", result.Beliefs[0].Content)
}

func TestParseLearningsJSON_BeliefsAsArrayOfStrings_Normalised(t *testing.T) {
	t.Parallel()

	data := `{"facts": [], "beliefs": ["first belief", "second belief"], "entities": []}`

	result, err := memory.ParseLearningsJSON(data)
	require.NoError(t, err)
	require.Len(t, result.Beliefs, 2)
	assert.Equal(t, "first belief", result.Beliefs[0].Content)
	assert.Equal(t, "second belief", result.Beliefs[1].Content)
}

func TestParseLearningsJSON_FactsAsArrayOfStrings_Normalised(t *testing.T) {
	t.Parallel()

	data := `{"facts": ["the auth middleware retries on 401"], "beliefs": [], "entities": []}`

	result, err := memory.ParseLearningsJSON(data)
	require.NoError(t, err)
	require.Len(t, result.Facts, 1)
	assert.Equal(t, "the auth middleware retries on 401", result.Facts[0].Content)
	assert.Equal(t, "observation", result.Facts[0].FactType)
	assert.Equal(t, "session", result.Facts[0].EntityName)
}

func TestParseLearningsJSON_FactsAsSingleString_Normalised(t *testing.T) {
	t.Parallel()

	data := `{"facts": "learned something", "beliefs": [], "entities": []}`

	result, err := memory.ParseLearningsJSON(data)
	require.NoError(t, err)
	require.Len(t, result.Facts, 1)
	assert.Equal(t, "learned something", result.Facts[0].Content)
}

func TestParseLearningsJSON_EntitiesAsArrayOfStrings_Normalised(t *testing.T) {
	t.Parallel()

	data := `{"facts": [], "beliefs": [], "entities": ["auth", "session", "token"]}`

	result, err := memory.ParseLearningsJSON(data)
	require.NoError(t, err)
	require.Len(t, result.Entities, 3)
	assert.Equal(t, "auth", result.Entities[0].Name)
	assert.Equal(t, "concept", result.Entities[0].Type)
}

func TestParseLearningsJSON_EntitiesAsSingleString_Normalised(t *testing.T) {
	t.Parallel()

	data := `{"facts": [], "beliefs": [], "entities": "auth"}`

	result, err := memory.ParseLearningsJSON(data)
	require.NoError(t, err)
	require.Len(t, result.Entities, 1)
	assert.Equal(t, "auth", result.Entities[0].Name)
}

func TestParseLearningsJSON_NullFields_NoError(t *testing.T) {
	t.Parallel()

	data := `{"facts": null, "beliefs": null, "entities": null}`

	result, err := memory.ParseLearningsJSON(data)
	require.NoError(t, err)
	assert.Empty(t, result.Facts)
	assert.Empty(t, result.Beliefs)
	assert.Empty(t, result.Entities)
}

func TestParseLearningsJSON_EmptyString_TreatedAsNone(t *testing.T) {
	t.Parallel()

	data := `{"facts": "", "beliefs": "", "entities": ""}`

	result, err := memory.ParseLearningsJSON(data)
	require.NoError(t, err)
	assert.Empty(t, result.Facts)
	assert.Empty(t, result.Beliefs)
	assert.Empty(t, result.Entities)
}

func TestParseLearningsJSON_MixedStrictAndLoose_Normalises(t *testing.T) {
	t.Parallel()

	data := `{
		"facts": [{"entity_name": "x", "entity_type": "m", "content": "c", "fact_type": "observation"}],
		"beliefs": ["simpler form"],
		"entities": [{"name": "auth", "type": "module"}]
	}`

	result, err := memory.ParseLearningsJSON(data)
	require.NoError(t, err)
	require.Len(t, result.Facts, 1)
	require.Len(t, result.Beliefs, 1)
	assert.Equal(t, "simpler form", result.Beliefs[0].Content)
	require.Len(t, result.Entities, 1)
}

func TestParseLearningsJSON_InvalidTopLevel_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := memory.ParseLearningsJSON(`not json at all`)
	require.Error(t, err)
}

func TestParseLearningsJSON_UnrecognisedBeliefShape_ReturnsError(t *testing.T) {
	t.Parallel()

	data := `{"facts": [], "beliefs": 42, "entities": []}`

	_, err := memory.ParseLearningsJSON(data)
	require.Error(t, err)
}

func TestParseLearningsJSON_UnrecognisedFactShape_ReturnsError(t *testing.T) {
	t.Parallel()

	data := `{"facts": 42, "beliefs": [], "entities": []}`

	_, err := memory.ParseLearningsJSON(data)
	require.Error(t, err)
}

func TestParseLearningsJSON_UnrecognisedEntityShape_ReturnsError(t *testing.T) {
	t.Parallel()

	data := `{"facts": [], "beliefs": [], "entities": 42}`

	_, err := memory.ParseLearningsJSON(data)
	require.Error(t, err)
}
