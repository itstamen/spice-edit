// =============================================================================
// File: internal/app/sidebarpath_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-09-03
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/termtext"
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

// TestSidebarPathCopy_EventScoped keeps clipboard writes tied to explicit
// tree and panel actions, never rendering or cursor movement.
func TestSidebarPathCopy_EventScoped(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.tree.SelectedPath = a.rootDir
	copied := recordingClipboard(a, nil)

	a.copyCurrentSidebarPath(false)
	a.copyCurrentSidebarPath(false)
	a.copyCurrentSidebarPath(true)
	if got := *copied; len(got) != 2 || got[0] != "." || got[1] != "." {
		t.Fatalf("copies = %#v, want two forced/event changes", got)
	}
	path := filepath.Join(a.rootDir, "opened.txt")
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(path)
	if got := len(*copied); got != 2 {
		t.Fatalf("tab synchronization copied %d times, want 2", got)
	}
	if a.clipBuf != "" {
		t.Fatalf("path copy changed editor clipboard to %q", a.clipBuf)
	}

	a.tree.SelectedPath = a.rootDir
	a.copyToSystem = func(string) error { return errors.New("clipboard down") }
	a.sidebarPathLastCopied = "previous"
	a.sidebarPathPrefix = []rune(".")
	a.copyCurrentSidebarPath(false)
	if a.sidebarPathLastCopied != "previous" {
		t.Fatalf("failed copy changed last copied to %q", a.sidebarPathLastCopied)
	}
	if got := a.statusMsg; got != "Copy failed: clipboard down" {
		t.Fatalf("failure flash = %q", got)
	}
	a.copyToSystem = func(string) error { return nil }
	a.copyCurrentSidebarPath(false)
	if a.sidebarPathLastCopied != "" {
		t.Fatalf("later retry last copied = %q, want empty path", a.sidebarPathLastCopied)
	}
}

// TestSidebarPathCopy_EmptyValueTracksSuccessfulWrites keeps a successful
// empty path distinct from the initial state so prefix "." can clear the
// system clipboard exactly once.
func TestSidebarPathCopy_EmptyValueTracksSuccessfulWrites(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.tree.SelectedPath = a.rootDir
	a.sidebarPathPrefix = []rune(".")
	copied := recordingClipboard(a, nil)

	a.copyCurrentSidebarPath(false)
	a.copyCurrentSidebarPath(false)
	if got := *copied; len(got) != 1 || got[0] != "" {
		t.Fatalf("empty path copies = %#v, want one empty write", got)
	}
	if !a.sidebarPathHasCopied {
		t.Fatal("successful empty copy was not recorded")
	}

	a.copyToSystem = func(string) error { return errors.New("clipboard down") }
	a.sidebarPathHasCopied = false
	a.copyCurrentSidebarPath(false)
	if a.sidebarPathHasCopied {
		t.Fatal("failed empty copy was recorded as successful")
	}
}

// TestSidebarPathMouse controls path mode and the rune-indexed prefix field
// without allowing panel clicks to reach tree context handling.
func TestSidebarPathMouse_RadioInputAndContextBoundary(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.tree.SelectedPath = a.rootDir
	copied := recordingClipboard(a, nil)
	x, y, _, _ := a.sidebarPathRect()

	if !a.handleSidebarPathMouse(x+2, y+2) || a.sidebarPathType != sidebarPathRelative {
		t.Fatal("relative radio was not handled")
	}
	if len(*copied) != 0 {
		t.Fatalf("unchanged mode copied %#v", *copied)
	}
	if !a.handleSidebarPathMouse(x+16, y+2) || a.sidebarPathType != sidebarPathAbsolute {
		t.Fatal("absolute radio was not handled")
	}
	if got := *copied; len(got) != 1 || got[0] != a.rootDir {
		t.Fatalf("mode copy = %#v, want absolute root", got)
	}

	a.sidebarPathPrefix = []rune("a中b")
	if !a.handleSidebarPathMouse(x+3, y+4) || !a.sidebarPathPrefixFocused {
		t.Fatal("prefix input was not focused")
	}
	if a.sidebarPathPrefixCursor != 1 {
		t.Fatalf("CJK cursor = %d, want start rune 1", a.sidebarPathPrefixCursor)
	}
	if !a.handleSidebarPathMouse(x+2, y+6) || a.sidebarPathPrefixFocused {
		t.Fatal("non-input panel click did not blur")
	}

	a.handleMouse(tcell.NewEventMouse(x+2, y+6, tcell.Button3, tcell.ModNone))
	if a.contextOpen || a.menuOpen {
		t.Fatal("panel right click opened a context or main menu")
	}
}

