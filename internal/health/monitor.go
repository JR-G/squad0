package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// State represents an agent's health state.
type State string

const (
	// StateHealthy indicates the agent is working normally.
	StateHealthy State = "healthy"
	// StateSlow indicates the agent is taking longer than expected.
	StateSlow State = "slow"
	// StateStuck indicates the agent has made no progress for too long.
	StateStuck State = "stuck"
	// StateFailing indicates the agent has repeated errors.
	StateFailing State = "failing"
	// StateIdle indicates the agent has no current work.
	StateIdle State = "idle"
)

// AgentHealth holds the health metrics for a single agent.
type AgentHealth struct {
	Role           agent.Role
	State          State
	SessionStart   *time.Time
	ErrorCount     int
	LastError      string
	TicketFailures map[string]int
	LastActivity   time.Time
}

// Monitor tracks health metrics for all agents.
type Monitor struct {
	mu         sync.RWMutex
	agents     map[agent.Role]*AgentHealth
	maxIdle    time.Duration
	maxSession time.Duration
	maxErrors  int
}

// MonitorConfig holds configuration for the health monitor.
type MonitorConfig struct {
	MaxIdleTime          time.Duration
	MaxSessionTime       time.Duration
	MaxConsecutiveErrors int
}

// NewMonitor creates a Monitor for the given agent roles.
func NewMonitor(roles []agent.Role, cfg MonitorConfig) *Monitor {
	agents := make(map[agent.Role]*AgentHealth, len(roles))
	for _, role := range roles {
		agents[role] = &AgentHealth{
			Role:           role,
			State:          StateIdle,
			TicketFailures: make(map[string]int),
			LastActivity:   time.Now(),
		}
	}

	return &Monitor{
		agents:     agents,
		maxIdle:    cfg.MaxIdleTime,
		maxSession: cfg.MaxSessionTime,
		maxErrors:  cfg.MaxConsecutiveErrors,
	}
}

// RecordSessionStart marks an agent as having started a session.
func (mon *Monitor) RecordSessionStart(role agent.Role) {
	mon.mu.Lock()
	defer mon.mu.Unlock()

	health, ok := mon.agents[role]
	if !ok {
		return
	}

	now := time.Now()
	health.SessionStart = &now
	health.State = StateHealthy
	health.LastActivity = now
}

// RecordSessionEnd marks an agent's session as complete.
func (mon *Monitor) RecordSessionEnd(role agent.Role, ticket string, succeeded bool) {
	mon.mu.Lock()
	defer mon.mu.Unlock()

	health, ok := mon.agents[role]
	if !ok {
		return
	}

	health.SessionStart = nil
	health.LastActivity = time.Now()

	if succeeded {
		health.ErrorCount = 0
		health.State = StateIdle
		return
	}

	health.ErrorCount++
	health.TicketFailures[ticket]++

	if health.ErrorCount >= mon.maxErrors {
		health.State = StateFailing
		return
	}

	health.State = StateIdle
}

// RecordError records an error for the given agent.
func (mon *Monitor) RecordError(role agent.Role, errMsg string) {
	mon.mu.Lock()
	defer mon.mu.Unlock()

	health, ok := mon.agents[role]
	if !ok {
		return
	}

	health.LastError = errMsg
	health.ErrorCount++
	health.LastActivity = time.Now()

	if health.ErrorCount >= mon.maxErrors {
		health.State = StateFailing
	}
}

// Evaluate checks all agents and updates their health states based on
// current metrics and time thresholds.
func (mon *Monitor) Evaluate() {
	mon.mu.Lock()
	defer mon.mu.Unlock()

	now := time.Now()

	for _, health := range mon.agents {
		mon.evaluateAgent(health, now)
	}
}

// GetHealth returns the current health state for the given agent.
func (mon *Monitor) GetHealth(role agent.Role) (AgentHealth, error) {
	mon.mu.RLock()
	defer mon.mu.RUnlock()

	health, ok := mon.agents[role]
	if !ok {
		return AgentHealth{}, fmt.Errorf("unknown agent %s", role)
	}

	return *health, nil
}

// AllHealth returns the health state of every agent.
func (mon *Monitor) AllHealth(_ context.Context) []AgentHealth {
	mon.mu.RLock()
	defer mon.mu.RUnlock()

	result := make([]AgentHealth, 0, len(mon.agents))
	for _, health := range mon.agents {
		result = append(result, *health)
	}

	return result
}

// UnhealthyAgents returns agents that are not healthy or idle.
func (mon *Monitor) UnhealthyAgents() []AgentHealth {
	mon.mu.RLock()
	defer mon.mu.RUnlock()

	var unhealthy []AgentHealth
	for _, health := range mon.agents {
		switch health.State {
		case StateHealthy, StateIdle:
			continue
		case StateSlow, StateStuck, StateFailing:
			unhealthy = append(unhealthy, *health)
		}
	}

	return unhealthy
}

func (mon *Monitor) evaluateAgent(health *AgentHealth, now time.Time) {
	if health.State == StateFailing {
		return
	}

	if health.SessionStart == nil {
		health.State = StateIdle
		return
	}

	health.State = classifySessionDuration(now.Sub(*health.SessionStart), mon.maxSession)
}

func classifySessionDuration(elapsed, maxSession time.Duration) State {
	if elapsed > maxSession {
		return StateStuck
	}

	if elapsed > maxSession/2 {
		return StateSlow
	}

	return StateHealthy
}
