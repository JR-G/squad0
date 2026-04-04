package routing

import (
	"context"
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
)

// ScrutinyLevel indicates how carefully a PR should be reviewed.
type ScrutinyLevel string

const (
	// ScrutinyLow means the engineer's PRs are consistently clean.
	ScrutinyLow ScrutinyLevel = "low"
	// ScrutinyNormal is the default review depth.
	ScrutinyNormal ScrutinyLevel = "normal"
	// ScrutinyHigh means the engineer's PRs frequently need revision.
	ScrutinyHigh ScrutinyLevel = "high"
)

const (
	opinionPrefix   = "[about:"
	opinionCleanPR  = "clean PRs"
	opinionNeedsFix = "PRs need revision"
)

// OpinionStore manages inter-agent beliefs about work quality.
// Uses the existing FactStore belief system with a naming convention.
type OpinionStore struct {
	factStores map[agent.Role]*memory.FactStore
}

// NewOpinionStore creates a store wrapping the per-agent fact stores.
func NewOpinionStore(factStores map[agent.Role]*memory.FactStore) *OpinionStore {
	return &OpinionStore{factStores: factStores}
}

// RecordReviewOutcome records a reviewer's observation about an
// engineer's work quality based on the review result.
func (store *OpinionStore) RecordReviewOutcome(
	ctx context.Context,
	reviewer, engineer agent.Role,
	approved bool,
	fixCycles int,
) error {
	factStore, ok := store.factStores[reviewer]
	if !ok {
		return nil
	}

	content := formatOpinion(engineer, approved, fixCycles)
	confidence := opinionConfidence(approved, fixCycles)

	_, err := factStore.CreateBelief(ctx, memory.Belief{
		Content:    content,
		Confidence: confidence,
	})
	if err != nil {
		return fmt.Errorf("recording opinion about %s: %w", engineer, err)
	}

	return nil
}

// ScrutinyFor returns the review scrutiny level for an engineer based
// on all reviewers' accumulated opinions.
func (store *OpinionStore) ScrutinyFor(ctx context.Context, engineer agent.Role) ScrutinyLevel {
	var positive, negative int

	for _, factStore := range store.factStores {
		beliefs, err := factStore.TopBeliefs(ctx, 20)
		if err != nil {
			continue
		}

		tag := fmt.Sprintf("%s%s]", opinionPrefix, engineer)
		for _, belief := range beliefs {
			if !strings.Contains(belief.Content, tag) {
				continue
			}

			if strings.Contains(belief.Content, opinionCleanPR) {
				positive++
			}
			if strings.Contains(belief.Content, opinionNeedsFix) {
				negative++
			}
		}
	}

	if positive > negative+2 {
		return ScrutinyLow
	}
	if negative > positive+1 {
		return ScrutinyHigh
	}
	return ScrutinyNormal
}

// ScrutinyHint returns a human-readable prompt hint for the given
// scrutiny level, or empty string for normal scrutiny.
func ScrutinyHint(level ScrutinyLevel, engineerName string) string {
	switch level { //nolint:exhaustive // normal returns empty
	case ScrutinyLow:
		return fmt.Sprintf("%s consistently delivers clean PRs — focus on architecture over nitpicks.", engineerName)
	case ScrutinyHigh:
		return fmt.Sprintf("%s's PRs have needed multiple revision cycles recently — review with extra care.", engineerName)
	default:
		return ""
	}
}

func formatOpinion(engineer agent.Role, approved bool, fixCycles int) string {
	tag := fmt.Sprintf("%s%s]", opinionPrefix, engineer)
	if approved && fixCycles == 0 {
		return tag + " " + opinionCleanPR
	}
	if fixCycles >= 2 {
		return tag + " " + opinionNeedsFix
	}
	return tag + " standard review outcome"
}

func opinionConfidence(approved bool, fixCycles int) float64 {
	if approved && fixCycles == 0 {
		return 0.7
	}
	if fixCycles >= 2 {
		return 0.6
	}
	return 0.5
}
