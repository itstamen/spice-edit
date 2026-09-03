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
	if got != "pkg/file.go" || !valid {
		t.Fatalf("default relative = %q, %v", got, valid)
	}

	a.sidebarPathType = sidebarPathAbsolute
	got, valid = a.currentSidebarPath()
	if got != filepath.ToSlash(child) || !valid {
		t.Fatalf("absolute = %q, %v", got, valid)
	}

	a.sidebarPathPrefix = []rune(filepath.ToSlash(root) + "/")
	got, valid = a.currentSidebarPath()
	if got != "pkg/file.go" || !valid {
		t.Fatalf("stripped = %q, %v", got, valid)
	}

	a.sidebarPathPrefix = []rune("wrong/")
	got, valid = a.currentSidebarPath()
	if got != filepath.ToSlash(child) || valid {
		t.Fatalf("invalid prefix = %q, %v", got, valid)
	}
}

// TestNormalizeSidebarPathValue keeps panel and clipboard values portable
// across native path separators without changing filesystem operation paths.
func TestNormalizeSidebarPathValue(t *testing.T) {
	for _, tt := range []struct {
		name      string
		path      string
		separator rune
		want      string
	}{
		{name: "windows relative", path: `docs\superpowers\spec.md`, separator: '\\', want: "docs/superpowers/spec.md"},
		{name: "windows absolute", path: `C:\repo\docs\spec.md`, separator: '\\', want: "C:/repo/docs/spec.md"},
		{name: "unix literal backslash", path: `docs\literal\name`, separator: '/', want: `docs\literal\name`},
		{name: "forward slashes", path: "/repo/docs/spec.md", separator: '/', want: "/repo/docs/spec.md"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSidebarPathValue(tt.path, tt.separator); got != tt.want {
				t.Fatalf("normalizeSidebarPathValue(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
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
	if got := *copied; len(got) != 1 || got[0] != filepath.ToSlash(a.rootDir) {
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

// TestSidebarPathPrefixPaste keeps trailing terminal line endings out of the
// visible prefix while preserving valid spaces and collapsing internal lines.
func TestSidebarPathPrefixPaste(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
		want string
	}{
		{name: "trailing CRLF", text: "docs/\r\n", want: "docs/"},
		{name: "trailing LF", text: "dir with spaces/\n", want: "dir with spaces/"},
		{name: "internal LF", text: "a\nb\n", want: "a b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSidebarPathPrefixPaste(tt.text); got != tt.want {
				t.Fatalf("normalized = %q, want %q", got, tt.want)
			}
		})
	}

	root := t.TempDir()
	selected := filepath.Join(root, "docs", "superpowers", "spec.md")
	if err := os.MkdirAll(filepath.Dir(selected), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selected, []byte("spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, root)
	a.tree.SelectedPath = selected
	a.sidebarPathPrefixFocused = true
	a.sidebarPathPrefixCursor = 0
	copied := recordingClipboard(a, nil)

	a.commandPasteRecent = func(time.Time) bool { return true }
	a.handleEvent(tcell.NewEventPaste(true))
	for _, r := range "docs/" {
		a.handleEvent(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	a.handleEvent(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyCtrlJ, 0, tcell.ModNone))
	a.handleEvent(tcell.NewEventPaste(false))

	if got := string(a.sidebarPathPrefix); got != "docs/" {
		t.Fatalf("prefix = %q, want docs/", got)
	}
	if got, valid := a.currentSidebarPath(); got != "superpowers/spec.md" || !valid {
		t.Fatalf("current path = %q, %v, want superpowers/spec.md, true", got, valid)
	}
	if got := *copied; len(got) != 1 || got[0] != "superpowers/spec.md" {
		t.Fatalf("copies = %#v, want one suffix copy", got)
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

// TestSidebarPathCollapseGeometry keeps the tree and path panel derived from
// one dynamic height so collapsing returns exactly seven rows to the tree.
func TestSidebarPathCollapseGeometry(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	_, _, _, expandedTreeH := a.sidebarRect()
	_, expandedY, expandedW, expandedH := a.sidebarPathRect()
	if expandedH != sidebarPathExpandedHeight {
		t.Fatalf("expanded panel height = %d, want %d", expandedH, sidebarPathExpandedHeight)
	}

	a.sidebarPathCollapsed = true
	_, _, _, collapsedTreeH := a.sidebarRect()
	_, collapsedY, collapsedW, collapsedH := a.sidebarPathRect()
	if collapsedH != sidebarPathCollapsedHeight {
		t.Fatalf("collapsed panel height = %d, want %d", collapsedH, sidebarPathCollapsedHeight)
	}
	if collapsedTreeH != expandedTreeH+sidebarPathExpandedHeight-sidebarPathCollapsedHeight {
		t.Fatalf("collapsed tree height = %d, expanded = %d", collapsedTreeH, expandedTreeH)
	}
	if collapsedY != expandedY+sidebarPathExpandedHeight-sidebarPathCollapsedHeight || collapsedW != expandedW {
		t.Fatalf("collapsed panel = (%d,%d), expanded = (%d,%d)", collapsedY, collapsedW, expandedY, expandedW)
	}

	inputX, inputY, inputW := a.sidebarPathInputRect()
	if inputX != 0 || inputY != 0 || inputW != 0 {
		t.Fatalf("collapsed input rect = (%d,%d,%d), want zero", inputX, inputY, inputW)
	}
}

// TestSidebarPathCollapseDrawing verifies each header advertises its action
// and a collapsed panel renders no interactive body rows.
func TestSidebarPathCollapseDrawing(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.draw()
	a.screen.Show()
	_, expandedY, expandedW, _ := a.sidebarPathRect()
	expanded := screenLine(a.screen.(tcell.SimulationScreen), expandedY)[:expandedW]
	if !strings.Contains(expanded, "PATH INFO ▲") {
		t.Fatalf("expanded header = %q, want PATH INFO ▲", expanded)
	}

	a.sidebarPathCollapsed = true
	a.draw()
	a.screen.Show()
	_, collapsedY, collapsedW, collapsedH := a.sidebarPathRect()
	if collapsedH != 1 {
		t.Fatalf("collapsed height = %d, want 1", collapsedH)
	}
	collapsed := screenLine(a.screen.(tcell.SimulationScreen), collapsedY)[:collapsedW]
	if !strings.Contains(collapsed, "PATH INFO ▼") {
		t.Fatalf("collapsed header = %q, want PATH INFO ▼", collapsed)
	}
	for _, label := range []string{"Path type", "Relative", "Absolute", "Strip prefix", "Current path"} {
		if strings.Contains(collapsed, label) {
			t.Fatalf("collapsed header contains body label %q: %q", label, collapsed)
		}
	}
}

// TestSidebarPathHeaderCollapseGesture treats a header press as one gesture
// even though the tree grows beneath the held pointer after the toggle.
func TestSidebarPathHeaderCollapseGesture(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.spiceConfigPath = filepath.Join(t.TempDir(), "config.json")
	a.sidebarPathPrefixFocused = true
	a.sidebarPathPrefixScroll = 4
	a.tree.SelectedPath = a.rootDir
	copied := recordingClipboard(a, nil)
	_, headerY, _, _ := a.sidebarPathRect()

	a.handleMouse(tcell.NewEventMouse(2, headerY, tcell.Button1, tcell.ModNone))
	if !a.sidebarPathCollapsed || a.dragMode != "path" {
		t.Fatalf("collapse state/drag = %v/%q, want true/path", a.sidebarPathCollapsed, a.dragMode)
	}
	if a.sidebarPathPrefixFocused || a.sidebarPathPrefixScroll != 0 {
		t.Fatalf("collapse focus/scroll = %v/%d, want false/0", a.sidebarPathPrefixFocused, a.sidebarPathPrefixScroll)
	}
	if got := len(*copied); got != 0 {
		t.Fatalf("header toggle copied %d paths, want 0", got)
	}

	// The old header row is now a tree row. A held Button1 motion must not
	// route into that newly exposed row or persist/toggle a second time.
	a.handleMouse(tcell.NewEventMouse(2, headerY, tcell.Button1, tcell.ModNone))
	if !a.sidebarPathCollapsed || a.dragMode != "path" {
		t.Fatalf("held motion state/drag = %v/%q, want true/path", a.sidebarPathCollapsed, a.dragMode)
	}
	a.handleMouse(tcell.NewEventMouse(2, headerY, tcell.ButtonNone, tcell.ModNone))
	if a.dragMode != "" {
		t.Fatalf("release dragMode = %q, want cleared", a.dragMode)
	}

	data, err := os.ReadFile(a.spiceConfigPath)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	if !strings.Contains(string(data), `"sidebar_path_collapsed": true`) {
		t.Fatalf("collapsed state was not persisted: %s", data)
	}

	_, headerY, _, _ = a.sidebarPathRect()
	a.handleMouse(tcell.NewEventMouse(2, headerY, tcell.Button1, tcell.ModNone))
	if a.sidebarPathCollapsed || a.dragMode != "path" {
		t.Fatalf("independent expand state/drag = %v/%q, want false/path", a.sidebarPathCollapsed, a.dragMode)
	}
	a.handleMouse(tcell.NewEventMouse(2, headerY, tcell.ButtonNone, tcell.ModNone))
	data, err = os.ReadFile(a.spiceConfigPath)
	if err != nil {
		t.Fatalf("read expanded config: %v", err)
	}
	if !strings.Contains(string(data), `"sidebar_path_collapsed": false`) {
		t.Fatalf("expanded state was not persisted: %s", data)
	}
}

// TestSidebarPathHeaderGestureConsumesCombinedWheelMasks keeps a backend's
// Button1+wheel event inside the panel gesture after collapse moves the tree.
func TestSidebarPathHeaderGestureConsumesCombinedWheelMasks(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(root, "file"+string(rune('a'+i))+".txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "active.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("a very long editor line ", 20)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, root)
	a.openFile(path)
	_, headerY, _, _ := a.sidebarPathRect()
	a.handleMouse(tcell.NewEventMouse(2, headerY, tcell.Button1, tcell.ModNone))
	if !a.sidebarPathCollapsed || a.dragMode != "path" {
		t.Fatalf("collapse state/drag = %v/%q, want true/path", a.sidebarPathCollapsed, a.dragMode)
	}

	// The pre-collapse header row is now in the expanded tree rectangle.
	a.handleMouse(tcell.NewEventMouse(1, headerY, tcell.Button1|tcell.WheelDown, tcell.ModNone))
	if a.tree.ScrollY != 0 {
		t.Fatalf("combined vertical wheel scrolled tree to %d", a.tree.ScrollY)
	}
	ex, ey, _, _ := a.editorRect()
	tab := a.activeTabPtr()
	if tab == nil {
		t.Fatal("expected active text tab")
	}
	a.handleMouse(tcell.NewEventMouse(ex+1, ey+1, tcell.Button1|tcell.WheelRight, tcell.ModNone))
	if tab.ScrollX != 0 {
		t.Fatalf("combined horizontal wheel scrolled editor to %d", tab.ScrollX)
	}

	a.handleMouse(tcell.NewEventMouse(ex+1, ey+1, tcell.ButtonNone, tcell.ModNone))
	if a.dragMode != "" {
		t.Fatalf("release dragMode = %q, want cleared", a.dragMode)
	}
	_, collapsedHeaderY, _, _ := a.sidebarPathRect()
	a.handleMouse(tcell.NewEventMouse(2, collapsedHeaderY, tcell.Button1, tcell.ModNone))
	if a.sidebarPathCollapsed {
		t.Fatal("independent header press did not expand panel")
	}
	a.handleMouse(tcell.NewEventMouse(2, collapsedHeaderY, tcell.ButtonNone, tcell.ModNone))
	if a.dragMode != "" {
		t.Fatalf("independent release dragMode = %q, want cleared", a.dragMode)
	}
}

// TestSidebarPathCollapsePersistenceFailure keeps the visible state useful
// when a malformed config cannot be updated, while reporting the failure.
func TestSidebarPathCollapsePersistenceFailure(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.spiceConfigPath = filepath.Join(t.TempDir(), "config.json")
	const malformed = `{"sidebar_path_collapsed":`
	if err := os.WriteFile(a.spiceConfigPath, []byte(malformed), 0o600); err != nil {
		t.Fatalf("seed malformed config: %v", err)
	}
	_, headerY, _, _ := a.sidebarPathRect()
	a.handleMouse(tcell.NewEventMouse(2, headerY, tcell.Button1, tcell.ModNone))
	if !a.sidebarPathCollapsed {
		t.Fatal("persistence failure reverted the in-memory collapsed state")
	}
	if !strings.HasPrefix(a.statusMsg, "config:") {
		t.Fatalf("status = %q, want config error", a.statusMsg)
	}
	data, err := os.ReadFile(a.spiceConfigPath)
	if err != nil {
		t.Fatalf("read malformed config: %v", err)
	}
	if string(data) != malformed {
		t.Fatalf("malformed config was overwritten: %q", data)
	}
}

// TestSidebarPathCollapsedInputIsolation keeps the single-row header from
// becoming a tree context, wheel, radio, or prefix-input target.
func TestSidebarPathCollapsedInputIsolation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(root, "file"+string(rune('a'+i))+".txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newTestApp(t, root)
	path := filepath.Join(root, "active.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("line\n", 80)), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(path)
	a.sidebarPathCollapsed = true
	a.draw()
	_, headerY, _, _ := a.sidebarPathRect()
	tab := a.activeTabPtr()
	if tab == nil {
		t.Fatal("expected active text tab")
	}

	a.handleMouse(tcell.NewEventMouse(1, headerY, tcell.Button3, tcell.ModNone))
	a.handleMouse(tcell.NewEventMouse(1, headerY, tcell.WheelDown, tcell.ModNone))
	a.handleMouse(tcell.NewEventMouse(1, headerY, tcell.WheelRight, tcell.ModNone))
	if a.contextOpen || a.menuOpen {
		t.Fatal("collapsed header opened a context or main menu")
	}
	if a.tree.ScrollY != 0 || tab.ScrollY != 0 || tab.ScrollX != 0 {
		t.Fatalf("collapsed header scrolled tree/editor to %d/%d/%d", a.tree.ScrollY, tab.ScrollY, tab.ScrollX)
	}
	if a.handleSidebarPathMouse(2, headerY+1) || a.sidebarPathPrefixFocused {
		t.Fatal("collapsed body remained a path panel input target")
	}
}

// TestSidebarPathCollapseRestoresEditorCursor verifies hiding a focused
// prefix field leaves the final terminal cursor with the active editor.
func TestSidebarPathCollapseRestoresEditorCursor(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, root)
	a.openFile(path)
	a.draw()
	a.screen.Show()
	baselineX, baselineY, baselineVisible := a.screen.(tcell.SimulationScreen).GetCursor()
	if !baselineVisible {
		t.Fatal("expected editor cursor before focusing PATH INFO")
	}
	a.sidebarPathPrefixFocused = true
	_, headerY, _, _ := a.sidebarPathRect()
	a.handleMouse(tcell.NewEventMouse(2, headerY, tcell.Button1, tcell.ModNone))
	a.handleMouse(tcell.NewEventMouse(2, headerY, tcell.ButtonNone, tcell.ModNone))
	a.draw()
	a.screen.Show()
	gotX, gotY, visible := a.screen.(tcell.SimulationScreen).GetCursor()
	if !visible || gotX != baselineX || gotY != baselineY {
		t.Fatalf("cursor = (%d,%d,%v), want editor (%d,%d,true)", gotX, gotY, visible, baselineX, baselineY)
	}
}

// TestSidebarPathCollapseUnavailableDoesNotPersist keeps missing panel
// geometry from making a hidden, single-file, or tiny-layout click durable.
func TestSidebarPathCollapseUnavailableDoesNotPersist(t *testing.T) {
	for _, setup := range []struct {
		name  string
		apply func(*App)
	}{
		{name: "hidden", apply: func(a *App) { a.sidebarShown = false }},
		{name: "tree-less", apply: func(a *App) { a.tree = nil }},
		{name: "tiny", apply: func(a *App) { a.height = sidebarPathExpandedHeight }},
	} {
		t.Run(setup.name, func(t *testing.T) {
			a := newTestApp(t, t.TempDir())
			a.spiceConfigPath = filepath.Join(t.TempDir(), "config.json")
			setup.apply(a)
			if x, y, w, h := a.sidebarPathRect(); x != 0 || y != 0 || w != 0 || h != 0 || a.sidebarPathHeight() != 0 {
				t.Fatalf("unavailable panel = (%d,%d,%d,%d), height=%d", x, y, w, h, a.sidebarPathHeight())
			}
			a.handleMouse(tcell.NewEventMouse(1, 0, tcell.Button1, tcell.ModNone))
			if _, err := os.Stat(a.spiceConfigPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unavailable panel persisted config: %v", err)
			}
		})
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
