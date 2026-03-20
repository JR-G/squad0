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
}

// DefaultEvolutionConfig returns sensible defaults for belief evolution.
func DefaultEvolutionConfig() EvolutionConfig {
	return EvolutionConfig{
		DecayHalfLifeDays: 30.0,
		MinConfidence:     0.1,
	}
}

// DecayBeliefs reduces the confidence of beliefs that haven't been
// confirmed recently. Uses exponential decay with the configured
// half-life.
func DecayBeliefs(ctx context.Context, factStore *FactStore, cfg EvolutionConfig) (int, error) {
	beliefs, err := factStore.TopBeliefs(ctx, 1000)
	if err != nil {
		return 0, fmt.Errorf("loading beliefs for decay: %w", err)
	}

	now := time.Now()
	updated := 0

	for _, belief := range beliefs {
		lastConfirmed := belief.CreatedAt
		if belief.LastConfirmedAt != nil {
			lastConfirmed = *belief.LastConfirmedAt
		}

		daysSince := now.Sub(lastConfirmed).Hours() / 24.0
		decayFactor := math.Pow(0.5, daysSince/cfg.DecayHalfLifeDays)
		newConfidence := belief.Confidence * decayFactor

		if newConfidence < cfg.MinConfidence {
			newConfidence = cfg.MinConfidence
		}

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

func updateBeliefConfidence(ctx context.Context, factStore *FactStore, beliefID int64, confidence float64) error {
	_, err := factStore.db.RawDB().ExecContext(ctx,
		`UPDATE beliefs SET confidence = ? WHERE id = ?`,
		confidence, beliefID,
	)
	return err
}

// GeneratePersonalitySummary builds a summary section from an agent's
// accumulated beliefs and facts, suitable for appending to their base
// personality file.
func GeneratePersonalitySummary(ctx context.Context, factStore *FactStore, graphStore *GraphStore, topK int) (string, error) {
	beliefs, err := factStore.TopBeliefs(ctx, topK)
	if err != nil {
		return "", fmt.Errorf("loading beliefs for personality: %w", err)
	}

	var builder strings.Builder

	if len(beliefs) > 0 {
		builder.WriteString("## Learned Beliefs\n\n")
		builder.WriteString("These are things you've learned from experience:\n\n")

		for _, belief := range beliefs {
			fmt.Fprintf(&builder, "- (confidence: %.1f) %s\n", belief.Confidence, belief.Content)
		}
		builder.WriteString("\n")
	}

	return builder.String(), nil
}
