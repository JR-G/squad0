package orchestrator_test

import (
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestValidateResponse_Clean_Passes(t *testing.T) {
	t.Parallel()

	rules := orchestrator.VoiceRules{
		MaxChars:        100,
		MaxSentences:    3,
		BannedPhrases:   []string{"forbidden"},
		RejectIfSimilar: 0.75,
	}

	result := orchestrator.ValidateResponse("Looks good to me.", rules, nil)

	assert.True(t, result.OK)
	assert.Empty(t, result.Reason)
}

func TestCheckLength_TooLong_Rejects(t *testing.T) {
	t.Parallel()

	rules := orchestrator.VoiceRules{MaxChars: 10}
	text := "This is definitely longer than ten characters."

	result := orchestrator.ValidateResponse(text, rules, nil)

	assert.False(t, result.OK)
	assert.Equal(t, "too long", result.Reason)
}

func TestCheckLength_Exact_Passes(t *testing.T) {
	t.Parallel()

	text := "hello"
	rules := orchestrator.VoiceRules{MaxChars: len(text)}

	result := orchestrator.ValidateResponse(text, rules, nil)

	assert.True(t, result.OK)
}

func TestCheckSentenceCount_TooMany_Rejects(t *testing.T) {
	t.Parallel()

	rules := orchestrator.VoiceRules{
		MaxChars:     500,
		MaxSentences: 2,
	}
	text := "First sentence. Second sentence. Third sentence."

	result := orchestrator.ValidateResponse(text, rules, nil)

	assert.False(t, result.OK)
	assert.Equal(t, "too many sentences", result.Reason)
}

func TestCheckSentenceCount_WithinLimit_Passes(t *testing.T) {
	t.Parallel()

	rules := orchestrator.VoiceRules{
		MaxChars:     500,
		MaxSentences: 3,
	}
	text := "First sentence. Second sentence."

	result := orchestrator.ValidateResponse(text, rules, nil)

	assert.True(t, result.OK)
}

func TestCheckBannedPhrases_MatchFound_Rejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		phrase string
		text   string
	}{
		{"exact match", "as an ai", "As an AI, I cannot do that."},
		{"mid sentence", "i can help with", "Sure, I can help with that task."},
		{"lowercase already", "on it!", "On it!"},
		{"partial sentence", "i'll go ahead", "I'll go ahead and fix it."},
		{"capability phrase", "my capabilities", "That's outside my capabilities right now."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rules := orchestrator.VoiceRules{
				MaxChars:      500,
				BannedPhrases: []string{tt.phrase},
			}

			result := orchestrator.ValidateResponse(tt.text, rules, nil)

			assert.False(t, result.OK)
			assert.Contains(t, result.Reason, "banned phrase")
		})
	}
}

func TestCheckBannedPhrases_Clean_Passes(t *testing.T) {
	t.Parallel()

	rules := orchestrator.VoiceRules{
		MaxChars:      500,
		BannedPhrases: []string{"forbidden", "not allowed"},
	}

	result := orchestrator.ValidateResponse("Perfectly normal response.", rules, nil)

	assert.True(t, result.OK)
}

func TestCheckSimilarity_Identical_Rejects(t *testing.T) {
	t.Parallel()

	text := "The auth module needs better error handling around retries."
	rules := orchestrator.VoiceRules{
		MaxChars:        500,
		RejectIfSimilar: 0.75,
	}
	recent := []string{"Alice: " + text}

	result := orchestrator.ValidateResponse(text, rules, recent)

	assert.False(t, result.OK)
	assert.Equal(t, "too similar to recent message", result.Reason)
}

func TestCheckSimilarity_Different_Passes(t *testing.T) {
	t.Parallel()

	rules := orchestrator.VoiceRules{
		MaxChars:        500,
		RejectIfSimilar: 0.75,
	}
	recent := []string{"Alice: The weather is sunny today and the birds are singing."}

	result := orchestrator.ValidateResponse("Database migrations need a rollback plan.", rules, recent)

	assert.True(t, result.OK)
}

func TestCheckSimilarity_EmptyRecent_Passes(t *testing.T) {
	t.Parallel()

	rules := orchestrator.VoiceRules{
		MaxChars:        500,
		RejectIfSimilar: 0.75,
	}

	result := orchestrator.ValidateResponse("Any text here.", rules, nil)

	assert.True(t, result.OK)
}

