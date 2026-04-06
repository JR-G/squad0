package orchestrator

import (
	"strings"
	"unicode"
)

// ValidationResult reports whether a response passed output validation.
type ValidationResult struct {
	OK     bool
	Reason string
}

func validationOK() ValidationResult { return ValidationResult{OK: true} }

func validationFail(reason string) ValidationResult {
	return ValidationResult{OK: false, Reason: reason}
}

// ValidateResponse runs all checks against the voice rules. Returns
// the first failure. Pure function — no side effects.
func ValidateResponse(text string, rules VoiceRules, recentMessages []string) ValidationResult {
	if result := checkLength(text, rules.MaxChars); !result.OK {
		return result
	}

	if result := checkSentenceCount(text, rules.MaxSentences); !result.OK {
		return result
	}

	if result := checkSimilarity(text, recentMessages, rules.RejectIfSimilar); !result.OK {
		return result
	}

	return validationOK()
}

func checkLength(text string, maxChars int) ValidationResult {
	if maxChars <= 0 {
		return validationOK()
	}

	if len(text) > maxChars {
		return validationFail("too long")
	}

	return validationOK()
}

func checkSentenceCount(text string, maxSentences int) ValidationResult {
	if maxSentences <= 0 {
		return validationOK()
	}

	count := countSentences(text)
	if count > maxSentences {
		return validationFail("too many sentences")
	}

	return validationOK()
}

func countSentences(text string) int {
	if text == "" {
		return 0
	}

	count := 0
	for idx, ch := range text {
		if ch != '.' && ch != '!' && ch != '?' {
			continue
		}
		// Don't count trailing punctuation or ellipsis.
		if idx+1 < len(text) && (text[idx+1] == '.' || unicode.IsLetter(rune(text[idx+1]))) {
			continue
		}
		count++
	}

	// At least one sentence if there's text.
	if count == 0 && strings.TrimSpace(text) != "" {
		count = 1
	}

	return count
}

func checkSimilarity(text string, recentMessages []string, threshold float64) ValidationResult {
	if threshold <= 0 || len(recentMessages) == 0 {
		return validationOK()
	}

	for _, recent := range recentMessages {
		// Strip the "name: " prefix from recent lines.
		if idx := strings.Index(recent, ": "); idx > 0 && idx < 30 {
			recent = recent[idx+2:]
		}

		sim := JaccardSimilarity(text, recent)
		if sim >= threshold {
			return validationFail("too similar to recent message")
		}
	}

	return validationOK()
}

// JaccardSimilarity computes the Jaccard index on word-level bigrams.
func JaccardSimilarity(textA, textB string) float64 {
	bigramsA := wordBigrams(textA)
	bigramsB := wordBigrams(textB)

	if len(bigramsA) == 0 || len(bigramsB) == 0 {
		return 0
	}

	intersection := 0
	for bigram := range bigramsA {
		if bigramsB[bigram] {
			intersection++
		}
	}

	union := len(bigramsA) + len(bigramsB) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

func wordBigrams(text string) map[string]bool {
	words := strings.Fields(strings.ToLower(text))
	if len(words) < 2 {
		return nil
	}

	bigrams := make(map[string]bool, len(words)-1)
	for idx := range len(words) - 1 {
		bigrams[words[idx]+" "+words[idx+1]] = true
	}

	return bigrams
}
