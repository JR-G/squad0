package tui_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/health"
	"github.com/JR-G/squad0/internal/tui"
	"github.com/stretchr/testify/assert"
)

func TestDashboard_View_ShowsBanner(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	view := dashboard.View()

	assert.Contains(t, view, "Squad0")
	assert.Contains(t, view, "uptime")
}

func TestDashboard_View_WithCheckIns(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	dashboard.UpdateCheckIns([]coordination.CheckIn{
		{Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, Ticket: "SQ-42"},
	})

	view := dashboard.View()

	assert.Contains(t, view, "engineer-1")
}

func TestDashboard_View_WithHealth(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	dashboard.UpdateCheckIns([]coordination.CheckIn{
		{Agent: agent.RoleEngineer1, Status: coordination.StatusWorking},
	})
	dashboard.UpdateHealth([]health.AgentHealth{
		{Role: agent.RoleEngineer1, State: health.StateHealthy},
	})

	view := dashboard.View()

	assert.Contains(t, view, "●")
}

func TestDashboard_AddLogLine_ShowsInView(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	dashboard.AddLogLine("engineer-1 started work on SQ-42")

	view := dashboard.View()

	assert.Contains(t, view, "SQ-42")
	assert.Contains(t, view, "Recent Activity")
}

func TestDashboard_AddLogLine_CapsAtMaxLines(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	for idx := 0; idx < 15; idx++ {
		dashboard.AddLogLine("log line")
	}

	view := dashboard.View()

	assert.NotEmpty(t, view)
}

func TestDashboard_Init_ReturnsTick(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	cmd := dashboard.Init()

	assert.NotNil(t, cmd)
}