// TestSidebarPathMouse_RightClickAwayBlursBeforeKeyboardRouting verifies each
// right-click route outside PATH INFO gives its menu ownership of later keys.
func TestSidebarPathMouse_RightClickAwayBlursBeforeKeyboardRouting(t *testing.T) {
	t.Run("tree context", func(t *testing.T) {
		a := newTestApp(t, t.TempDir())
		a.sidebarPathPrefix = []rune("keep")
		a.sidebarPathPrefixFocused = true
		copied := recordingClipboard(a, nil)
		a.draw()

		a.handleMouse(tcell.NewEventMouse(1, 1, tcell.Button3, tcell.ModNone))
		copyCount := len(*copied)
		a.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))

		if a.sidebarPathPrefixFocused {
			t.Fatal("tree context did not blur PATH INFO input")
		}
		if !a.contextOpen || a.contextHover != 1 {
			t.Fatalf("contextOpen=%v hover=%d, want open at row 1", a.contextOpen, a.contextHover)
		}
		if got := string(a.sidebarPathPrefix); got != "keep" {
			t.Fatalf("prefix = %q, want unchanged", got)
		}
		if got := len(*copied); got != copyCount {
			t.Fatalf("keyboard copied %d paths, want %d", got, copyCount)
		}
	})

	t.Run("tab context", func(t *testing.T) {
		a, _ := seedCloseTabs(t, "a.txt", "b.txt")
		a.sidebarPathPrefix = []rune("keep")
		a.sidebarPathPrefixFocused = true
		copied := recordingClipboard(a, nil)
		a.draw()
		r := a.lastTabRects[0]

		a.handleMouse(tcell.NewEventMouse(r.X+1, 0, tcell.Button3, tcell.ModNone))
		copyCount := len(*copied)
		a.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))

		if a.sidebarPathPrefixFocused {
			t.Fatal("tab context did not blur PATH INFO input")
		}
		if !a.contextOpen || a.contextHover != 1 {
			t.Fatalf("contextOpen=%v hover=%d, want open at row 1", a.contextOpen, a.contextHover)
		}
		if got := string(a.sidebarPathPrefix); got != "keep" {
			t.Fatalf("prefix = %q, want unchanged", got)
		}
		if got := len(*copied); got != copyCount {
			t.Fatalf("keyboard copied %d paths, want %d", got, copyCount)
		}
	})

	t.Run("editor main menu", func(t *testing.T) {
		a, _ := seedCloseTabs(t, "a.txt", "b.txt")
		a.sidebarPathPrefix = []rune("keep")
		a.sidebarPathPrefixFocused = true
		copied := recordingClipboard(a, nil)
		a.draw()
		ex, ey, _, _ := a.editorRect()

		a.handleMouse(tcell.NewEventMouse(ex+2, ey+2, tcell.Button3, tcell.ModNone))
		copyCount := len(*copied)
		initialHover := a.hoveredMenuRow
		a.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))

		if a.sidebarPathPrefixFocused {
			t.Fatal("main menu did not blur PATH INFO input")
		}
		if !a.menuOpen || a.hoveredMenuRow == initialHover {
			t.Fatalf("menuOpen=%v hover=%d, want keyboard navigation from %d", a.menuOpen, a.hoveredMenuRow, initialHover)
		}
		if got := string(a.sidebarPathPrefix); got != "keep" {
			t.Fatalf("prefix = %q, want unchanged", got)
		}
		if got := len(*copied); got != copyCount {
			t.Fatalf("keyboard copied %d paths, want %d", got, copyCount)
		}
	})
}

