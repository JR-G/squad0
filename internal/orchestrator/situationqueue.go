package orchestrator

import (
	"fmt"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// Severity classifies how urgent a situation is. Determines the PM's
// response and whether the CEO gets notified.
type Severity string

const (
	// SeverityInfo is handled by the PM silently (nudge, merge, etc.).
	SeverityInfo Severity = "info"
	// SeverityWarning goes to triage with a recommended action.
	SeverityWarning Severity = "warning"
	// SeverityCritical goes to triage AND pings the CEO.
	SeverityCritical Severity = "critical"
)

// SituationType identifies what the sensor detected.
type SituationType string

// Situation types detected by Go sensors.
const (
	SitUnmergedApprovedPR SituationType = "unmerged_approved_pr"
	SitStaleWorkingAgent  SituationType = "stale_working_agent"
	SitOrphanedPR         SituationType = "orphaned_pr"
	SitAgedTriageItem     SituationType = "aged_triage_item"
	SitRepeatedFailure    SituationType = "repeated_failure"
	SitZombieCheckIn      SituationType = "zombie_checkin"
	SitPipelineDrift      SituationType = "pipeline_drift"
)

// Situation is a management issue detected by a Go sensor that needs
// the PM's judgment. The PM decides what to do; the Go code just
// detects and queues.
type Situation struct {
	Type        SituationType
	Severity    Severity
	Engineer    agent.Role
	Ticket      string
	PRURL       string
	Description string
	DetectedAt  time.Time
	Escalations int // how many times this has been re-escalated
}

// Key returns a dedup key for this situation. Same type + ticket + engineer
// means it's the same issue — don't queue it twice.
func (sit Situation) Key() string {
	return fmt.Sprintf("%s:%s:%s", sit.Type, sit.Ticket, sit.Engineer)
}

// SituationQueue collects situations from sensors for the PM to process.
// Thread-safe. Deduplicates by situation key.
type SituationQueue struct {
	mu       sync.Mutex
	items    []Situation
	seen     map[string]time.Time // key → last queued time
	resolved map[string]time.Time // key → resolved time (cooldown)
}

// NewSituationQueue creates an empty queue.
func NewSituationQueue() *SituationQueue {
	return &SituationQueue{
		seen:     make(map[string]time.Time),
		resolved: make(map[string]time.Time),
	}
}

// Push adds a situation to the queue. Deduplicates: if the same
// situation key was queued in the last 10 minutes, it's skipped.
// If it was resolved in the last 30 minutes, it's also skipped
// (cooldown prevents re-detection of handled situations).
func (queue *SituationQueue) Push(sit Situation) {
	queue.mu.Lock()
	defer queue.mu.Unlock()

	key := sit.Key()
	now := time.Now()

	// Cooldown — don't re-queue recently resolved situations.
	if resolved, ok := queue.resolved[key]; ok && now.Sub(resolved) < 30*time.Minute {
		return
	}

	// Dedup — don't queue the same situation twice within 10 minutes.
	if last, ok := queue.seen[key]; ok && now.Sub(last) < 10*time.Minute {
		return
	}

	sit.DetectedAt = now
	queue.items = append(queue.items, sit)
	queue.seen[key] = now
}

// Drain returns all queued situations and clears the queue.
// The PM processes the returned batch in a single session.
func (queue *SituationQueue) Drain() []Situation {
	queue.mu.Lock()
	defer queue.mu.Unlock()

	if len(queue.items) == 0 {
		return nil
	}

	items := queue.items
	queue.items = nil
	return items
}

// Resolve marks a situation key as handled. Prevents re-detection
// for the cooldown period.
func (queue *SituationQueue) Resolve(key string) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.resolved[key] = time.Now()
}

// Len returns the number of queued situations.
func (queue *SituationQueue) Len() int {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return len(queue.items)
}

// Escalate bumps the severity of a situation and re-queues it.
// Used when a situation has been in triage too long without action.
func (queue *SituationQueue) Escalate(sit Situation) {
	queue.mu.Lock()
	defer queue.mu.Unlock()

	sit.Escalations++
	sit.Severity = bumpSeverity(sit.Severity)
	sit.DetectedAt = time.Now()

	// Allow re-queue even if recently seen — escalation overrides dedup.
	key := sit.Key()
	queue.items = append(queue.items, sit)
	queue.seen[key] = time.Now()
}

func bumpSeverity(current Severity) Severity {
	switch current { //nolint:exhaustive // critical stays critical
	case SeverityInfo:
		return SeverityWarning
	case SeverityWarning:
		return SeverityCritical
	default:
		return SeverityCritical
	}
}

// FormatForPM formats all situations into a single prompt section for
// the PM to process. Groups by severity, highest first.
func FormatForPM(situations []Situation) string {
	if len(situations) == 0 {
		return ""
	}

	var critical, warning, info []Situation
	for _, sit := range situations {
		switch sit.Severity { //nolint:exhaustive // info is the default
		case SeverityCritical:
			critical = append(critical, sit)
		case SeverityWarning:
			warning = append(warning, sit)
		default:
			info = append(info, sit)
		}
	}

	result := "## Situations Requiring Your Attention\n\n"

	if len(critical) > 0 {
		result += "*CRITICAL — act immediately:*\n"
		for idx, sit := range critical {
			result += fmt.Sprintf("%d. [%s] %s\n", idx+1, sit.Type, sit.Description)
		}
		result += "\n"
	}

	if len(warning) > 0 {
		result += "*WARNING — needs action soon:*\n"
		for idx, sit := range warning {
			result += fmt.Sprintf("%d. [%s] %s\n", idx+1, sit.Type, sit.Description)
		}
		result += "\n"
	}

	if len(info) > 0 {
		result += "*INFO — handle if you can:*\n"
		for idx, sit := range info {
			result += fmt.Sprintf("%d. [%s] %s\n", idx+1, sit.Type, sit.Description)
		}
		result += "\n"
	}

	result += "For each situation, decide: nudge the engineer, reassign, escalate to triage, or ignore.\n"
	result += "Respond with a numbered action for each. Use names. Be direct.\n"

	return result
}
