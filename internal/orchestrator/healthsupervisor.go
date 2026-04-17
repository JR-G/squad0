package orchestrator

import (
	"log"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
)

// HealthSupervisor wraps the health.Monitor with the orchestrator-
// specific behaviour: nil-safe forwarding (so Orchestrator code
// doesn't need scattered `if orch.monitor == nil` checks) and
// engineer-eligibility filtering based on per-role health state.
//
// Second slice of the orchestrator god-object split. Pure forward —
// no state of its own beyond the embedded monitor pointer.
type HealthSupervisor struct {
	monitor *health.Monitor
}

// NewHealthSupervisor wraps a Monitor. A nil monitor produces a
// supervisor that no-ops every record call and treats every role
// as healthy — matches the original conditional behaviour without
// scattering nil checks across callers.
func NewHealthSupervisor(monitor *health.Monitor) *HealthSupervisor {
	return &HealthSupervisor{monitor: monitor}
}

// Monitor returns the underlying monitor (may be nil) for callers
// that need to wire the alerter or query health states directly.
func (sup *HealthSupervisor) Monitor() *health.Monitor {
	return sup.monitor
}

// RecordSessionStart marks the start of a session for the role. No-op
// when no monitor is configured.
func (sup *HealthSupervisor) RecordSessionStart(role agent.Role) {
	if sup == nil || sup.monitor == nil {
		return
	}
	sup.monitor.RecordSessionStart(role)
}

// RecordSessionEnd marks the end of a session and its outcome. No-op
// when no monitor is configured.
func (sup *HealthSupervisor) RecordSessionEnd(role agent.Role, ticket string, success bool) {
	if sup == nil || sup.monitor == nil {
		return
	}
	sup.monitor.RecordSessionEnd(role, ticket, success)
}

// FilterHealthyEngineers returns the subset of engineers that are
// not in a failing health state. When no monitor is configured every
// engineer passes through unchanged.
func (sup *HealthSupervisor) FilterHealthyEngineers(roles []agent.Role) []agent.Role {
	engineers := filterEngineers(roles)

	if sup == nil || sup.monitor == nil {
		return engineers
	}

	healthy := make([]agent.Role, 0, len(engineers))
	for _, role := range engineers {
		agentHealth, err := sup.monitor.GetHealth(role)
		if err != nil {
			healthy = append(healthy, role)
			continue
		}
		if agentHealth.State == health.StateFailing {
			log.Printf("tick: skipping %s — health state is failing", role)
			continue
		}
		healthy = append(healthy, role)
	}

	return healthy
}
