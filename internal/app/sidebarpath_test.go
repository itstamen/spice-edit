// =============================================================================
// File: internal/app/sidebarpath_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-09-03
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestCurrentSidebarPath covers the relative/absolute display and prefix
// validation rules used by the sidebar path panel.
func TestCurrentSidebarPath(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "pkg", "file.go")
	a := newTestApp(t, root)
	a.tree.SelectedPath = child

	got, valid := a.currentSidebarPath()
	if got != filepath.Join("pkg", "file.go") || !valid {
		t.Fatalf("default relative = %q, %v", got, valid)
	}

	a.sidebarPathType = sidebarPathAbsolute
	got, valid = a.currentSidebarPath()
	if got != child || !valid {
		t.Fatalf("absolute = %q, %v", got, valid)
	}

	a.sidebarPathPrefix = []rune(root + string(filepath.Separator))
	got, valid = a.currentSidebarPath()
	if got != filepath.Join("pkg", "file.go") || !valid {
		t.Fatalf("stripped = %q, %v", got, valid)
	}

	a.sidebarPathPrefix = []rune("wrong/")
	got, valid = a.currentSidebarPath()
	if got != child || valid {
		t.Fatalf("invalid prefix = %q, %v", got, valid)
	}
}

// TestCurrentSidebarPath_EdgeCases pins down empty selections, the root, and
// prefixes which either leave the value unchanged or consume it completely.
func TestCurrentSidebarPath_EdgeCases(t *testing.T) {
	root := t.TempDir()
	a := newTestApp(t, root)
	a.tree.SelectedPath = ""

	if got, valid := a.currentSidebarPath(); got != "" || !valid {
		t.Fatalf("empty selection = %q, %v", got, valid)
	}

	a.tree.SelectedPath = root
	if got, valid := a.currentSidebarPath(); got != "." || !valid {
		t.Fatalf("root relative = %q, %v", got, valid)
	}

	a.sidebarPathPrefix = nil
	if got, valid := a.currentSidebarPath(); got != "." || !valid {
		t.Fatalf("empty prefix = %q, %v", got, valid)
	}

	a.sidebarPathPrefix = []rune(".")
	if got, valid := a.currentSidebarPath(); got != "" || !valid {
		t.Fatalf("whole prefix = %q, %v", got, valid)
	}
}

// TestSidebarPathRect locates the eight-row panel below the file tree and
// removes it whenever the sidebar cannot render a usable tree.
func TestSidebarPathRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	tx, ty, tw, th := a.sidebarRect()
	x, y, w, h := a.sidebarPathRect()
	if x != 0 || y != ty+th || w != a.sidebarWidth-1 || h != sidebarPathPanelHeight {
		t.Fatalf("panel rect = (%d,%d,%d,%d), tree = (%d,%d,%d,%d)", x, y, w, h, tx, ty, tw, th)
	}

	a.sidebarShown = false
	if x, y, w, h := a.sidebarPathRect(); x != 0 || y != 0 || w != 0 || h != 0 {
		t.Fatalf("hidden panel rect = (%d,%d,%d,%d)", x, y, w, h)
	}

	a.sidebarShown = true
	a.tree = nil
	if x, y, w, h := a.sidebarPathRect(); x != 0 || y != 0 || w != 0 || h != 0 {
		t.Fatalf("tree-less panel rect = (%d,%d,%d,%d)", x, y, w, h)
	}

	a.tree = newTestApp(t, t.TempDir()).tree
	a.height = sidebarPathPanelHeight
	if x, y, w, h := a.sidebarPathRect(); x != 0 || y != 0 || w != 0 || h != 0 {
		t.Fatalf("short panel rect = (%d,%d,%d,%d)", x, y, w, h)
	}
}

// TestDrawSidebarPath renders panel content only in its reserved bottom rows,
// marks invalid prefixes as errors, and never touches the clipboard.
func TestDrawSidebarPath(t *testing.T) {
	root := t.TempDir()
	a := newTestApp(t, root)
	a.tree.SelectedPath = filepath.Join(root, strings.Repeat("directory/", 12)+"file.go")
	copied := recordingClipboard(a, nil)
	a.draw()
	a.screen.Show()

	_, py, pw, ph := a.sidebarPathRect()
	panel := make([]string, 0, ph)
	for y := py; y < py+ph; y++ {
		panel = append(panel, screenLine(a.screen.(tcell.SimulationScreen), y)[:pw])
	}
	joined := strings.Join(panel, "\n")
	for _, want := range []string{"Path type", "(x) Relative  ( ) Absolute", "Strip prefix", "Current path"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("panel is missing %q:\n%s", want, joined)
		}
	}

	_, _, _, treeH := a.sidebarRect()
	for y := 0; y < treeH; y++ {
		if strings.Contains(screenLine(a.screen.(tcell.SimulationScreen), y)[:pw], "Path type") {
			t.Fatalf("panel label leaked into tree row %d", y)
		}
	}
	if strings.Contains(screenLine(a.screen.(tcell.SimulationScreen), a.height-1), "Path type") {
		t.Fatal("panel label leaked into status row")
	}

	for y := py + ph - 2; y < py+ph; y++ {
		if len([]rune(screenLine(a.screen.(tcell.SimulationScreen), y)[:pw])) != pw {
			t.Fatalf("path row %d exceeded panel width", y)
		}
	}
	if len(*copied) != 0 {
		t.Fatalf("draw copied %v", *copied)
	}

	a.sidebarPathPrefix = []rune("wrong/")
	a.draw()
	a.screen.Show()
	_, _, style, _ := a.screen.GetContent(1, py+4)
	fg, _, _ := style.Decompose()
	if fg != a.theme.Error {
		t.Fatalf("invalid input fg = %v, want %v", fg, a.theme.Error)
	}

	a.sidebarPathPrefix = nil
	a.draw()
	a.screen.Show()
	_, _, style, _ = a.screen.GetContent(1, py+4)
	fg, _, _ = style.Decompose()
	if fg == a.theme.Error {
		t.Fatalf("valid input fg = %v, do not want %v", fg, a.theme.Error)
	}
	if len(*copied) != 0 {
		t.Fatalf("repeated draw copied %v", *copied)
	}
}
