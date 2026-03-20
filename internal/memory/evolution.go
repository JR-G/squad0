package memory

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// EvolutionConfig holds configuration for belief evolution.
type EvolutionConfig struct {
	DecayHalfLifeDays float64
	MinConfidence     float64
	DecayRate         float64
}

// DefaultEvolutionConfig returns sensible defaults for belief evolution.
func DefaultEvolutionConfig() EvolutionConfig {
	halfLife := 30.0
	return EvolutionConfig{
		DecayHalfLifeDays: halfLife,
		MinConfidence:     0.1,
		DecayRate:         math.Ln2 / halfLife,
	}
}

// RetrievalScore computes the effective retrieval score for a belief,
// incorporating confidence, temporal decay, and access frequency.
// Formula: confidence × exp(-decay_rate × days_since_confirmed) × log(1 + access_count)
func RetrievalScore(confidence, daysSinceConfirmed float64, accessCount int, decayRate float64) float64 {
	decay := math.Exp(-decayRate * daysSinceConfirmed)
	accessBoost := math.Log(float64(1 + accessCount + 1))
	return confidence * decay * accessBoost
}

// DecayBeliefs reduces the confidence of beliefs that haven't been
// confirmed or accessed recently. Uses exponential decay.
func DecayBeliefs(ctx context.Context, factStore *FactStore, cfg EvolutionConfig) (int, error) {
	beliefs, err := factStore.TopBeliefs(ctx, 1000)
	if err != nil {
		return 0, fmt.Errorf("loading beliefs for decay: %w", err)
	}

	now := time.Now()
	updated := 0

	for _, belief := range beliefs {
		newConfidence := computeDecayedConfidence(belief, now, cfg)

		if math.Abs(newConfidence-belief.Confidence) < 0.01 {
			continue
		}

		err := updateBeliefConfidence(ctx, factStore, belief.ID, newConfidence)
		if err != nil {
			return updated, fmt.Errorf("decaying belief %d: %w", belief.ID, err)
		}

		updated++
	}

	return updated, nil
}

func computeDecayedConfidence(belief Belief, now time.Time, cfg EvolutionConfig) float64 {
	lastActivity := mostRecentActivity(belief)
	daysSince := now.Sub(lastActivity).Hours() / 24.0
	decayFactor := math.Exp(-cfg.DecayRate * daysSince)
	newConfidence := belief.Confidence * decayFactor

	if newConfidence < cfg.MinConfidence {
		return cfg.MinConfidence
	}

	return newConfidence
}

func mostRecentActivity(belief Belief) time.Time {
	latest := belief.CreatedAt

	if belief.LastConfirmedAt != nil && belief.LastConfirmedAt.After(latest) {
		latest = *belief.LastConfirmedAt
	}

	if belief.LastAccessedAt != nil && belief.LastAccessedAt.After(latest) {
		latest = *belief.LastAccessedAt
	}

	return latest
}

func updateBeliefConfidence(ctx context.Context, factStore *FactStore, beliefID int64, confidence float64) error {
	_, err := factStore.db.RawDB().ExecContext(ctx,
		`UPDATE beliefs SET confidence = ? WHERE id = ?`,
		confidence, beliefID,
	)
	return err
}

// GeneratePersonalitySummary builds a summary section from an agent's
// accumulated beliefs and facts, suitable for appending to their base
// personality file. During consolidation, it clusters related beliefs
// and highlights the strongest patterns.
func GeneratePersonalitySummary(ctx context.Context, factStore *FactStore, graphStore *GraphStore, topK int) (string, error) {
	beliefs, err := factStore.TopBeliefs(ctx, topK)
	if err != nil {
		return "", fmt.Errorf("loading beliefs for personality: %w", err)
	}

	var builder strings.Builder

	if len(beliefs) == 0 {
		return "", nil
	}

	builder.WriteString("## Learned Beliefs\n\n")
	builder.WriteString("These are things you've learned from experience:\n\n")

	for _, belief := range beliefs {
		strength := describeStrength(belief)
		fmt.Fprintf(&builder, "- %s %s\n", strength, belief.Content)
	}
	builder.WriteString("\n")

	return builder.String(), nil
}

func describeStrength(belief Belief) string {
	switch {
	case belief.Confidence >= 0.8:
		return "(strong)"
	case belief.Confidence >= 0.5:
		return "(moderate)"
	default:
		return "(weak)"
	}
}
