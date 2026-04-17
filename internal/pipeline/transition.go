package pipeline

import (
	"errors"
	"fmt"
)

// ErrIllegalTransition is returned when a stage transition is rejected
// by CanTransition. Wrap-friendly via errors.Is so callers can branch
// on it without parsing error strings.
var ErrIllegalTransition = errors.New("illegal stage transition")

// legalTransitions encodes the allowed Stage moves. A stage that
// permits an idempotent transition (X → X) appears in its own list,
// which keeps reconciler-driven re-advances cheap and side-effect
// free. Terminal stages (merged, failed) appear as keys mapping to
// empty slices — explicit "no further moves allowed" rather than an
// implicit absence.
//
// Transitions worth calling out:
//
//   - approved → reviewing is intentionally legal: a fresh blocking
//     comment after approval (e.g. CodeRabbit / Devin posting after
//     a stale-bot-supersession check resets) needs to push the PR
//     back into review without rewriting history.
//   - approved → changes_requested for the same reason — explicit
//     "go fix this" from a human after a previously-approved state.
//   - changes_requested → working closes the loop (engineer picks
//     back up) and is the most common live path through the graph.
//   - assigned → failed lets the orchestrator abandon a ticket that
//     never gets started (worktree creation failures, immediate
//     death, etc.) without forcing a no-op working transition first.
var legalTransitions = map[Stage][]Stage{
	StageAssigned:         {StageAssigned, StageWorking, StageFailed},
	StageWorking:          {StageWorking, StagePROpened, StageReviewing, StageFailed},
	StagePROpened:         {StagePROpened, StageReviewing, StageChangesRequested, StageFailed},
	StageReviewing:        {StageReviewing, StageApproved, StageChangesRequested, StageFailed},
	StageChangesRequested: {StageChangesRequested, StageWorking, StageReviewing, StageFailed},
	StageApproved:         {StageApproved, StageMerged, StageReviewing, StageChangesRequested, StageFailed},
	StageMerged:           {},
	StageFailed:           {},
}

// CanTransition reports whether moving from `from` to `to` is allowed
// by the lifecycle rules. Returns nil when the move is legal,
// ErrIllegalTransition (wrapped with context) otherwise.
//
// Idempotent same-stage transitions are always allowed — a reconciler
// re-asserting a state shouldn't error.
//
// Unknown stages on either side are rejected so that a typo or
// removed stage in calling code surfaces immediately rather than
// silently bypassing the lifecycle.
func CanTransition(from, to Stage) error {
	allowed, ok := legalTransitions[from]
	if !ok {
		return fmt.Errorf("%w: source stage %q is not recognised", ErrIllegalTransition, from)
	}
	if _, toOK := legalTransitions[to]; !toOK {
		return fmt.Errorf("%w: target stage %q is not recognised", ErrIllegalTransition, to)
	}

	for _, candidate := range allowed {
		if candidate == to {
			return nil
		}
	}
	return fmt.Errorf("%w: %s cannot move to %s", ErrIllegalTransition, from, to)
}

// LegalNextStages returns the stages that `from` is allowed to
// transition into (including itself if idempotent moves are
// permitted). Returned slice is a copy — callers can mutate freely.
func LegalNextStages(from Stage) []Stage {
	allowed, ok := legalTransitions[from]
	if !ok {
		return nil
	}
	out := make([]Stage, len(allowed))
	copy(out, allowed)
	return out
}