// TestSidebarPathMouse_TreePressConsumesMotion ensures a held tree press is
// one gesture: motion cannot copy, retarget, or toggle nodes a second time.
func TestSidebarPathMouse_TreePressConsumesMotion(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a-dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, root)
	copied := recordingClipboard(a, nil)
	a.draw()

	press := tcell.NewEventMouse(1, 2, tcell.Button1, tcell.ModNone)
	a.handleMouse(press)
	if a.dragMode != "tree" {
		t.Fatalf("tree press dragMode = %q, want tree", a.dragMode)
	}
	if got := a.tree.SelectedPath; got != dir {
		t.Fatalf("selected after press = %q, want %q", got, dir)
	}
	if !a.tree.Root.Children[0].Expanded {
		t.Fatal("directory did not expand on initial press")
	}

	a.handleMouse(tcell.NewEventMouse(1, 2, tcell.Button1, tcell.ModNone))
	a.handleMouse(tcell.NewEventMouse(1, 3, tcell.Button1, tcell.ModNone))
	if got := len(*copied); got != 1 {
		t.Fatalf("held motion copied %d paths, want 1", got)
	}
	if got := a.tree.SelectedPath; got != dir {
		t.Fatalf("held motion selected %q, want %q", got, dir)
	}
	if !a.tree.Root.Children[0].Expanded {
		t.Fatal("held motion collapsed directory")
	}

	a.handleMouse(tcell.NewEventMouse(1, 3, tcell.ButtonNone, tcell.ModNone))
	if a.dragMode != "" {
		t.Fatalf("release dragMode = %q, want cleared", a.dragMode)
	}
	a.handleMouse(press)
	if got := len(*copied); got != 2 {
		t.Fatalf("second press copies = %d, want 2", got)
	}
	if a.tree.Root.Children[0].Expanded {
		t.Fatal("second independent press did not collapse directory")
	}
}

// TestSidebarPathKeyAndPaste captures editing, focus termination, and paste
// routing while ensuring all focused keys stay out of the active buffer.
func TestSidebarPathKeyAndPaste(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, root)
	a.openFile(path)
	a.tree.SelectedPath = path
	a.sidebarPathPrefixFocused = true
	a.sidebarPathPrefix = []rune("中a")
	a.sidebarPathPrefixCursor = len(a.sidebarPathPrefix)
	copied := recordingClipboard(a, nil)

	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	if a.sidebarPathPrefixCursor != 1 || len(*copied) != 0 {
		t.Fatalf("left cursor/copies = %d/%d", a.sidebarPathPrefixCursor, len(*copied))
	}
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if a.sidebarPathPrefixCursor != 2 || len(*copied) != 0 {
		t.Fatalf("right cursor/copies = %d/%d", a.sidebarPathPrefixCursor, len(*copied))
	}
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyRune, 0x01, tcell.ModNone))
	if got := string(a.sidebarPathPrefix); got != "中a" || len(*copied) != 0 {
		t.Fatalf("control rune changed prefix/copies to %q/%d", got, len(*copied))
	}
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	if got := string(a.sidebarPathPrefix); got != "中xa" {
		t.Fatalf("rune edit = %q", got)
	}
	if len(*copied) != 1 {
		t.Fatalf("invalid prefix edit copies = %d, want 1", len(*copied))
	}
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone))
	if len(*copied) != 1 {
		t.Fatalf("unchanged derived value copied %d times, want 1", len(*copied))
	}
	*copied = nil
	a.sidebarPathLastCopied = ""
	a.sidebarPathPrefix = nil
	a.sidebarPathPrefixCursor = 0
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone))
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone))
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyRune, 'o', tcell.ModNone))
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone))
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone))
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone))
	if got := string(a.sidebarPathPrefix); got != "" {
		t.Fatalf("edit keys = %q, want empty", got)
	}
	if len(*copied) != 4 {
		t.Fatalf("content edits copied %d times, want 4", len(*copied))
	}

	a.lastEscape = time.Now()
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	if a.sidebarPathPrefixFocused || !a.lastEscape.IsZero() {
		t.Fatal("Esc did not blur and clear leader state")
	}
	a.sidebarPathPrefixFocused = true
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	a.sidebarPathPrefixFocused = true
	a.handleSidebarPathKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if a.sidebarPathPrefixFocused || len(*copied) != 4 {
		t.Fatal("blur keys changed focus or copied")
	}

	a.sidebarPathPrefixFocused = true
	a.sidebarPathPrefix = []rune("pre")
	a.sidebarPathPrefixCursor = len(a.sidebarPathPrefix)
	pasteEventText(a, "one", "two")
	if got := string(a.sidebarPathPrefix); got != "preone two" {
		t.Fatalf("pasted prefix = %q", got)
	}
	if got := a.activeTabPtr().Buffer.String(); got != "original" {
		t.Fatalf("focused path edit changed buffer to %q", got)
	}

	a.handleKey(tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone))
	if got := string(a.sidebarPathPrefix); got != "preone twoz" {
		t.Fatalf("handleKey did not route focused rune: %q", got)
	}
	if got := a.activeTabPtr().Buffer.String(); got != "original" {
		t.Fatalf("handleKey edited buffer to %q", got)
	}
}

