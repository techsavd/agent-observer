package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/techsavd/agent-observer/core/schema"
)

func TestDashboardRendersActiveBatchAndHidesInactive(t *testing.T) {
	model := testModel()
	model.width = 140
	model.height = 36
	output := model.View()
	if !strings.Contains(output, "1 Runs") || !strings.Contains(output, "CUR") || !strings.Contains(output, "active") {
		t.Fatalf("expected dashboard batch output, got:\n%s", output)
	}
	if strings.Contains(output, "inactive run:0") || strings.Contains(output, "Inactive task") {
		t.Fatalf("expected inactive batch hidden by default, got:\n%s", output)
	}
}

func TestDashboardCanShowInactive(t *testing.T) {
	model := testModel()
	model.width = 140
	model.height = 36
	model.showInactive = true
	output := model.View()
	if !strings.Contains(output, "inactive") {
		t.Fatalf("expected inactive batch when toggled, got:\n%s", output)
	}
}

func TestDashboardRendersShellPane(t *testing.T) {
	model := testModel()
	model.width = 140
	model.height = 36
	model.focus = "shell"
	if err := model.runs.add(&ptySession{
		id:       "run-1",
		provider: providerShell,
		argv:     []string{"/bin/sh"},
		cwd:      "/tmp/project",
		screen:   newScreen(40, 12),
		raw:      []string{"$ pwd", "/tmp/project"},
	}); err != nil {
		t.Fatal(err)
	}
	output := model.View()
	if !strings.Contains(output, "shell://local") || !strings.Contains(output, "/tmp/project") {
		t.Fatalf("expected shell pane, got:\n%s", output)
	}
}

func TestDashboardShellControlsHiddenByDefault(t *testing.T) {
	model := testModel()
	model.width = 140
	model.height = 36
	output := model.View()
	if strings.Contains(output, "s shell") || strings.Contains(output, "ctrl+o detach shell") {
		t.Fatalf("expected shell controls hidden, got:\n%s", output)
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if cmd != nil {
		t.Fatalf("expected no shell command when disabled")
	}
	if updated.(Model).focus == "shell" {
		t.Fatalf("expected focus to stay off shell")
	}
}

func TestDashboardCanEnableShell(t *testing.T) {
	model := testModel().WithShellEnabled(true)
	model.width = 140
	model.height = 36
	output := model.View()
	if !strings.Contains(output, "s shell") || !strings.Contains(output, "ctrl+o detach shell") {
		t.Fatalf("expected shell controls when enabled, got:\n%s", output)
	}
}

func TestDashboardCanSetRefreshInterval(t *testing.T) {
	model := testModel().WithRefreshInterval(2 * time.Second)
	if model.refreshEvery != 2*time.Second {
		t.Fatalf("expected refresh interval to be set, got %s", model.refreshEvery)
	}
}

func TestDashboardCompactSizesRenderCorePanes(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{80, 24},
		{100, 30},
		{120, 36},
		{160, 48},
	} {
		model := testModel()
		model.width = size.width
		model.height = size.height
		output := model.View()
		if !strings.Contains(output, "1 Runs") || !strings.Contains(output, "2 Tasks") || !strings.Contains(output, "4 Health") {
			t.Fatalf("expected core panes at %dx%d, got:\n%s", size.width, size.height, output)
		}
	}
}

func TestDashboardRendersDetailsAndActionFooter(t *testing.T) {
	model := testModel()
	model.width = 140
	model.height = 36
	output := model.View()
	for _, expected := range []string{"3 Details", "/ filter", "1 runs", "2 tasks", "3 details", "4 Health"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q in production dashboard output, got:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "window://") || strings.Contains(output, "current-run://") {
		t.Fatalf("expected product panel labels instead of debug URI chrome, got:\n%s", output)
	}
}