func TestJaccardSimilarity_Identical_ReturnsOne(t *testing.T) {
	t.Parallel()

	text := "the quick brown fox jumps over the lazy dog"
	sim := orchestrator.JaccardSimilarity(text, text)

	assert.InDelta(t, 1.0, sim, 0.001)
}

func TestJaccardSimilarity_Different_ReturnsZero(t *testing.T) {
	t.Parallel()

	sim := orchestrator.JaccardSimilarity("alpha beta gamma", "delta epsilon zeta")

	assert.InDelta(t, 0.0, sim, 0.001)
}

func TestJaccardSimilarity_PartialOverlap(t *testing.T) {
	t.Parallel()

	sim := orchestrator.JaccardSimilarity("the quick brown fox", "the quick red fox")

	assert.Greater(t, sim, 0.0)
	assert.Less(t, sim, 1.0)
}

func TestJaccardSimilarity_EmptyInput_ReturnsZero(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, 0.0, orchestrator.JaccardSimilarity("", "hello world"), 0.001)
	assert.InDelta(t, 0.0, orchestrator.JaccardSimilarity("hello world", ""), 0.001)
	assert.InDelta(t, 0.0, orchestrator.JaccardSimilarity("", ""), 0.001)
}

func TestCountSentences_Various(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"empty string", "", 0},
		{"no punctuation", "hello world", 1},
		{"single sentence", "Hello world.", 1},
		{"two sentences", "First. Second.", 2},
		{"question and statement", "Really? Yes.", 2},
		{"exclamation", "Wow! Nice work.", 2},
		{"ellipsis counts trailing dot", "Wait... really.", 2},
		{"trailing text no period", "First sentence. And more", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// countSentences is unexported, so test through ValidateResponse
			// with a high MaxSentences to let it pass, then check with exact
			// limit to verify the count.
			if tt.expected == 0 {
				// Empty text always passes.
				rules := orchestrator.VoiceRules{MaxChars: 500, MaxSentences: 1}
				result := orchestrator.ValidateResponse(tt.text, rules, nil)
				assert.True(t, result.OK)
				return
			}

			// Exact limit should pass.
			passRules := orchestrator.VoiceRules{
				MaxChars:     500,
				MaxSentences: tt.expected,
			}
			passResult := orchestrator.ValidateResponse(tt.text, passRules, nil)
			assert.True(t, passResult.OK, "expected %d sentences to pass limit of %d for %q",
				tt.expected, tt.expected, tt.text)

			// One below the expected count should fail.
			if tt.expected > 1 {
				failRules := orchestrator.VoiceRules{
					MaxChars:     500,
					MaxSentences: tt.expected - 1,
				}
				failResult := orchestrator.ValidateResponse(tt.text, failRules, nil)
				assert.False(t, failResult.OK, "expected %d sentences to fail limit of %d for %q",
					tt.expected, tt.expected-1, tt.text)
			}
		})
	}
}

func TestCheckSimilarity_StripsSpeakerPrefix(t *testing.T) {
	t.Parallel()

	// The similarity check strips "Name: " prefixes before comparing.
	text := "We should add retry logic to the payment module."
	rules := orchestrator.VoiceRules{
		MaxChars:        500,
		RejectIfSimilar: 0.75,
	}

	// Identical text with a speaker prefix should still be rejected.
	recent := []string{"Engineer-1: " + text}
	result := orchestrator.ValidateResponse(text, rules, recent)

	assert.False(t, result.OK)

	// Completely different text with a prefix should pass.
	recent2 := []string{"Engineer-1: The database schema looks correct."}
	result2 := orchestrator.ValidateResponse(text, rules, recent2)

	assert.True(t, result2.OK)
}

func TestJaccardSimilarity_SingleWord_ReturnsZero(t *testing.T) {
	t.Parallel()

	// A single word produces no bigrams.
	sim := orchestrator.JaccardSimilarity("hello", "hello")

	assert.InDelta(t, 0.0, sim, 0.001)
}

// Ensure long strings that exceed limits fail on the first check (length).
func TestValidateResponse_MultipleViolations_ReportsFirst(t *testing.T) {
	t.Parallel()

	rules := orchestrator.VoiceRules{
		MaxChars:      10,
		MaxSentences:  1,
		BannedPhrases: []string{"forbidden"},
	}
	text := strings.Repeat("x", 20) + " forbidden. Another sentence."

	result := orchestrator.ValidateResponse(text, rules, nil)

	assert.False(t, result.OK)
	assert.Equal(t, "too long", result.Reason, "length check should fire first")
}
