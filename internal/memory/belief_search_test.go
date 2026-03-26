package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractKeywords_EmptyString_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	result := memory.ExtractKeywords("")
	assert.Empty(t, result)
}

func TestExtractKeywords_ShortWords_Filtered(t *testing.T) {
	t.Parallel()

	// All words are under 3 characters — should return empty.
	result := memory.ExtractKeywords("I am on it")
	assert.Empty(t, result)
}

func TestExtractKeywords_StopWordsOnly_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	result := memory.ExtractKeywords("the and for with not does should")
	assert.Empty(t, result)
}

func TestExtractKeywords_MixedContent_ReturnsSignificant(t *testing.T) {
	t.Parallel()

	result := memory.ExtractKeywords("the retry logic should handle timeouts gracefully")
	assert.Contains(t, result, "retry")
	assert.Contains(t, result, "logic")
	assert.Contains(t, result, "handle")
	assert.Contains(t, result, "timeouts")
}

func TestExtractKeywords_PunctuationStripped(t *testing.T) {
	t.Parallel()

	result := memory.ExtractKeywords("error-handling; retries, (backoff)")
	assert.NotContains(t, result, ";")
	assert.NotContains(t, result, ",")
	assert.NotContains(t, result, "(")
	assert.NotContains(t, result, ")")
}

func TestExtractKeywords_MaxFiveTerms(t *testing.T) {
	t.Parallel()

	result := memory.ExtractKeywords(
		"authentication authorization middleware validation encryption serialisation compression",
	)
	// At most 5 terms joined with " OR ".
	terms := countORTerms(result)
	assert.LessOrEqual(t, terms, 5)
}

func TestExtractKeywords_ORJoined(t *testing.T) {
	t.Parallel()

	result := memory.ExtractKeywords("retry backoff exponential")
	assert.Contains(t, result, " OR ")
}

func TestSanitiseBeliefQuery_EmptyString_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	result := memory.SanitiseBeliefQuery("")
	assert.Empty(t, result)
}

func TestSanitiseBeliefQuery_WhitespaceOnly_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	result := memory.SanitiseBeliefQuery("   ")
	assert.Empty(t, result)
}

func TestSanitiseBeliefQuery_SpecialCharactersStripped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"double quotes", `"injection" "attempt"`},
		{"single quotes", `'quoted' 'terms'`},
		{"parentheses", `(grouped) (terms)`},
		{"asterisks", `wild*card`},
		{"colons", `prefix:value`},
		{"mixed special", `"*(': combined`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := memory.SanitiseBeliefQuery(tt.input)
			assert.NotContains(t, result, `""`)
			assert.NotContains(t, result, "'")
			assert.NotContains(t, result, "(")
			assert.NotContains(t, result, ")")
			assert.NotContains(t, result, "*")
		})
	}
}

func TestSanitiseBeliefQuery_HyphenSplits(t *testing.T) {
	t.Parallel()

	result := memory.SanitiseBeliefQuery("TASK-42")
	// Should split on hyphen and quote each term.
	assert.Contains(t, result, `"TASK"`)
	assert.Contains(t, result, `"42"`)
	assert.Contains(t, result, " OR ")
}

func TestSanitiseBeliefQuery_AllSpecialCharacters_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	result := memory.SanitiseBeliefQuery(`"*()':`)
	assert.Empty(t, result)
}

func TestSanitiseBeliefQuery_NormalTerms_Quoted(t *testing.T) {
	t.Parallel()

	result := memory.SanitiseBeliefQuery("auth retry")
	assert.Equal(t, `"auth" OR "retry"`, result)
}

func TestSearchBeliefsByKeyword_EmptyKeyword_ReturnsNil(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)

	results, err := factStore.SearchBeliefsByKeyword(context.Background(), "", 5)

	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestSearchBeliefsByKeyword_SpecialCharactersOnly_ReturnsNil(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)

	results, err := factStore.SearchBeliefsByKeyword(context.Background(), `"*()':`, 5)

	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestConfirmOrCreate_EmptyContent_CreatesNew(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	// Empty content has no keywords so findSimilarBelief returns 0.
	err := factStore.ConfirmOrCreate(ctx, "", "success")
	require.NoError(t, err)
}

// countORTerms counts the number of terms in an " OR "-joined string.
func countORTerms(joined string) int {
	if joined == "" {
		return 0
	}

	count := 1
	for idx := 0; idx < len(joined); idx++ {
		if idx+4 <= len(joined) && joined[idx:idx+4] == " OR " {
			count++
		}
	}

	return count
}