func TestDashboardMouseSelectsRunAndTask(t *testing.T) {
	model := testModel()
	model.width = 140
	model.height = 36
	model.showInactive = true

	layout := model.dashboardLayout()
	runRect := layout.panels[panelRuns]
	updated, _ := model.Update(tea.MouseMsg{
		X:      runRect.x + 3,
		Y:      runRect.y + 2 + runsStaticRows + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	model = updated.(Model)
	if model.focus != panelRuns || model.selected != 1 {
		t.Fatalf("expected mouse to select second run, focus=%s selected=%d", model.focus, model.selected)
	}

	model.selected = 0
	model.taskSelected = 0
	layout = model.dashboardLayout()
	taskRect := layout.panels[panelTasks]
	updated, _ = model.Update(tea.MouseMsg{
		X:      taskRect.x + 3,
		Y:      taskRect.y + 2 + tasksStaticRows + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	model = updated.(Model)
	if model.focus != panelTasks || model.taskSelected != 1 {
		t.Fatalf("expected mouse to select second task, focus=%s taskSelected=%d", model.focus, model.taskSelected)
	}
}

func TestDashboardMouseWheelScrollsDetails(t *testing.T) {
	model := testModel()
	model.width = 140
	model.height = 20
	model.focus = panelDetails
	layout := model.dashboardLayout()
	detailRect := layout.panels[panelDetails]
	updated, _ := model.Update(tea.MouseMsg{
		X:      detailRect.x + 3,
		Y:      detailRect.y + 3,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	model = updated.(Model)
	if model.focus != panelDetails || model.detailOffset == 0 {
		t.Fatalf("expected details panel to scroll on wheel, focus=%s offset=%d", model.focus, model.detailOffset)
	}
}

func TestDashboardFilterModeFiltersTasks(t *testing.T) {
	model := testModel()
	model.width = 140
	model.height = 36
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Review")})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)

	if model.filter != "Review" || model.filtering {
		t.Fatalf("expected applied Review filter, filter=%q filtering=%t", model.filter, model.filtering)
	}
	tasks := model.tasks("active")
	if len(tasks) != 1 || tasks[0].Title != "Review scanner warnings" {
		t.Fatalf("expected filtered review task, got %#v", tasks)
	}
}

func TestDashboardDirectFocusAndHelp(t *testing.T) {
	model := testModel()
	model.width = 140
	model.height = 36
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	model = updated.(Model)
	if model.focus != panelDetails {
		t.Fatalf("expected direct focus to details, got %s", model.focus)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	model = updated.(Model)
	if !strings.Contains(model.View(), "Mouse: click focuses/selects rows") {
		t.Fatalf("expected help overlay to document mouse support, got:\n%s", model.View())
	}
}

func TestDashboardFooterClickTogglesInactive(t *testing.T) {
	model := testModel()
	model.width = 140
	model.height = 36
	_, actions := model.footerTextAndActions()
	var inactiveX int
	for _, action := range actions {
		if action.kind == "inactive" {
			inactiveX = action.start
			break
		}
	}
	updated, _ := model.Update(tea.MouseMsg{
		X:      inactiveX,
		Y:      model.dashboardLayout().footerY,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	model = updated.(Model)
	if !model.showInactive {
		t.Fatalf("expected footer click to toggle inactive")
	}
}

func TestDashboardMouseFocusesVisibleShell(t *testing.T) {
	model := testModel().WithShellEnabled(true)
	model.width = 140
	model.height = 36
	if err := model.runs.add(&ptySession{
		id:       "run-1",
		provider: providerShell,
		argv:     []string{"/bin/sh"},
		cwd:      "/tmp/project",
		screen:   newScreen(40, 12),
		raw:      []string{"$ pwd", "/tmp/project"},
	}); err != nil {
		t.Fatal(err)
	}
	model.focus = panelRuns
	layout := model.dashboardLayout()
	shellRect := layout.panels[panelShell]
	updated, _ := model.Update(tea.MouseMsg{
		X:      shellRect.x + 3,
		Y:      shellRect.y + 3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	model = updated.(Model)
	if model.focus != panelShell {
		t.Fatalf("expected mouse click to focus visible shell, got %s", model.focus)
	}
}

func TestDashboardRespectsNoColor(t *testing.T) {
	original := os.Getenv("NO_COLOR")
	t.Setenv("NO_COLOR", "1")
	t.Cleanup(func() { _ = os.Setenv("NO_COLOR", original) })
	model := testModel()
	model.width = 140
	model.height = 36
	output := model.View()
	if strings.Contains(output, "\x1b[") {
		t.Fatalf("expected NO_COLOR output to omit ANSI styling, got:\n%s", output)
	}
}

func TestWatchModeEmptyStateGuidesUser(t *testing.T) {
	model := NewWatch(schema.WorldSnapshot{Tasks: map[string]schema.TaskSnapshot{}, Batches: map[string]schema.BatchSnapshot{}}, false, nil)
	model.width = 140
	model.height = 36
	output := model.View()
	if !strings.Contains(output, "mode:watch") || !strings.Contains(output, "Start an agent session now.") {
		t.Fatalf("expected watch guidance, got:\n%s", output)
	}
}

func TestDashboardRendersSessionsInRunsPanel(t *testing.T) {
	model := testModelWithSessions()
	model.width = 140
	model.height = 36
	output := model.View()
	if !strings.Contains(output, "claude") || !strings.Contains(output, "BUSY") {
		t.Fatalf("expected busy claude session row, got:\n%s", output)
	}
	if strings.Contains(output, "old-session") {
		t.Fatalf("expected done session hidden by default, got:\n%s", output)
	}
}

func TestDashboardSessionSelectionShowsDetail(t *testing.T) {
	model := testModelWithSessions()
	model.width = 140
	model.height = 36
	// Sessions sort before batches, so the first run row is the busy session.
	model.selected = 0
	output := model.View()
	if !strings.Contains(output, "claude-fable-5") || !strings.Contains(output, "Fixing the login page") {
		t.Fatalf("expected session detail with model and last text, got:\n%s", output)
	}
	if !strings.Contains(output, "resume   available") {
		t.Fatalf("expected resumable marker, got:\n%s", output)
	}
}

func TestDashboardFilterMatchesSessions(t *testing.T) {
	model := testModelWithSessions()
	model.filter = "login"
	sessions := model.sessions()
	if len(sessions) != 1 || sessions[0].ID != "sess-busy" {
		t.Fatalf("expected filter to match session last text, got %#v", sessions)
	}
}

func testModelWithSessions() Model {
	model := testModel()
	now := time.Now()
	model.world.Sessions = map[string]schema.SessionSnapshot{
		"claude:sess-busy": {
			ID: "sess-busy", Provider: "claude", Title: "login-fix", CWD: "/tmp/demo",
			Status: schema.SessionBusy, Model: "claude-fable-5", Turns: 4,
			LastText: "Fixing the login page now.", Resumable: true,
			Tokens: &schema.TokenUsage{Input: 100, Output: 20},
			PID:    4242, LastUpdated: now,
		},
		"claude:old-session": {
			ID: "old-session", Provider: "claude", CWD: "/tmp/old",
			Status: schema.SessionDone, LastUpdated: now.Add(-24 * time.Hour),
		},
	}
	return model
}

func testModel() Model {
	now := time.Now()
	world := schema.WorldSnapshot{
		Tasks: map[string]schema.TaskSnapshot{
			"active:1":   {ID: "active:1", BatchID: "active", Title: "Active task", Description: strings.Repeat("Implementation detail with enough text to wrap across the details panel. ", 8), ActiveForm: "Implement dashboard cockpit", Status: schema.StatusRunning, Role: schema.RoleCoding, ActiveFiles: []schema.ActiveFile{{Path: "internal/tui/model.go"}, {Path: "internal/tui/model_test.go"}, {Path: "README.md"}}, SourcePath: "/tmp/tasks/active/1.json", LastUpdated: now},
			"active:2":   {ID: "active:2", BatchID: "active", Title: "Review scanner warnings", ActiveForm: "Review scanner warning handling", Status: schema.StatusWaiting, Role: schema.RoleReviewer, SourcePath: "/tmp/tasks/active/2.json", LastUpdated: now.Add(-time.Minute)},
			"inactive:1": {ID: "inactive:1", BatchID: "inactive", Title: "Inactive task", Status: schema.StatusCompleted, LastUpdated: now.Add(-time.Hour)},
		},
		Batches: map[string]schema.BatchSnapshot{
			"active":   {BatchID: "active", TaskIDs: []string{"active:1", "active:2"}, Counts: schema.BatchCounts{Running: 1, Waiting: 1}, LastUpdated: now},
			"inactive": {BatchID: "inactive", TaskIDs: []string{"inactive:1"}, Counts: schema.BatchCounts{Completed: 1}, LastUpdated: now.Add(-time.Hour)},
		},
		Warnings: []schema.WarningSnapshot{
			{SourcePath: "/tmp/tasks/malformed/1.json", Message: "unexpected EOF"},
		},
	}
	return New(world, false, nil)
}