// TestSidebarPathWheel confines vertical tree scrolling to the tree rectangle.
func TestSidebarPathWheel_ConfinesTreeScrolling(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(root, "file"+string(rune('a'+i))+".txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newTestApp(t, root)
	activePath := filepath.Join(root, "active.txt")
	if err := os.WriteFile(activePath, []byte(strings.Repeat("line\n", 80)), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(activePath)
	a.draw()
	_, py, _, _ := a.sidebarPathRect()
	tab := a.activeTabPtr()
	if tab == nil {
		t.Fatal("expected active text tab")
	}
	tabScroll := tab.ScrollY
	a.scrollAt(1, py, wheelLines)
	if a.tree.ScrollY != 0 {
		t.Fatalf("panel wheel scrolled tree to %d", a.tree.ScrollY)
	}
	if tab.ScrollY != tabScroll {
		t.Fatalf("panel wheel scrolled editor to %d, want %d", tab.ScrollY, tabScroll)
	}
	a.scrollAt(1, 2, wheelLines)
	if a.tree.ScrollY != wheelLines {
		t.Fatalf("tree wheel ScrollY = %d, want %d", a.tree.ScrollY, wheelLines)
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
	for _, want := range []string{"PATH INFO", "Path type", "(x) Relative  ( ) Absolute", "Strip prefix", "Current path"} {
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

// TestDrawSidebarPath_FocusedInputOwnsFinalCursor verifies editor rendering
// cannot replace the terminal caret shown by the focused prefix input.
func TestDrawSidebarPath_FocusedInputOwnsFinalCursor(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, root)
	a.openFile(path)
	a.sidebarPathPrefix = []rune("pre")
	a.sidebarPathPrefixCursor = 2
	a.sidebarPathPrefixFocused = true

	a.draw()
	a.screen.Show()
	inputX, inputY, _ := a.sidebarPathInputRect()
	wantX := inputX + termtext.VisualCol(string(a.sidebarPathPrefix), a.sidebarPathPrefixCursor)
	gotX, gotY, visible := a.screen.(tcell.SimulationScreen).GetCursor()
	if !visible || gotX != wantX || gotY != inputY {
		t.Fatalf("cursor = (%d,%d,%v), want (%d,%d,true)", gotX, gotY, visible, wantX, inputY)
	}
}

// TestDrawSidebarPath_OverlaysRetainCursorPrecedence verifies later focus
// layers still win even if stale PATH INFO focus state is present.
func TestDrawSidebarPath_OverlaysRetainCursorPrecedence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("find", func(t *testing.T) {
		a := newTestApp(t, root)
		a.openFile(path)
		a.openFind()
		a.sidebarPathPrefixFocused = true
		a.draw()
		a.screen.Show()

		barX, barY, _, _ := a.findBarRect()
		wantX := barX + textWidth(" Find: ")
		gotX, gotY, visible := a.screen.(tcell.SimulationScreen).GetCursor()
		if !visible || gotX != wantX || gotY != barY {
			t.Fatalf("find cursor = (%d,%d,%v), want (%d,%d,true)", gotX, gotY, visible, wantX, barY)
		}
	})

	t.Run("menu", func(t *testing.T) {
		a := newTestApp(t, root)
		a.openFile(path)
		a.openMenu()
		a.sidebarPathPrefixFocused = true
		a.draw()
		a.screen.Show()

		if x, y, visible := a.screen.(tcell.SimulationScreen).GetCursor(); visible {
			t.Fatalf("menu left cursor visible at (%d,%d)", x, y)
		}
	})
}

// TestDrawSidebarPath_CompactRadioLayout keeps both path type choices visible
// at the minimum supported sidebar width while retaining the eight-row panel.
func TestDrawSidebarPath_CompactRadioLayout(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.sidebarWidth = minSidebarWidth
	a.draw()
	a.screen.Show()

	_, py, pw, _ := a.sidebarPathRect()
	if pw != minSidebarWidth-1 {
		t.Fatalf("panel width = %d, want %d", pw, minSidebarWidth-1)
	}
	panel := make([]string, 0, sidebarPathPanelHeight)
	for y := py; y < py+sidebarPathPanelHeight; y++ {
		panel = append(panel, screenLine(a.screen.(tcell.SimulationScreen), y)[:pw])
	}
	joined := strings.Join(panel, "\n")
	for _, want := range []string{"PATH INFO", "(x) Relative", "( ) Absolute"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compact panel is missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(panel[2], "Absolute") {
		t.Fatalf("absolute option should use its own compact row:\n%s", joined)
	}
}

// TestDrawSidebarPathTail_CJKCombiningStaysInBounds verifies the tail helper
// keeps wide and combining clusters inside its two-row drawing rectangle.
func TestDrawSidebarPathTail_CJKCombiningStaysInBounds(t *testing.T) {
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("screen init: %v", err)
	}
	defer scr.Fini()
	scr.SetSize(8, 2)
	style := tcell.StyleDefault
	for y := 0; y < 2; y++ {
		scr.SetContent(6, y, '!', nil, style)
	}

	drawSidebarPathTail(scr, 0, 0, 6, "prefix/中文/e\u0301", style)
	scr.Show()
	for y := 0; y < 2; y++ {
		got, _, _ := scr.Get(6, y)
		if got != "!" {
			t.Fatalf("tail overwrote boundary at row %d: got %q", y, got)
		}
	}
	for y := 0; y < 2; y++ {
		line := screenLine(scr, y)[:6]
		if termtext.StringWidth(line) > 6 {
			t.Fatalf("tail row %d width = %d, want <= 6", y, termtext.StringWidth(line))
		}
	}
}

// TestDrawSidebarPathTail_OddWidthWideSuffixUsesBothRows verifies suffix
// selection accounts for the unused cell left by wide glyphs on odd rows.
func TestDrawSidebarPathTail_OddWidthWideSuffixUsesBothRows(t *testing.T) {
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("screen init: %v", err)
	}
	defer scr.Fini()
	scr.SetSize(17, 2)
	style := tcell.StyleDefault
	for y := 0; y < 2; y++ {
		scr.SetContent(15, y, '!', nil, style)
	}

	drawSidebarPathTail(scr, 0, 0, 15, "一二三四五六七八九十甲乙丙丁戊", style)
	scr.Show()
	first, _, _ := scr.Get(0, 0)
	last, _, _ := scr.Get(12, 1)
	if first != "二" || last != "戊" {
		t.Fatalf("drawn suffix starts/ends %q/%q, want %q/%q", first, last, "二", "戊")
	}
	for y := 0; y < 2; y++ {
		got, _, _ := scr.Get(15, y)
		if got != "!" {
			t.Fatalf("tail overwrote boundary at row %d: got %q", y, got)
		}
	}
}

// TestDrawSidebarPathTail_OversizedClusterIsSkipped verifies a grapheme wider
// than a whole row cannot escape the drawing rectangle or hide a valid tail.
func TestDrawSidebarPathTail_OversizedClusterIsSkipped(t *testing.T) {
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("screen init: %v", err)
	}
	defer scr.Fini()
	scr.SetSize(2, 2)
	style := tcell.StyleDefault
	for y := 0; y < 2; y++ {
		scr.SetContent(1, y, '!', nil, style)
	}

	drawSidebarPathTail(scr, 0, 0, 1, "中a", style)
	scr.Show()
	got, _, _ := scr.Get(0, 0)
	if got != "a" {
		t.Fatalf("tail after oversized cluster = %q, want %q", got, "a")
	}
	for y := 0; y < 2; y++ {
		boundary, _, _ := scr.Get(1, y)
		if boundary != "!" {
			t.Fatalf("oversized cluster overwrote boundary at row %d: got %q", y, boundary)
		}
	}
}
