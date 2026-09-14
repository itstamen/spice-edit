// =============================================================================
// File: internal/app/statusbar_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-09-14
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestStatusFileManagerLabel uses the host application's familiar name.
func TestStatusFileManagerLabel(t *testing.T) {
	for _, tt := range []struct {
		goos string
		want string
	}{
		{goos: "darwin", want: "Finder"},
		{goos: "windows", want: "Explorer"},
		{goos: "linux", want: "File manager"},
	} {
		if got := statusFileManagerLabel(tt.goos); got != tt.want {
			t.Fatalf("statusFileManagerLabel(%q) = %q, want %q", tt.goos, got, tt.want)
		}
	}
}

// TestDrawStatusBar_BranchFirstAndButtons checks both sides of the new layout.
func TestDrawStatusBar_BranchFirstAndButtons(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.gitBranch = "master"
	a.statusMsg = "Opened config_macos.py"
	a.statusUntil = time.Now().Add(time.Minute)
	a.drawStatusBar()
	a.screen.Show()

	_, sy, _, _ := a.statusRect()
	line := screenLine(a.screen.(tcell.SimulationScreen), sy)
	if got := line[:len("[master] Opened config_macos.py")]; got != "[master] Opened config_macos.py" {
		t.Fatalf("left status = %q", got)
	}
	for _, button := range a.statusBarButtons() {
		var rendered strings.Builder
		for x := button.X; x < button.X+button.Width; x++ {
			r, _, _, _ := a.screen.GetContent(x, sy)
			rendered.WriteRune(r)
		}
		if got := strings.TrimSpace(rendered.String()); got != button.Label {
			t.Fatalf("button at %d = %q, want %q", button.X, got, button.Label)
		}
		_, _, style, _ := a.screen.GetContent(button.X+1, sy)
		_, bg, _ := style.Decompose()
		if bg != a.theme.StatusBG {
			t.Fatalf("button %q background = %v, want status background %v", button.Label, bg, a.theme.StatusBG)
		}
		if button.X > 0 {
			r, _, dividerStyle, _ := a.screen.GetContent(button.X-statusButtonGap, sy)
			_, dividerBG, _ := dividerStyle.Decompose()
			if r != '│' || dividerBG != a.theme.StatusBG {
				t.Fatalf("button %q divider = %q on %v", button.Label, r, dividerBG)
			}
		}
	}
}

// TestStatusBarButtons_RightAlignedAndConstrained keeps Menu available on a
// narrow status bar and prevents action rectangles from overlapping.
func TestStatusBarButtons_RightAlignedAndConstrained(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	buttons := a.statusBarButtons()
	if len(buttons) != 4 || buttons[len(buttons)-2].Label != "Prefix" || buttons[len(buttons)-1].Label != "Menu" {
		t.Fatalf("buttons = %#v", buttons)
	}
	last := buttons[len(buttons)-1]
	if last.X+last.Width != a.width {
		t.Fatalf("right edge = %d, want %d", last.X+last.Width, a.width)
	}
	for i := 1; i < len(buttons); i++ {
		if buttons[i-1].X+buttons[i-1].Width >= buttons[i].X {
			t.Fatalf("buttons overlap: %#v", buttons)
		}
	}

	a.width = len(" Menu ")
	buttons = a.statusBarButtons()
	if len(buttons) != 1 || buttons[0].Label != "Menu" {
		t.Fatalf("narrow buttons = %#v, want Menu only", buttons)
	}
}

// TestStatusBarMouse_MenuActivatesOnMatchingRelease ensures held mouse motion
// cannot repeatedly fire a status action and releasing elsewhere cancels it.
func TestStatusBarMouse_MenuActivatesOnMatchingRelease(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	buttons := a.statusBarButtons()
	menu := buttons[len(buttons)-1]
	_, sy, _, _ := a.statusRect()

	a.handleMouse(tcell.NewEventMouse(menu.X+1, sy, tcell.Button1, tcell.ModNone))
	if a.menuOpen || a.dragMode != "status" {
		t.Fatalf("press menuOpen/drag = %v/%q", a.menuOpen, a.dragMode)
	}
	a.handleMouse(tcell.NewEventMouse(menu.X+1, sy, tcell.Button1, tcell.ModNone))
	if a.menuOpen {
		t.Fatal("held button activated Menu")
	}
	a.handleMouse(tcell.NewEventMouse(menu.X+1, sy, tcell.ButtonNone, tcell.ModNone))
	if !a.menuOpen || a.dragMode != "" {
		t.Fatalf("release menuOpen/drag = %v/%q", a.menuOpen, a.dragMode)
	}
}

// TestStatusBarMouse_PrefixTogglesSelectedFolder verifies the rendered Prefix
// button is wired to the sidebar setting rather than only presentational.
func TestStatusBarMouse_PrefixTogglesSelectedFolder(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "docs")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, "start.md")
	if err := os.WriteFile(file, []byte("guide"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, root)
	a.tree.SelectedPath = file

	var prefix statusBarButton
	for _, button := range a.statusBarButtons() {
		if button.Label == "Prefix" {
			prefix = button
			break
		}
	}
	if prefix.Width == 0 {
		t.Fatal("Prefix button is missing")
	}
	_, sy, _, _ := a.statusRect()
	a.handleMouse(tcell.NewEventMouse(prefix.X+1, sy, tcell.Button1, tcell.ModNone))
	a.handleMouse(tcell.NewEventMouse(prefix.X+1, sy, tcell.ButtonNone, tcell.ModNone))
	if got := string(a.sidebarPathPrefix); got != "docs/" {
		t.Fatalf("Prefix click set %q, want docs/", got)
	}
}

// TestStatusBarActions_ReuseLeaderTargets verifies Open and Finder dispatch
// through the same actions as Esc o and Esc e.
func TestStatusBarActions_ReuseLeaderTargets(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("open", func(t *testing.T) {
		a := newTestApp(t, root)
		a.openFile(target)
		called := make(chan string, 1)
		a.openPath = func(path string) error { called <- path; return nil }
		statusOpenWithDefaultApp(a)
		select {
		case got := <-called:
			if got != target {
				t.Fatalf("opened %q, want %q", got, target)
			}
		case <-time.After(time.Second):
			t.Fatal("Open.. did not invoke Esc o action")
		}
	})

	t.Run("file manager", func(t *testing.T) {
		a := newTestApp(t, root)
		called := make(chan string, 1)
		a.revealPath = func(path string, _ bool) error { called <- path; return nil }
		statusShowInFileManager(a)
		select {
		case got := <-called:
			if got != root {
				t.Fatalf("revealed %q, want %q", got, root)
			}
		case <-time.After(time.Second):
			t.Fatal("file-manager button did not invoke Esc e action")
		}
	})
}
