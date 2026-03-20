package tui_test

import (
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboard_Update_QuitOnQ(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()

	qMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	updated, cmd := dashboard.Update(qMsg)

	require.NotNil(t, cmd)
	finalModel, ok := updated.(tui.DashboardModel)
	require.True(t, ok)

	view := finalModel.View()
	assert.Contains(t, view, "Shutting down")
}

func TestDashboard_Update_QuitOnCtrlC(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()

	ctrlCMsg := tea.KeyMsg{Type: tea.KeyCtrlC}
	updated, cmd := dashboard.Update(ctrlCMsg)

	require.NotNil(t, cmd)
	finalModel, ok := updated.(tui.DashboardModel)
	require.True(t, ok)

	view := finalModel.View()
	assert.Contains(t, view, "Shutting down")
}

func TestDashboard_Update_TickUpdatesUptime(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()

	tickTime := time.Now().Add(5 * time.Minute)
	tickMsg := tui.NewTickMsg(tickTime)
	updated, cmd := dashboard.Update(tickMsg)

	require.NotNil(t, cmd)
	finalModel, ok := updated.(tui.DashboardModel)
	require.True(t, ok)

	view := finalModel.View()
	assert.Contains(t, view, "uptime")
}

func TestDashboard_Update_UnknownKeyIgnored(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()

	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	updated, cmd := dashboard.Update(keyMsg)

	assert.Nil(t, cmd)
	finalModel, ok := updated.(tui.DashboardModel)
	require.True(t, ok)

	view := finalModel.View()
	assert.Contains(t, view, "Squad0")
	assert.NotContains(t, view, "Shutting down")
}

func TestDashboard_Update_NonKeyMsgIgnored(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()

	windowMsg := tea.WindowSizeMsg{Width: 120, Height: 40}
	updated, cmd := dashboard.Update(windowMsg)

	assert.Nil(t, cmd)
	finalModel, ok := updated.(tui.DashboardModel)
	require.True(t, ok)
	assert.NotEmpty(t, finalModel.View())
}

func TestDashboard_View_Quitting_ShowsShutdown(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()

	qMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	updated, _ := dashboard.Update(qMsg)

	finalModel, ok := updated.(tui.DashboardModel)
	require.True(t, ok)

	view := finalModel.View()
	assert.Contains(t, view, "Shutting down")
	assert.NotContains(t, view, "q to quit")
}

func TestDashboard_View_NotQuitting_ShowsQuitHint(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	view := dashboard.View()

	assert.Contains(t, view, "q to quit")
}

func TestDashboard_View_WithLogLines_ShowsRecentActivity(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	dashboard.AddLogLine("first log entry")
	dashboard.AddLogLine("second log entry")

	view := dashboard.View()

	assert.Contains(t, view, "Recent Activity")
	assert.Contains(t, view, "first log entry")
	assert.Contains(t, view, "second log entry")
}

func TestDashboard_View_NoLogLines_SkipsRecentActivity(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	view := dashboard.View()

	assert.NotContains(t, view, "Recent Activity")
}

func TestDashboard_View_NoCheckIns_SkipsAgentStatus(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	view := dashboard.View()

	assert.NotContains(t, view, "Agent Status")
}

func TestDashboard_UpdateCheckIns_ReflectsInView(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	assert.NotContains(t, dashboard.View(), "Agent Status")

	dashboard.UpdateCheckIns(nil)
	assert.NotContains(t, dashboard.View(), "Agent Status")
}

func TestDashboard_UpdateHealth_DoesNotPanic(t *testing.T) {
	t.Parallel()

	dashboard := tui.NewDashboard()
	assert.NotPanics(t, func() {
		dashboard.UpdateHealth(nil)
	})
}
