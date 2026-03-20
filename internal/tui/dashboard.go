package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/health"
	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg time.Time

// DashboardModel is a bubbletea model for the live orchestrator dashboard.
type DashboardModel struct {
	checkIns     []coordination.CheckIn
	healthStates []health.AgentHealth
	uptime       time.Duration
	startTime    time.Time
	logLines     []string
	quitting     bool
}

// NewDashboard creates a new dashboard model.
func NewDashboard() DashboardModel {
	return DashboardModel{
		startTime: time.Now(),
		logLines:  make([]string, 0, 20),
	}
}

// Init starts the tick timer.
func (model DashboardModel) Init() tea.Cmd {
	return tickCmd()
}

// Update handles events and refreshes the display.
func (model DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return model.handleKey(msg)
	case tickMsg:
		model.uptime = time.Since(model.startTime)
		return model, tickCmd()
	default:
		return model, nil
	}
}

func (model DashboardModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		model.quitting = true
		return model, tea.Quit
	default:
		return model, nil
	}
}

// View renders the dashboard.
func (model DashboardModel) View() string {
	if model.quitting {
		return "\n" + Muted.Render("Shutting down...") + "\n"
	}

	var builder strings.Builder

	builder.WriteString(Banner())
	builder.WriteString(renderUptime(model.uptime))
	builder.WriteString("\n")

	if len(model.checkIns) > 0 {
		builder.WriteString(FormatAgentStatus(model.checkIns, model.healthStates))
	}

	if len(model.logLines) > 0 {
		builder.WriteString(renderRecentLogs(model.logLines))
	}

	builder.WriteString("\n")
	builder.WriteString(Muted.Render("  q to quit"))
	builder.WriteString("\n")

	return builder.String()
}

// UpdateCheckIns refreshes the agent status data.
func (model *DashboardModel) UpdateCheckIns(checkIns []coordination.CheckIn) {
	model.checkIns = checkIns
}

// UpdateHealth refreshes the health data.
func (model *DashboardModel) UpdateHealth(states []health.AgentHealth) {
	model.healthStates = states
}

// AddLogLine adds a log entry to the recent logs display.
func (model *DashboardModel) AddLogLine(line string) {
	maxLines := 10
	model.logLines = append(model.logLines, line)
	if len(model.logLines) > maxLines {
		model.logLines = model.logLines[len(model.logLines)-maxLines:]
	}
}

func renderUptime(uptime time.Duration) string {
	hours := int(uptime.Hours())
	mins := int(uptime.Minutes()) % 60
	secs := int(uptime.Seconds()) % 60

	return fmt.Sprintf("  %s %02d:%02d:%02d\n",
		Muted.Render("uptime"),
		hours, mins, secs,
	)
}

func renderRecentLogs(lines []string) string {
	var builder strings.Builder

	builder.WriteString(Section("Recent Activity"))
	builder.WriteString("\n")

	for _, line := range lines {
		builder.WriteString(fmt.Sprintf("  %s\n", Muted.Render(line)))
	}

	return builder.String()
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(tickTime time.Time) tea.Msg {
		return tickMsg(tickTime)
	})
}
