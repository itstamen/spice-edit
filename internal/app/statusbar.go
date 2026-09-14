// =============================================================================
// File: internal/app/statusbar.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-09-14
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"runtime"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/termtext"
)

const statusButtonGap = 1

// statusBarButton is one rendered and clickable action at the right edge of
// the status bar. X and Width are absolute terminal-cell coordinates.
type statusBarButton struct {
	Label  string
	X      int
	Width  int
	Action func(*App)
}

// statusFileManagerLabel uses the operating system's familiar application
// name while keeping a clear generic label on other desktop platforms.
func statusFileManagerLabel(goos string) string {
	switch goos {
	case "darwin":
		return "Finder"
	case "windows":
		return "Explorer"
	default:
		return "File manager"
	}
}

// statusBarButtons returns the action buttons which fit at the right edge.
// Layout starts with Menu so the primary navigation escape hatch survives on
// constrained terminals, then adds the other actions when room permits.
func (a *App) statusBarButtons() []statusBarButton {
	defs := []statusBarButton{
		{Label: "Open..", Action: statusOpenWithDefaultApp},
		{Label: statusFileManagerLabel(runtime.GOOS), Action: statusShowInFileManager},
		{Label: "Prefix", Action: statusToggleCurrentPrefix},
		{Label: "Menu", Action: statusOpenMenu},
	}

	right := a.width
	buttons := make([]statusBarButton, 0, len(defs))
	for i := len(defs) - 1; i >= 0; i-- {
		button := defs[i]
		button.Width = termtext.StringWidth(button.Label) + 2
		gap := 0
		if len(buttons) > 0 {
			gap = statusButtonGap
		}
		if button.Width+gap > right {
			continue
		}
		right -= button.Width + gap
		button.X = right
		buttons = append([]statusBarButton{button}, buttons...)
	}
	return buttons
}

// drawStatusBarButtons keeps every idle control inside the continuous status
// rail. Thin dividers define the hit areas; only the actively pressed button
// inverts, so controls never blend into the editor background above them.
func (a *App) drawStatusBarButtons(buttons []statusBarButton) {
	_, sy, _, _ := a.statusRect()
	for i, button := range buttons {
		dividerX := button.X - statusButtonGap
		if dividerX >= 0 {
			dividerStyle := tcell.StyleDefault.Background(a.theme.StatusBG).Foreground(a.theme.Subtle)
			a.screen.SetContent(dividerX, sy, '│', nil, dividerStyle)
		}

		style := tcell.StyleDefault.Background(a.theme.StatusBG).Foreground(a.theme.BG).Bold(true)
		if a.dragMode == "status" && a.statusButtonPressed == i+1 {
			style = tcell.StyleDefault.Background(a.theme.BG).Foreground(a.theme.StatusBG).Bold(true)
		}
		for x := button.X; x < button.X+button.Width; x++ {
			a.screen.SetContent(x, sy, ' ', nil, style)
		}
		drawAtWidth(a.screen, button.X+1, sy, button.Width-2, button.Label, style)
	}
}

// statusBarButtonAt returns the current button index under a screen point.
func (a *App) statusBarButtonAt(x, y int) int {
	_, sy, _, _ := a.statusRect()
	if y != sy {
		return -1
	}
	for i, button := range a.statusBarButtons() {
		if x >= button.X && x < button.X+button.Width {
			return i
		}
	}
	return -1
}

// activateStatusBarButton invokes a button only when a press is released over
// the same action, matching normal button behavior and preventing drag repeats.
func (a *App) activateStatusBarButton(x, y, pressed int) {
	buttons := a.statusBarButtons()
	index := a.statusBarButtonAt(x, y)
	if pressed <= 0 || index != pressed-1 || index >= len(buttons) {
		return
	}
	buttons[index].Action(a)
}

// statusOpenWithDefaultApp shares the exact action mapped to Esc o.
func statusOpenWithDefaultApp(a *App) {
	if action := leaderActionFor('o'); action != nil {
		action(a)
	}
}

// statusShowInFileManager shares the exact action mapped to Esc e.
func statusShowInFileManager(a *App) {
	if action := leaderActionFor('e'); action != nil {
		action(a)
	}
}

// statusToggleCurrentPrefix switches Strip prefix for the selected tree node.
func statusToggleCurrentPrefix(a *App) {
	a.toggleCurrentSidebarPrefix()
}

// statusOpenMenu shares the action reached by pressing Esc twice.
func statusOpenMenu(a *App) {
	a.openMenu()
}
