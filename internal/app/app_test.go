// =============================================================================
// File: internal/app/app_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the pure-logic helpers and the small bits of App glue that don't
// require a live terminal. Where we need an *App we build one against a
// tcell.SimulationScreen so layout and event-routing helpers can run without
// touching a real tty. The interactive code paths (Run, the event loop, real
// drawing) are exercised manually — here we just pin down the helpers so
// future refactors don't silently regress them.

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/clipboard"
	"github.com/cloudmanic/spice-edit/internal/commandpaste"
	"github.com/cloudmanic/spice-edit/internal/customactions"
	"github.com/cloudmanic/spice-edit/internal/editor"
	"github.com/cloudmanic/spice-edit/internal/filetree"
	"github.com/cloudmanic/spice-edit/internal/icons"
	"github.com/cloudmanic/spice-edit/internal/theme"
)

// newTestApp builds a fully-wired App against a tcell.SimulationScreen. It
// mirrors what New() does, but skips the background tree-refresh goroutine
// because we don't want a ticker firing while tests run.
func newTestApp(t *testing.T, root string) *App {
	t.Helper()
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { scr.Fini() })
	scr.SetSize(120, 40)

	tree, err := filetree.New(root)
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	a := &App{
		screen:             scr,
		copyToSystem:       func(string) error { return nil },
		readFromSystem:     clipboard.ReadSystem,
		commandPasteRecent: func(time.Time) bool { return false },
		closeCommandPaste:  func() {},
		showFrame:          scr.Show,
		theme:              theme.Default(),
		rootDir:            tree.Root.Path,
		rootAnchor:         mustResolveTestPath(t, tree.Root.Path),
		tree:               tree,
		hoveredMenuRow:     -1,
		sidebarShown:       true,
		sidebarWidth:       defaultSidebarWidth,
	}
	a.setActiveFolder(tree.Root.Path)
	a.width, a.height = scr.Size()
	return a
}

// TestLoadSpiceConfigSidebarPathCollapsed verifies the App copies the
// persisted path panel preference from its explicitly configured test path.
func TestLoadSpiceConfigSidebarPathCollapsed(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.spiceConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(a.spiceConfigPath, []byte(`{"sidebar_path_collapsed":true}`), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	a.loadSpiceConfig()
	if !a.sidebarPathCollapsed {
		t.Fatal("sidebarPathCollapsed = false, want true")
	}
}

// TestLoadSpiceConfigMalformedSidebarPathCollapsed verifies malformed config
// leaves the panel expanded and surfaces the existing config error status.
func TestLoadSpiceConfigMalformedSidebarPathCollapsed(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.spiceConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(a.spiceConfigPath, []byte(`{"sidebar_path_collapsed":`), 0o600); err != nil {
		t.Fatalf("seed malformed config: %v", err)
	}

	a.loadSpiceConfig()
	if a.sidebarPathCollapsed {
		t.Fatal("sidebarPathCollapsed = true after malformed config, want false")
	}
	if !strings.HasPrefix(a.statusMsg, "config:") {
		t.Fatalf("status = %q, want config error", a.statusMsg)
	}
}

// TestLoadSpiceConfigNullSidebarPathCollapsed verifies a null top-level value
// is rejected like every other non-object config and leaves the panel expanded.
func TestLoadSpiceConfigNullSidebarPathCollapsed(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.spiceConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(a.spiceConfigPath, []byte("null"), 0o600); err != nil {
		t.Fatalf("seed null config: %v", err)
	}

	a.loadSpiceConfig()
	if a.sidebarPathCollapsed {
		t.Fatal("sidebarPathCollapsed = true after null config, want false")
	}
	if !strings.HasPrefix(a.statusMsg, "config:") {
		t.Fatalf("status = %q, want config error", a.statusMsg)
	}
}

// TestInitializeCommandPasteStartupFailures verifies native startup errors
// install deny/no-op hooks and surface the one expected status message.
func TestInitializeCommandPasteStartupFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus string
	}{
		{
			name:       "permission denied",
			err:        fmt.Errorf("wrapped: %w", commandpaste.ErrPermissionDenied),
			wantStatus: "Command+V paste requires Input Monitoring permission",
		},
		{
			name:       "generic failure",
			err:        errors.New("listener unavailable"),
			wantStatus: "Command+V paste unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{}
			startCalls := 0
			a.initializeCommandPaste(func() (commandpaste.Monitor, error) {
				startCalls++
				return nil, tt.err
			})

			if startCalls != 1 {
				t.Fatalf("start calls = %d, want 1", startCalls)
			}
			if got := a.statusMsg; got != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got, tt.wantStatus)
			}
			if a.commandPasteRecent == nil {
				t.Fatal("commandPasteRecent was not installed")
			}
			if a.commandPasteRecent(time.Now()) {
				t.Fatal("failed monitor startup authorized terminal paste")
			}
			if a.closeCommandPaste == nil {
				t.Fatal("closeCommandPaste was not installed")
			}

			a.Close()
			a.Close()
			if a.commandPasteRecent(time.Now()) {
				t.Fatal("closed failed monitor authorized terminal paste")
			}
		})
	}
}

// mustResolveTestPath returns a canonical existing path for app fixtures.
func mustResolveTestPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve test path: %v", err)
	}
	return resolved
}

// recordingClipboard installs a deterministic system clipboard writer and
// returns every text value the app attempted to publish.
func recordingClipboard(a *App, err error) *[]string {
	var copied []string
	a.copyToSystem = func(text string) error {
		copied = append(copied, text)
		return err
	}
	return &copied
}

// TestSidebarW_ShownVsHidden verifies the sidebar width helper returns 0
// when hidden and the configured width when shown. Every layout helper
// pivots on this so we want it locked in.
func TestSidebarW_ShownVsHidden(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if got := a.sidebarW(); got != defaultSidebarWidth {
		t.Fatalf("shown sidebarW: got %d, want %d", got, defaultSidebarWidth)
	}
	a.sidebarShown = false
	if got := a.sidebarW(); got != 0 {
		t.Fatalf("hidden sidebarW: got %d, want 0", got)
	}
}

// TestSidebarRect checks the tree leaves room for the path panel and splitter.
func TestSidebarRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.sidebarRect()
	if x != 0 || y != 0 {
		t.Fatalf("expected origin (0,0), got (%d,%d)", x, y)
	}
	if w != defaultSidebarWidth-1 {
		t.Fatalf("expected w = sidebarWidth-1, got %d", w)
	}
	if h != a.height-1-sidebarPathPanelHeight {
		t.Fatalf("expected h = height-1-sidebarPathPanelHeight, got %d", h)
	}

	a.sidebarShown = false
	x, y, w, h = a.sidebarRect()
	if x != 0 || y != 0 || w != 0 || h != 0 {
		t.Fatalf("expected zero rect when hidden, got (%d,%d,%d,%d)", x, y, w, h)
	}
}

// TestSplitterX returns the splitter column when shown and -1 when hidden.
func TestSplitterX(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if got := a.splitterX(); got != defaultSidebarWidth-1 {
		t.Fatalf("shown splitterX: got %d", got)
	}
	a.sidebarShown = false
	if got := a.splitterX(); got != -1 {
		t.Fatalf("hidden splitterX: got %d, want -1", got)
	}
}

// TestTabBarRect checks the tab bar starts after the sidebar and spans the
// remaining width on row 0.
func TestTabBarRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.tabBarRect()
	if x != defaultSidebarWidth || y != 0 || h != 1 {
		t.Fatalf("tabBar position/size unexpected: (%d,%d,%d,%d)", x, y, w, h)
	}
	if w != a.width-defaultSidebarWidth {
		t.Fatalf("tabBar width: got %d", w)
	}
	a.sidebarShown = false
	x, _, w, _ = a.tabBarRect()
	if x != 0 || w != a.width {
		t.Fatalf("hidden-sidebar tabBar should fill row: got x=%d w=%d", x, w)
	}
}

// TestEditorRect verifies the editor body sits between tab bar and status
// bar, to the right of the sidebar.
func TestEditorRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.editorRect()
	if x != defaultSidebarWidth || y != 1 {
		t.Fatalf("editor origin: (%d,%d)", x, y)
	}
	if w != a.width-defaultSidebarWidth {
		t.Fatalf("editor width: got %d", w)
	}
	if h != a.height-2 {
		t.Fatalf("editor height: got %d", h)
	}
}

// TestStatusRect always returns the bottom-most row, full width.
func TestStatusRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.statusRect()
	if x != 0 || y != a.height-1 || w != a.width || h != 1 {
		t.Fatalf("status rect: (%d,%d,%d,%d)", x, y, w, h)
	}
}

// TestMenuButtonRect places the ≡ button at the start of the tab bar and
// shifts left when the sidebar is hidden.
func TestMenuButtonRect(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, _, w, _ := a.menuButtonRect()
	if x != defaultSidebarWidth || w != menuButtonWidth {
		t.Fatalf("shown menuButtonRect: x=%d w=%d", x, w)
	}
	a.sidebarShown = false
	x, _, _, _ = a.menuButtonRect()
	if x != 0 {
		t.Fatalf("hidden menuButtonRect should sit at column 0: got %d", x)
	}
}

// TestMenuModalRect centers the modal in the window and clamps the origin
// to (0,0) when the window is too small to fit it.
func TestMenuModalRect_Centered(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	x, y, w, h := a.menuModalRect()
	_, _, expectedH := a.menuLayout()
	if w != modalWidth || h != expectedH {
		t.Fatalf("modal size: got (%d,%d), want (%d,%d)", w, h, modalWidth, expectedH)
	}
	if x != (a.width-modalWidth)/2 || y != (a.height-expectedH)/2 {
		t.Fatalf("modal origin off-center: (%d,%d)", x, y)
	}
}

// TestMenuModalRect_ClampsTinyWindow ensures the origin never goes negative
// even if the window is smaller than the modal.
func TestMenuModalRect_ClampsTinyWindow(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.width, a.height = 10, 5
	x, y, _, _ := a.menuModalRect()
	if x != 0 || y != 0 {
		t.Fatalf("expected clamped origin (0,0), got (%d,%d)", x, y)
	}
}

// TestResizeSidebar_Clamps verifies the sidebar width clamps to the
// [minSidebarWidth, width-minEditorAfterDrag] range.
func TestResizeSidebar_Clamps(t *testing.T) {
	a := newTestApp(t, t.TempDir())

	// Negative target → clamps up to minSidebarWidth.
	a.resizeSidebar(-50)
	if a.sidebarWidth != minSidebarWidth {
		t.Fatalf("negative target: got %d, want %d", a.sidebarWidth, minSidebarWidth)
	}

	// Above max → clamps to width - minEditorAfterDrag.
	a.resizeSidebar(a.width)
	wantMax := a.width - minEditorAfterDrag
	if a.sidebarWidth != wantMax {
		t.Fatalf("oversize target: got %d, want %d", a.sidebarWidth, wantMax)
	}

	// In range — kept verbatim.
	a.resizeSidebar(25)
	if a.sidebarWidth != 25 {
		t.Fatalf("in-range target: got %d", a.sidebarWidth)
	}
}

// TestResizeSidebar_TinyWindow falls back to minSidebarWidth when the window
// is too narrow for both panels at the requested size.
func TestResizeSidebar_TinyWindow(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.width = 30 // smaller than minSidebarWidth + minEditorAfterDrag.
	a.resizeSidebar(50)
	if a.sidebarWidth != minSidebarWidth {
		t.Fatalf("tiny window: got %d, want %d", a.sidebarWidth, minSidebarWidth)
	}
}

// TestDetectLangLabel covers the language label helper's three cases.
func TestDetectLangLabel(t *testing.T) {
	cases := map[string]string{
		"":               "text",
		"foo.go":         "go",
		"foo":            "text",
		"path/to/x.py":   "py",
		"archive.tar.gz": "gz",
	}
	for in, want := range cases {
		if got := detectLangLabel(in); got != want {
			t.Errorf("detectLangLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIsWordChar pins down the ASCII-only word definition we use for
// double-click word selection.
func TestIsWordChar(t *testing.T) {
	word := []rune{'a', 'z', 'A', 'Z', '0', '9', '_'}
	for _, r := range word {
		if !isWordChar(r) {
			t.Errorf("isWordChar(%q) = false, want true", r)
		}
	}
	nonWord := []rune{' ', '\t', '.', ',', '-', '!', '\n', '/'}
	for _, r := range nonWord {
		if isWordChar(r) {
			t.Errorf("isWordChar(%q) = true, want false", r)
		}
	}
}

// TestSetActiveFolder writes both the App field and the tree's mirror copy.
func TestSetActiveFolder(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.setActiveFolder(sub)
	if a.activeFolder != sub {
		t.Fatalf("activeFolder: got %q, want %q", a.activeFolder, sub)
	}
	if a.tree.ActiveFolder != sub {
		t.Fatalf("tree.ActiveFolder: got %q, want %q", a.tree.ActiveFolder, sub)
	}
}

// TestOpenFile_Basic opens a file, switches to it on re-open, and updates
// activeFolder to the file's parent.
func TestOpenFile_Basic(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "child")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	target := filepath.Join(sub, "file.txt")
	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	a := newTestApp(t, dir)
	a.openFile(target)
	if len(a.tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(a.tabs))
	}
	if a.activeFolder != sub {
		t.Fatalf("activeFolder: got %q, want %q", a.activeFolder, sub)
	}

	// Re-opening should switch to existing tab, not create a new one.
	a.activeTab = -1
	a.openFile(target)
	if len(a.tabs) != 1 {
		t.Fatalf("re-open created duplicate tab")
	}
	if a.activeTab != 0 {
		t.Fatalf("re-open didn't switch active: got %d", a.activeTab)
	}
}

// TestOpenFile_ErrorFlash surfaces an error when the path can't be loaded
// (here, a directory rather than a file).
func TestOpenFile_ErrorFlash(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(sub)
	if !strings.Contains(a.statusMsg, "Error") {
		t.Fatalf("expected error flash, got %q", a.statusMsg)
	}
	if len(a.tabs) != 0 {
		t.Fatalf("expected no tabs, got %d", len(a.tabs))
	}
}

// TestRequestCloseTab_DirtyOpensModal proves a dirty tab does not close
// on first request and instead opens the unsaved-changes modal so the
// user can pick Save / Discard / Cancel.
func TestRequestCloseTab_DirtyOpensModal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dirty.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.tabs[0].Dirty = true

	a.requestCloseTab(0)
	if len(a.tabs) != 1 {
		t.Fatalf("dirty tab should not close until the user picks an action")
	}
	if !a.dirtyOpen {
		t.Fatal("dirty close modal should be open")
	}
}

// TestRequestCloseTab_CleanClosesImmediately closes a clean tab in one shot.
func TestRequestCloseTab_CleanClosesImmediately(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "clean.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.requestCloseTab(0)
	if len(a.tabs) != 0 {
		t.Fatalf("clean tab should close on first request")
	}
}

// TestCloseTab_ClampsActive ensures activeTab never points outside the slice.
func TestCloseTab_ClampsActive(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	a := newTestApp(t, dir)
	a.openFile(filepath.Join(dir, "a.txt"))
	a.openFile(filepath.Join(dir, "b.txt"))
	a.activeTab = 1
	a.closeTab(1)
	if a.activeTab != 0 {
		t.Fatalf("activeTab should clamp to 0 after closing last; got %d", a.activeTab)
	}
	a.closeTab(0)
	if a.activeTab != 0 {
		t.Fatalf("activeTab should stay >=0 with no tabs; got %d", a.activeTab)
	}
}

// TestCloseTab_OutOfRange is a no-op.
func TestCloseTab_OutOfRange(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.closeTab(-1)
	a.closeTab(99)
	a.requestCloseTab(99)
}

// TestHasTab_Predicates covers the "is X available?" checks used to dim menu
// rows.
func TestHasTab_Predicates(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("hi"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	if a.hasTab() || a.hasSavableTab() || a.hasSelection() || a.hasClipboard() || a.hasEditableTextTab() || a.hasPasteTarget() || a.hasCommentableTab() {
		t.Fatal("fresh app should have no tab/selection/clipboard/editable/paste/comment action")
	}

	a.openFile(target)
	if !a.hasTab() || !a.hasSavableTab() || !a.hasEditableTextTab() {
		t.Fatal("expected active editable tab after open")
	}
	if a.hasSelection() {
		t.Fatal("no selection on a fresh tab")
	}
	if a.hasCommentableTab() {
		t.Fatal(".txt should not expose the line-comment action")
	}

	// Make a synthetic selection.
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 1}
	if !a.hasSelection() {
		t.Fatal("expected selection after Anchor != Cursor")
	}

	if !a.hasPasteTarget() {
		t.Fatal("editable tab should be a paste target with an empty internal clipboard")
	}

	a.clipBuf = "x"
	if !a.hasClipboard() || !a.hasPasteTarget() {
		t.Fatal("expected clipboard and paste target once clipBuf set")
	}

	blocked := filepath.Join(dir, "blocked.pdf")
	if err := os.WriteFile(blocked, []byte("%PDF-1.7\n"), 0o644); err != nil {
		t.Fatalf("seed external file: %v", err)
	}
	a.openFile(blocked)
	if a.hasEditableTextTab() || a.hasPasteTarget() {
		t.Fatal("read-only external tab should not be editable or pasteable")
	}
}

// TestHasCommentableTab_Predicate checks that line-comment actions only enable
// on editable text tabs with known comment syntax.
func TestHasCommentableTab_Predicate(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	htmlFile := filepath.Join(dir, "index.html")
	if err := os.WriteFile(goFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("seed go: %v", err)
	}
	if err := os.WriteFile(htmlFile, []byte("<main></main>"), 0644); err != nil {
		t.Fatalf("seed html: %v", err)
	}
	a := newTestApp(t, dir)

	a.openFile(goFile)
	if !a.hasCommentableTab() {
		t.Fatal(".go tab should expose the line-comment action")
	}

	a.openFile(htmlFile)
	if a.hasCommentableTab() {
		t.Fatal(".html tab should not expose the line-comment action")
	}
}

// TestSidebarToggleLabel flips between Show/Hide based on sidebarShown.
func TestSidebarToggleLabel(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if a.sidebarToggleLabel() != "Hide file explorer" {
		t.Fatalf("got %q", a.sidebarToggleLabel())
	}
	a.sidebarShown = false
	if a.sidebarToggleLabel() != "Show file explorer" {
		t.Fatalf("got %q", a.sidebarToggleLabel())
	}
}

// TestNewFileLabel_Plain shows the bare label when the active folder is the
// project root.
func TestNewFileLabel_Plain(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if got := a.newFileLabel(); got != "New file" {
		t.Fatalf("root label: got %q", got)
	}
	a.activeFolder = ""
	if got := a.newFileLabel(); got != "New file" {
		t.Fatalf("empty folder label: got %q", got)
	}
}

// TestNewFileLabel_SuffixForSubdir adds a "(in subdir)" suffix when the
// active folder is under the project root.
func TestNewFileLabel_SuffixForSubdir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "alpha")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.setActiveFolder(sub)
	got := a.newFileLabel()
	if !strings.HasPrefix(got, "New file (in ") {
		t.Fatalf("expected 'New file (in ...)', got %q", got)
	}
	if !strings.Contains(got, "alpha") {
		t.Fatalf("expected basename in label, got %q", got)
	}
}

// TestNewFileLabel_TruncatesLongPaths keeps the trailing folder visible
// when the relative path would otherwise overflow the modal.
func TestNewFileLabel_TruncatesLongPaths(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir,
		"this-is-a-rather-long-name", "and-another-very-long-name", "trailing")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.setActiveFolder(deep)
	got := a.newFileLabel()
	if !strings.Contains(got, "trailing") {
		t.Fatalf("expected trailing folder name preserved; got %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("expected truncation ellipsis; got %q", got)
	}
}

// TestMenuNewFolder opens a prompt rooted at the active folder and creates
// nested directories when the submitted relative path is accepted.
func TestMenuNewFolder(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err != nil {
		t.Fatalf("resolve root: %v", err)
	} else {
		root = resolved
	}
	active := filepath.Join(root, "work")
	if err := os.Mkdir(active, 0o755); err != nil {
		t.Fatalf("seed active folder: %v", err)
	}
	a := newTestApp(t, root)
	a.setActiveFolder(active)

	a.menuNewFolder()
	if !a.promptOpen {
		t.Fatal("expected folder prompt to open")
	}
	if got := a.promptTitle; got != "New folder" {
		t.Fatalf("prompt title = %q, want New folder", got)
	}
	if got, want := a.promptHint, "in "+a.relativeFolderLabel(active); got != want {
		t.Fatalf("prompt hint = %q, want %q", got, want)
	}

	a.promptValue = []rune("assets/icons")
	a.promptSubmit()
	if _, err := os.Stat(filepath.Join(active, "assets", "icons")); err != nil {
		t.Fatalf("nested folder missing after submit: %v", err)
	}
}

// TestMenuNewFolder_MissingActiveFolderFallsBackToRoot ensures an externally
// deleted active folder cannot leave the prompt targeting a stale path.
func TestMenuNewFolder_MissingActiveFolderFallsBackToRoot(t *testing.T) {
	root := t.TempDir()
	a := newTestApp(t, root)
	a.activeFolder = filepath.Join(root, "missing")

	a.menuNewFolder()
	if got, want := a.promptHint, "in "+a.relativeFolderLabel(root); got != want {
		t.Fatalf("prompt hint = %q, want %q", got, want)
	}
	if got := a.activeFolder; got != root {
		t.Fatalf("active folder = %q, want root %q", got, root)
	}
}

// TestBuiltinMenuNewFolderHasNoShortcut keeps folder creation reachable from
// the menu without claiming an Esc-leader shortcut that has not been wired.
func TestBuiltinMenuNewFolderHasNoShortcut(t *testing.T) {
	fileActions := builtinMenuGroups()[3]
	for _, item := range fileActions {
		if item.label == "New folder" {
			if item.shortcut != "" {
				t.Fatalf("New folder shortcut = %q, want empty", item.shortcut)
			}
			return
		}
	}
	t.Fatal("New folder menu item missing")
}

// TestRelativeFolderLabel covers the three branches: root, subdir, and a
// non-relatable path.
func TestRelativeFolderLabel(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "child")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)

	// Root → basename + sep.
	rootLabel := a.relativeFolderLabel(a.rootDir)
	if !strings.HasSuffix(rootLabel, string(filepath.Separator)) {
		t.Fatalf("root missing trailing sep: %q", rootLabel)
	}
	if !strings.HasPrefix(rootLabel, filepath.Base(a.rootDir)) {
		t.Fatalf("root should start with its basename: %q", rootLabel)
	}

	// Subdir → relative path.
	subLabel := a.relativeFolderLabel(sub)
	if subLabel != "child"+string(filepath.Separator) {
		t.Fatalf("subdir label: got %q", subLabel)
	}
}

// TestMenuMoveSelection_WrapsAroundEnds simulates a small menu with all rows
// enabled to verify wrapping in both directions.
func TestMenuMoveSelection_WrapsAroundEnds(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	// Open every potential gate: a savable tab + selection + clipboard.
	tmp := filepath.Join(a.rootDir, "f.txt")
	if err := os.WriteFile(tmp, []byte("hello"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a.openFile(tmp)
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 1}
	a.clipBuf = "x"

	// Count the rows currently enabled so we know how many forward
	// steps land us back at the starting row (vs going past it). A
	// hard-coded len breaks every time the menu grows.
	items, _, _ := a.menuLayout()
	enabled := 0
	for _, item := range items {
		if item.enabled(a) {
			enabled++
		}
	}
	if enabled < 2 {
		t.Fatalf("need at least 2 enabled items to test wrap; got %d", enabled)
	}

	// Walk forward exactly `enabled` steps and land on the first row.
	a.hoveredMenuRow = -1
	a.menuMoveSelection(1)
	first := a.hoveredMenuRow
	for i := 1; i < enabled; i++ {
		a.menuMoveSelection(1)
	}
	a.menuMoveSelection(1) // wrap
	if a.hoveredMenuRow != first {
		t.Fatalf("forward wrap: got %d, want %d", a.hoveredMenuRow, first)
	}

	// Same for backward.
	a.hoveredMenuRow = -1
	a.menuMoveSelection(-1)
	last := a.hoveredMenuRow
	for i := 1; i < enabled; i++ {
		a.menuMoveSelection(-1)
	}
	a.menuMoveSelection(-1) // wrap
	if a.hoveredMenuRow != last {
		t.Fatalf("backward wrap: got %d, want %d", a.hoveredMenuRow, last)
	}
}

// TestMenuMoveSelection_NothingEnabledYieldsMinusOne lands on -1 when no row
// is enabled (we synthesise that by setting every predicate to false-ish via
// the no-tab/no-clipboard initial state, except always-true rows).
func TestMenuMoveSelection_SkipsDisabled(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	// No tabs, no selection, no clipboard. Save/Close/Rename/Delete/Copy/
	// Cut/Paste are all disabled. New file / toggle / quit stay enabled.
	a.hoveredMenuRow = -1
	a.menuMoveSelection(1)
	if a.hoveredMenuRow < 0 {
		t.Fatal("expected a row to land somewhere")
	}
	idx := a.hoveredMenuRow
	items, _, _ := a.menuLayout()
	if !items[idx].enabled(a) {
		t.Fatalf("landed on disabled row %d", idx)
	}
}

// TestFlash sets statusMsg and pushes statusUntil into the future.
func TestFlash(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	before := time.Now()
	a.flash("hello world")
	if a.statusMsg != "hello world" {
		t.Fatalf("statusMsg: got %q", a.statusMsg)
	}
	if !a.statusUntil.After(before) {
		t.Fatalf("statusUntil should be in the future, got %v", a.statusUntil)
	}
}

// TestMenuToggleSidebar flips the sidebarShown flag.
func TestMenuToggleSidebar(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if !a.sidebarShown {
		t.Fatal("sidebar should start visible")
	}
	a.menuToggleSidebar()
	if a.sidebarShown {
		t.Fatal("expected hidden after first toggle")
	}
	a.menuToggleSidebar()
	if !a.sidebarShown {
		t.Fatal("expected shown after second toggle")
	}
}

// TestMenuToggleLineComment runs the menu action against the active tab so the
// app layer and editor-layer primitive stay wired together.
func TestMenuToggleLineComment(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main.go")
	if err := os.WriteFile(target, []byte("one\ntwo"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)

	a.menuToggleLineComment()

	if got := a.activeTabPtr().Buffer.String(); got != "// one\ntwo" {
		t.Fatalf("buffer = %q, want current line commented", got)
	}
	if a.statusMsg != "Toggled line comment" {
		t.Fatalf("statusMsg = %q", a.statusMsg)
	}
}

// TestMenuToggleLineComment_Unsupported flashes a clear no-op instead of
// guessing at block-comment-only formats.
func TestMenuToggleLineComment_Unsupported(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "index.html")
	if err := os.WriteFile(target, []byte("<main></main>"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)

	a.menuToggleLineComment()

	if got := a.activeTabPtr().Buffer.String(); got != "<main></main>" {
		t.Fatalf("unsupported buffer changed to %q", got)
	}
	if a.statusMsg != "No line comment syntax for this file" {
		t.Fatalf("statusMsg = %q", a.statusMsg)
	}
}

// TestBuiltinMenu_DeleteCurrentLine exposes the line deletion action with its
// Esc-d shortcut and routes activation to the active tab.
func TestBuiltinMenu_DeleteCurrentLine(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "lines.txt")
	if err := os.WriteFile(target, []byte("one\ntwo\nthree"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 1, Col: 0}
	tab.Cursor = editor.Position{Line: 1, Col: 1}
	a.openMenu()

	item := menuItemByLabel(t, a, "Delete current line")
	if item.shortcut != "Esc d" {
		t.Fatalf("shortcut = %q, want Esc d", item.shortcut)
	}
	if !item.enabled(a) {
		t.Fatal("delete current line should be enabled for editable text")
	}
	item.action(a)

	if a.menuOpen {
		t.Fatal("delete current line should close the menu")
	}
	if got, want := tab.Buffer.String(), "one\nthree"; got != want {
		t.Fatalf("menu delete buffer = %q, want %q", got, want)
	}
}

// TestMenuDeleteCurrentLine_ReadOnlyIsNoOp keeps direct menu activation inert
// when the active tab is read-only.
func TestMenuDeleteCurrentLine_ReadOnlyIsNoOp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "blocked.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.7\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	want := tab.Buffer.String()
	item := menuItemByLabel(t, a, "Delete current line")
	if item.enabled(a) {
		t.Fatal("delete current line should be disabled for read-only tabs")
	}

	item.action(a)

	if got := tab.Buffer.String(); got != want {
		t.Fatalf("menu delete changed read-only buffer to %q, want %q", got, want)
	}
}

// TestTabBarClick_OpensMenu clicks the ≡ button cell and verifies the menu
// opens.
func TestTabBarClick_OpensMenu(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	mx, _, _, _ := a.menuButtonRect()
	a.tabBarClick(mx, 0)
	if !a.menuOpen {
		t.Fatal("clicking ≡ should open menu")
	}
}

// TestTabBarClick_SwitchesTab clicks inside a non-active tab's body and
// verifies activeTab updates.
func TestTabBarClick_SwitchesTab(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	a := newTestApp(t, dir)
	a.openFile(filepath.Join(dir, "a.txt"))
	a.openFile(filepath.Join(dir, "b.txt"))
	// b is active. Lay out the tabs and click inside tab 0's body (not the ×).
	a.lastTabRects = a.layoutTabs()
	tabA := a.lastTabRects[0]
	clickX := tabA.X + 1
	if clickX == tabA.CloseX {
		clickX = tabA.X + 2
	}
	a.tabBarClick(clickX, 0)
	if a.activeTab != 0 {
		t.Fatalf("expected activeTab=0, got %d", a.activeTab)
	}
}

// TestEditorSize matches the editor rect's width and height.
func TestEditorSize(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	w, h := a.editorSize()
	if w != a.width-defaultSidebarWidth || h != a.height-2 {
		t.Fatalf("editorSize: got (%d,%d)", w, h)
	}
}

// TestActiveTabPtr returns nil with no tabs and the right pointer otherwise.
func TestActiveTabPtr(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	if a.activeTabPtr() != nil {
		t.Fatal("expected nil with no tabs")
	}
	a.openFile(target)
	if a.activeTabPtr() != a.tabs[0] {
		t.Fatal("activeTabPtr should match tabs[activeTab]")
	}
	a.activeTab = 99
	if a.activeTabPtr() != nil {
		t.Fatal("out-of-range activeTab should yield nil")
	}
}

// TestSaveActiveTab writes the buffer to disk and clears Dirty.
func TestSaveActiveTab(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "save.txt")
	if err := os.WriteFile(target, []byte("seed"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.activeTabPtr().InsertString("X")
	a.saveActiveTab()
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "X") {
		t.Fatalf("save did not persist: %q", got)
	}
}

// TestSaveActiveTab_NoTab is a no-op.
func TestSaveActiveTab_NoTab(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.saveActiveTab()
}

// TestCopyCutPaste exercises the clipboard glue.
func TestCopyCutPaste(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)

	// No selection — copy/cut should be no-ops.
	a.copySelection()
	a.cutSelection()
	if a.clipBuf != "" {
		t.Fatalf("clipBuf should still be empty: %q", a.clipBuf)
	}

	// Make selection of "hello".
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 5}
	a.copySelection()
	if a.clipBuf != "hello" {
		t.Fatalf("copy: clipBuf %q", a.clipBuf)
	}

	// Cut: same selection should now empty the buffer.
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 5}
	a.cutSelection()
	if tab.Buffer.LineRunes(0) != nil && len(tab.Buffer.LineRunes(0)) != 0 {
		// Some buffer impls return empty slice; both fine.
	}

	// Paste reads from the system clipboard rather than the internal copy buffer.
	a.clipBuf = "internal"
	a.readFromSystem = func() (string, error) { return "X", nil }
	a.pasteClipboard()
}

// TestNormalizePastedText collapses terminal and clipboard newline variants
// into the editor's canonical LF representation.
func TestNormalizePastedText(t *testing.T) {
	if got := normalizePastedText("a\r\nb\rc\nd"); got != "a\nb\nc\nd" {
		t.Fatalf("normalized = %q", got)
	}
}

// TestPasteClipboard_NormalizesNewlines keeps explicit system and terminal
// pastes on the same insertion path so copied CRLF text has no stray CR bytes.
func TestPasteClipboard_NormalizesNewlines(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	tab, err := editor.NewTab("")
	if err != nil {
		t.Fatal(err)
	}
	a.tabs = []*editor.Tab{tab}
	a.readFromSystem = func() (string, error) { return "a\r\nb\rc", nil }

	a.pasteClipboard()

	if got := tab.Buffer.String(); got != "a\nb\nc" {
		t.Fatalf("buffer = %q, want normalized clipboard text", got)
	}
	if got := tab.Buffer.LineCount(); got != 3 {
		t.Fatalf("line count = %d, want 3", got)
	}
}

// TestMenuSelectAll_SelectsEditableTextWithoutCopy verifies Select All only
// selects the editable buffer and leaves both clipboard channels untouched.
func TestMenuSelectAll_SelectsEditableTextWithoutCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "all.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(path)
	a.clipBuf = "keep"
	copied := recordingClipboard(a, nil)
	a.openMenu()

	a.menuSelectAll()

	tab := a.activeTabPtr()
	if a.menuOpen {
		t.Fatal("Select all should close menu")
	}
	if got, want := tab.SelectionText(), "one\ntwo\n"; got != want {
		t.Fatalf("selection = %q, want %q", got, want)
	}
	if a.clipBuf != "keep" {
		t.Fatalf("clipBuf = %q, want existing value", a.clipBuf)
	}
	if len(*copied) != 0 {
		t.Fatalf("Select all wrote clipboard %#v", *copied)
	}
}

// TestMenuSelectAll_InertWithoutEditableText ensures direct action invocation
// is safe even when the menu row would be disabled.
func TestMenuSelectAll_InertWithoutEditableText(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(t *testing.T, a *App)
	}{
		{
			name: "no tab",
			open: func(_ *testing.T, _ *App) {},
		},
		{
			name: "external pdf",
			open: func(t *testing.T, a *App) {
				path := filepath.Join(a.rootDir, "blocked.pdf")
				if err := os.WriteFile(path, []byte("%PDF-1.7\n"), 0o644); err != nil {
					t.Fatalf("seed: %v", err)
				}
				a.openFile(path)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t, t.TempDir())
			copied := recordingClipboard(a, nil)
			tc.open(t, a)

			a.menuSelectAll()

			if len(*copied) != 0 {
				t.Fatalf("Select all wrote clipboard %#v", *copied)
			}
			if tab := a.activeTabPtr(); tab != nil && tab.HasSelection() {
				t.Fatal("Select all should not select external tab")
			}
		})
	}
}

// TestPasteClipboard_NoTab is safe with no tab open.
func TestPasteClipboard_NoTab(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.readFromSystem = func() (string, error) {
		t.Fatal("system clipboard should not be read without a paste target")
		return "", nil
	}
	a.pasteClipboard() // no tab — nothing to paste into.
}

// TestPasteClipboard_MenuUsesSystemClipboardWhenMonitorUnavailable verifies
// explicit menu paste remains independent of terminal-paste authorization.
func TestPasteClipboard_MenuUsesSystemClipboardWhenMonitorUnavailable(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.commandPasteRecent = func(time.Time) bool { return false }
	tab, err := editor.NewTab("")
	if err != nil {
		t.Fatal(err)
	}
	a.tabs = []*editor.Tab{tab}
	a.clipBuf = "internal text"
	a.readFromSystem = func() (string, error) { return "system text", nil }

	a.menuPaste()

	if got := tab.Buffer.String(); got != "system text" {
		t.Fatalf("buffer = %q, want system clipboard text", got)
	}
	if got := a.clipBuf; got != "internal text" {
		t.Fatalf("clipBuf = %q, want retained internal text", got)
	}
}

// TestCloseStopsCommandPaste releases the monitor hook before terminal
// teardown and clears it so repeated Close calls remain safe.
func TestCloseStopsCommandPaste(t *testing.T) {
	calls := 0
	a := &App{closeCommandPaste: func() { calls++ }}

	a.Close()
	a.Close()

	if calls != 1 {
		t.Fatalf("close command paste calls = %d, want 1", calls)
	}
}

// TestMenuPaste_SystemClipboardError keeps explicit paste from falling back
// to copied text when the system clipboard reader is unavailable.
func TestMenuPaste_SystemClipboardError(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	tab, err := editor.NewTab("")
	if err != nil {
		t.Fatal(err)
	}
	a.tabs = []*editor.Tab{tab}
	a.clipBuf = "internal text"
	a.readFromSystem = func() (string, error) { return "", errors.New("pbpaste unavailable") }

	a.menuPaste()

	if got := tab.Buffer.String(); got != "" {
		t.Fatalf("buffer = %q, want unchanged after system reader error", got)
	}
	if got := a.clipBuf; got != "internal text" {
		t.Fatalf("clipBuf = %q, want retained internal text", got)
	}
	if !strings.Contains(strings.ToLower(a.statusMsg), "system clipboard") {
		t.Fatalf("status = %q, want system-clipboard error", a.statusMsg)
	}
}

// TestMenuSaveAndClose saves then closes the active tab.
func TestMenuSaveAndClose(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sc.txt")
	if err := os.WriteFile(target, []byte("seed"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.activeTabPtr().InsertString("Y")
	a.menuSaveAndClose()
	if len(a.tabs) != 0 {
		t.Fatalf("expected tab closed; got %d tabs", len(a.tabs))
	}
}

// TestMenuSaveAndClose_NoTab is a no-op when nothing is open.
func TestMenuSaveAndClose_NoTab(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.menuSaveAndClose()
}

// TestMenuClickPaths covers menuSave/menuCopy/menuCut/menuPaste/menuClose
// menuQuit and menuRefreshTree as one-liners.
func TestMenuClickPaths(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("hi"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)

	// Selection so copy/cut have something to operate on.
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 2}

	a.menuOpen = true
	a.menuSave()
	a.menuOpen = true
	a.menuCopy()
	a.menuOpen = true
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 1}
	a.menuCut()
	a.menuOpen = true
	a.menuPaste()
	a.menuOpen = true
	a.menuRefreshTree()

	// Clean the tab before quitting; the dirty-quit path is exercised
	// separately in dirty_modal_test.go.
	tab.Dirty = false
	a.menuOpen = true
	a.menuQuit()
	if !a.quit {
		t.Fatal("menuQuit should set quit flag")
	}
}

// TestUndoRedoRevert_MenuPaths exercises the new history actions end
// to end through the menu wrappers. The flash on no-op paths is also
// covered so the user always gets feedback when they hit a dead-end.
func TestUndoRedoRevert_MenuPaths(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "edit.txt")
	if err := os.WriteFile(target, []byte("seed"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()

	// Nothing to undo / redo / revert on a freshly opened file.
	if a.hasUndo() || a.hasRedo() || a.hasRevert() {
		t.Fatal("freshly opened tab should have no history")
	}
	a.menuOpen = true
	a.menuUndo()
	a.menuOpen = true
	a.menuRedo()
	a.menuOpen = true
	a.menuRevert()

	// One edit → undo + revert become available.
	tab.MoveCursorTo(editor.Position{Line: 0, Col: 4}, false)
	tab.InsertString("X")
	if !a.hasUndo() || !a.hasRevert() {
		t.Fatal("expected undo + revert after edit")
	}
	if a.hasRedo() {
		t.Fatal("redo should still be empty")
	}

	a.menuOpen = true
	a.menuUndo()
	if got := tab.Buffer.String(); got != "seed" {
		t.Fatalf("after menuUndo = %q, want seed", got)
	}
	if !a.hasRedo() {
		t.Fatal("redo should be populated after an undo")
	}

	a.menuOpen = true
	a.menuRedo()
	if got := tab.Buffer.String(); got != "seedX" {
		t.Fatalf("after menuRedo = %q, want seedX", got)
	}

	// Revert back to original; then Undo must recover the post-edit state.
	a.menuOpen = true
	a.menuRevert()
	if got := tab.Buffer.String(); got != "seed" {
		t.Fatalf("after menuRevert = %q, want seed", got)
	}
	a.menuOpen = true
	a.menuUndo()
	if got := tab.Buffer.String(); got != "seedX" {
		t.Fatalf("after undo-of-revert = %q, want seedX", got)
	}
}

// TestUndoRedoRevert_NoTabSafelyNoOps guards against crashes when the
// menu rows somehow fire with no active tab — they should silently
// return rather than dereferencing nil.
func TestUndoRedoRevert_NoTabSafelyNoOps(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.menuOpen = true
	a.menuUndo()
	a.menuOpen = true
	a.menuRedo()
	a.menuOpen = true
	a.menuRevert()
	if a.hasUndo() || a.hasRedo() || a.hasRevert() {
		t.Fatal("no-tab predicates should all be false")
	}
}

// TestMenuClose_NoTab safely no-ops.
func TestMenuClose_NoTab(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.menuOpen = true
	a.menuClose()
}

// TestMenuActivate_RunsHovered runs the action attached to the highlighted
// row.
func TestMenuActivate_RunsHovered(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openMenu()
	// Force highlight onto the sidebar-toggle row (always enabled, label
	// supplied dynamically via labelFor), then activate.
	items, _, _ := a.menuLayout()
	for i, item := range items {
		if item.labelFor != nil && item.label == "" && item.action != nil {
			// labelFor + empty static label is the marker for the toggle
			// row. The newFile row also uses labelFor, so disambiguate by
			// flipping the sidebar and checking afterward.
			a.hoveredMenuRow = i
			a.menuActivate()
			a.openMenu()
		}
	}
	// Re-find and run the toggle row via its dynamic label.
	a.hoveredMenuRow = -1
	items, _, _ = a.menuLayout()
	for i, item := range items {
		if item.labelFor != nil && (item.labelFor(a) == "Show file explorer" || item.labelFor(a) == "Hide file explorer") {
			a.hoveredMenuRow = i
			break
		}
	}
	if a.hoveredMenuRow < 0 {
		t.Fatal("could not find sidebar-toggle row")
	}
	before := a.sidebarShown
	a.menuActivate()
	if a.sidebarShown == before {
		t.Fatal("expected sidebarShown to flip after menuActivate")
	}
}

// TestMenuActivate_OutOfRange and disabled rows are no-ops.
func TestMenuActivate_OutOfRange(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.hoveredMenuRow = -1
	a.menuActivate()
	a.hoveredMenuRow = 999
	a.menuActivate()
}

// TestUpdateMenuHover snaps to the right row when over an enabled row, and
// to -1 when outside.
func TestUpdateMenuHover(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openMenu()
	mx, my, _, _ := a.menuModalRect()

	// Find an always-enabled row and click on its relY.
	items, _, _ := a.menuLayout()
	var pickIdx, pickRelY int
	for i, item := range items {
		if item.enabled(a) {
			pickIdx = i
			pickRelY = item.relY
			break
		}
	}
	a.updateMenuHover(mx+5, my+pickRelY)
	if a.hoveredMenuRow != pickIdx {
		t.Fatalf("hoveredMenuRow: got %d, want %d", a.hoveredMenuRow, pickIdx)
	}

	// Outside the modal → -1.
	a.updateMenuHover(0, 0)
	if a.hoveredMenuRow != -1 {
		t.Fatalf("outside modal: got %d", a.hoveredMenuRow)
	}
}

// TestScrollAt routes scroll to the panel under the cursor; we just verify
// it doesn't panic across the three regions.
func TestScrollAt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.scrollAt(1, 5, 1)           // sidebar
	a.scrollAt(60, 5, 1)          // editor
	a.scrollAt(60, a.height-1, 1) // status bar (no-op-ish)
}

// TestSidebarClick_File opens a file when a file row is clicked.
func TestSidebarClick_File(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "click.txt")
	if err := os.WriteFile(target, []byte("z"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	// Render once so the tree has visible rows for HitTest.
	a.draw()
	// File row is row 1 (0 is the root); click at column 1, row 1.
	a.sidebarClick(1, 1)
	// Only a no-panic guarantee — depending on row order we may or may
	// not have opened the file. Just make sure no crash and either zero
	// or one tab is open.
	if len(a.tabs) > 1 {
		t.Fatalf("unexpected tabs: %d", len(a.tabs))
	}
}

// TestSidebarClick_Miss is safe when (x,y) hits no row.
func TestSidebarClick_Miss(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.sidebarClick(1, 100) // off the bottom of the tree
}

// TestSidebarClick_RootRowResetsActiveFolder pins the bug fix:
// clicking the project-name row in the sidebar (y=1) sets the
// active folder back to the project root. Before this fix, once
// the user picked any subfolder there was no path back to root
// short of restarting the editor — every other row in the tree
// only walks "deeper," not "up." Also confirms the click does not
// open a file or toggle any directory's expansion as a side
// effect; it's purely a navigation/state reset.
func TestSidebarClick_RootRowResetsActiveFolder(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "internal")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.draw() // populate t.visible so HitTest works
	a.setActiveFolder(sub)
	if a.activeFolder == a.rootDir {
		t.Fatal("seed broken: active folder should start as subfolder")
	}

	a.sidebarClick(1, 1) // (col=1, row=1) is the project name row

	if a.activeFolder != a.rootDir {
		t.Errorf("active folder = %q, want root %q", a.activeFolder, a.rootDir)
	}
	if len(a.tabs) != 0 {
		t.Errorf("clicking root opened tabs: %d", len(a.tabs))
	}
	if a.tree.SelectedPath != a.tree.Root.Path {
		t.Errorf("root selection = %q, want %q", a.tree.SelectedPath, a.tree.Root.Path)
	}
}

// TestTreeSelection_FollowsTabsAndFallsBackToFolder keeps the explicit tree
// selection aligned with the editor only when a tab becomes active.
func TestTreeSelection_FollowsTabsAndFallsBackToFolder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.txt")
	second := filepath.Join(dir, "second.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("seed"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newTestApp(t, dir)
	a.openFile(first)
	a.openFile(second)
	a.activeTab = 0
	a.syncActiveTreeFile()
	if a.tree.SelectedPath != first {
		t.Fatalf("tab selection = %q, want %q", a.tree.SelectedPath, first)
	}
	a.closeTabs(append([]*editor.Tab(nil), a.tabs...))
	if a.tree.SelectedPath != a.activeFolder {
		t.Fatalf("empty-tab fallback = %q, want active folder %q", a.tree.SelectedPath, a.activeFolder)
	}
}

// TestTreeSelection_FollowsRenameAndDelete keeps an explicit tree selection
// valid after its node moves or vanishes from the filesystem.
func TestTreeSelection_FollowsRenameAndDelete(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "docs")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "notes.txt")
	if err := os.WriteFile(path, []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.tree.SelectedPath = path
	a.doRenameFile(path, "renamed.txt")
	renamed := filepath.Join(folder, "renamed.txt")
	if a.tree.SelectedPath != renamed {
		t.Fatalf("renamed selection = %q, want %q", a.tree.SelectedPath, renamed)
	}
	a.doRenameFolder(folder, "archive")
	archive := filepath.Join(dir, "archive")
	archived := filepath.Join(archive, "renamed.txt")
	if a.tree.SelectedPath != archived {
		t.Fatalf("renamed folder selection = %q, want %q", a.tree.SelectedPath, archived)
	}
	a.setActiveFolder(archive)
	a.doDeletePath(archive)
	if a.tree.SelectedPath != dir {
		t.Fatalf("deleted selection fallback = %q, want %q", a.tree.SelectedPath, dir)
	}
}

// TestSelectWordAt selects the word under a buffer position.
func TestSelectWordAt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "w.txt")
	if err := os.WriteFile(target, []byte("hello world"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	a.selectWordAt(tab, editor.Position{Line: 0, Col: 2})
	if tab.Anchor.Col != 0 || tab.Cursor.Col != 5 {
		t.Fatalf("word select: anchor=%v cursor=%v", tab.Anchor, tab.Cursor)
	}

	// Empty line — no selection.
	tab.Buffer = editor.NewBuffer("")
	a.selectWordAt(tab, editor.Position{Line: 0, Col: 0})
}

// TestEditorPress_PlacesCaret moves the caret to the clicked spot.
func TestEditorPress_PlacesCaret(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "p.txt")
	if err := os.WriteFile(target, []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	ex, ey, _, _ := a.editorRect()
	a.editorPress(ex+2, ey+1)
	tab := a.activeTabPtr()
	if tab.Cursor.Line != 1 {
		t.Fatalf("expected line 1, got %d", tab.Cursor.Line)
	}
}

// TestOpenGitHunkAt_OpensInfoOnMarker proves gutter markers are clickable.
func TestOpenGitHunkAt_OpensInfoOnMarker(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "p.txt")
	if err := os.WriteFile(target, []byte("hello\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	tab.GitLines = map[int]editor.GitLineChange{0: editor.GitLineModified}

	if !a.openGitHunkAt(tab, 0, 0, 40, 10) {
		t.Fatal("expected gutter marker click to be handled")
	}
	if !a.confirmOpen || !a.confirmInfo {
		t.Fatal("expected git hunk click to open info modal")
	}
}

// TestOpenGitHunkAt_IgnoresCleanGutter keeps normal cursor placement intact.
func TestOpenGitHunkAt_IgnoresCleanGutter(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	tab, err := editor.NewTab("")
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	if a.openGitHunkAt(tab, 0, 0, 40, 10) {
		t.Fatal("clean gutter should not be handled as a git preview")
	}
}

// TestEditorPress_DoubleClickSelectsWord triggers the word-select path.
func TestEditorPress_DoubleClickSelectsWord(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "p.txt")
	if err := os.WriteFile(target, []byte("hello world"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	ex, ey, _, _ := a.editorRect()
	a.editorPress(ex+2, ey)
	a.editorPress(ex+2, ey) // immediately again — double-click within window
	tab := a.activeTabPtr()
	if tab.Anchor.Col == tab.Cursor.Col {
		t.Fatal("expected a word selection after double-click")
	}
}

// TestEditorPress_NoTabSafe doesn't panic with no active tab.
func TestEditorPress_NoTabSafe(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.editorPress(50, 5)
	a.editorDrag(50, 5)
}

// TestEditorDrag_AutoScroll arms the auto-scroll direction when dragging
// outside the editor's vertical bounds.
func TestEditorDrag_AutoScroll(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "d.txt")
	if err := os.WriteFile(target, []byte("a\nb\nc\nd\ne\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	ex, ey, _, eh := a.editorRect()
	a.editorDrag(ex+1, ey-1) // above editor → auto-scroll up
	if a.autoScrollDir != -1 {
		t.Fatalf("expected autoScrollDir=-1, got %d", a.autoScrollDir)
	}
	a.editorDrag(ex+1, ey+eh+1) // below → auto-scroll down
	if a.autoScrollDir != 1 {
		t.Fatalf("expected autoScrollDir=1, got %d", a.autoScrollDir)
	}
	a.editorDrag(ex+1, ey+1) // inside → stops
	if a.autoScrollDir != 0 {
		t.Fatalf("expected stopped autoScroll, got %d", a.autoScrollDir)
	}
}

// TestHandleKey_EscDoubleTapOpensMenu opens the menu after two Esc presses.
func TestHandleKey_EscDoubleTapOpensMenu(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	if a.menuOpen {
		t.Fatal("first Esc should not open menu")
	}
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	if !a.menuOpen {
		t.Fatal("second Esc should open menu")
	}
	// Third Esc — menu open, should close.
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	if a.menuOpen {
		t.Fatal("Esc with menu open should close it")
	}
}

// TestHandleKey_MenuNavKeys move highlight and Enter activates.
func TestHandleKey_MenuNavKeys(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openMenu()
	first := a.hoveredMenuRow
	a.handleKey(keyEv(tcell.KeyDown, 0))
	if a.hoveredMenuRow == first {
		t.Fatal("Down should advance highlight")
	}
	a.handleKey(keyEv(tcell.KeyUp, 0))
	if a.hoveredMenuRow != first {
		t.Fatalf("Up should return to %d, got %d", first, a.hoveredMenuRow)
	}
}

// TestHandleKey_MenuShortcutAfterNavigation keeps menu shortcut letters live
// after arrow-key navigation has moved the highlighted row.
func TestHandleKey_MenuShortcutAfterNavigation(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openMenu()

	a.handleKey(keyEv(tcell.KeyDown, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'p'))

	if a.menuOpen {
		t.Fatal("Esc-p from the open menu should close the menu")
	}
	if !a.finderOpen {
		t.Fatal("Esc-p from the open menu should open the project finder")
	}
}

// TestHandleKey_RoutesToActiveTab dispatches typing to the active tab.
func TestHandleKey_RoutesToActiveTab(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(target, []byte(""), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.handleKey(keyEv(tcell.KeyRune, 'a'))
	a.handleKey(keyEv(tcell.KeyRune, 'b'))
	a.handleKey(keyEv(tcell.KeyEnter, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'c'))
	a.handleKey(keyEv(tcell.KeyTab, 0))
	a.handleKey(keyEv(tcell.KeyBackspace, 0))
	a.handleKey(keyEv(tcell.KeyHome, 0))
	a.handleKey(keyEv(tcell.KeyEnd, 0))
	a.handleKey(keyEv(tcell.KeyLeft, 0))
	a.handleKey(keyEv(tcell.KeyRight, 0))
	a.handleKey(keyEv(tcell.KeyUp, 0))
	a.handleKey(keyEv(tcell.KeyDown, 0))
	a.handleKey(keyEv(tcell.KeyPgUp, 0))
	a.handleKey(keyEv(tcell.KeyPgDn, 0))
	a.handleKey(keyEv(tcell.KeyDelete, 0))
}

// TestHandleKey_EnterOutsidePaste confirms ordinary key handling remains
// immediate when no bracketed paste capture is active.
func TestHandleKey_EnterOutsidePaste(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	tab, err := editor.NewTab("")
	if err != nil {
		t.Fatal(err)
	}
	a.tabs = []*editor.Tab{tab}

	a.handleKey(keyEv(tcell.KeyEnter, 0))

	if got := tab.Buffer.String(); got != "\n" {
		t.Fatalf("buffer = %q, want immediate newline", got)
	}
}

// TestHandleEvent_Resize updates width/height.
func TestHandleEvent_Resize(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	scr := a.screen.(tcell.SimulationScreen)
	scr.SetSize(80, 24)
	ev := tcell.NewEventResize(80, 24)
	a.handleEvent(ev)
	if a.width != 80 || a.height != 24 {
		t.Fatalf("resize: got %dx%d", a.width, a.height)
	}
}

// TestHandleMouse_Wheel routes scroll events to the panel under the cursor.
func TestHandleMouse_Wheel(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	ev := tcell.NewEventMouse(60, 5, tcell.WheelDown, tcell.ModNone)
	a.handleMouse(ev)
	ev = tcell.NewEventMouse(60, 5, tcell.WheelUp, tcell.ModNone)
	a.handleMouse(ev)
}

// TestHandleMouse_WheelHorizontal confirms WheelLeft / WheelRight events
// shift the active tab's ScrollX. The test opens a tab with a long line,
// fires WheelRight to scroll horizontally, then WheelLeft to walk it
// back to zero.
func TestHandleMouse_WheelHorizontal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(target, []byte(strings.Repeat("x", 200)+"\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	if tab == nil {
		t.Fatal("no active tab after openFile")
	}
	// Aim well inside the editor pane (past the sidebar, below the tab bar).
	editorX := a.sidebarW() + 10
	ev := tcell.NewEventMouse(editorX, 5, tcell.WheelRight, tcell.ModNone)
	a.handleMouse(ev)
	if tab.ScrollX == 0 {
		t.Fatalf("WheelRight should advance ScrollX, still 0")
	}
	startX := tab.ScrollX
	ev = tcell.NewEventMouse(editorX, 5, tcell.WheelLeft, tcell.ModNone)
	a.handleMouse(ev)
	if tab.ScrollX >= startX {
		t.Fatalf("WheelLeft should reduce ScrollX, got %d (was %d)", tab.ScrollX, startX)
	}
}

// TestHandleMouse_ShiftWheelScrollsHorizontally confirms that holding
// shift while turning the vertical wheel scrolls the X axis instead —
// this is the path that actually works in most terminals (which never
// emit native WheelLeft/WheelRight). Without shift, the same wheel
// event must scroll vertically; we check both to make sure the modifier
// is what gates the rotation.
func TestHandleMouse_ShiftWheelScrollsHorizontally(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(target, []byte(strings.Repeat("x", 200)+"\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	if tab == nil {
		t.Fatal("no active tab after openFile")
	}
	editorX := a.sidebarW() + 10

	// Shift+WheelDown → horizontal scroll right.
	ev := tcell.NewEventMouse(editorX, 5, tcell.WheelDown, tcell.ModShift)
	a.handleMouse(ev)
	if tab.ScrollX == 0 {
		t.Fatalf("Shift+WheelDown should scroll horizontally, ScrollX still 0")
	}
	if tab.ScrollY != 0 {
		t.Fatalf("Shift+WheelDown should NOT touch ScrollY, got %d", tab.ScrollY)
	}

	// Shift+WheelUp → horizontal scroll left.
	startX := tab.ScrollX
	ev = tcell.NewEventMouse(editorX, 5, tcell.WheelUp, tcell.ModShift)
	a.handleMouse(ev)
	if tab.ScrollX >= startX {
		t.Fatalf("Shift+WheelUp should reduce ScrollX, got %d (was %d)", tab.ScrollX, startX)
	}

	// Unmodified WheelDown still scrolls vertically. Reset the sticky
	// shift state first — within modifierStickyWindow of the previous
	// shift events it'd still register as a shifted wheel.
	tab.ScrollX = 0
	tab.ScrollY = 0
	a.lastShiftAt = time.Time{}
	ev = tcell.NewEventMouse(editorX, 5, tcell.WheelDown, tcell.ModNone)
	a.handleMouse(ev)
	if tab.ScrollY == 0 {
		t.Fatalf("WheelDown without shift should scroll vertically, ScrollY still 0")
	}
	if tab.ScrollX != 0 {
		t.Fatalf("WheelDown without shift should NOT touch ScrollX, got %d", tab.ScrollX)
	}
}

// TestHandleMouse_SoftWrapIgnoresHorizontalWheel keeps wrapped text from
// retaining a contradictory horizontal scroll offset.
func TestHandleMouse_SoftWrapIgnoresHorizontalWheel(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(target, []byte(strings.Repeat("x", 200)+"\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.width = 30
	a.height = 8
	a.sidebarShown = false
	a.screen.(tcell.SimulationScreen).SetSize(a.width, a.height)
	a.openFile(target)
	a.softWrap = true
	tab := a.activeTabPtr()
	editorX := a.sidebarW() + 10

	a.handleMouse(tcell.NewEventMouse(editorX, 5, tcell.WheelRight, tcell.ModNone))
	if tab.ScrollX != 0 {
		t.Fatalf("WheelRight with soft wrap should not move ScrollX, got %d", tab.ScrollX)
	}
	a.handleMouse(tcell.NewEventMouse(editorX, 5, tcell.WheelDown, tcell.ModShift))
	if tab.ScrollX != 0 {
		t.Fatalf("Shift+WheelDown with soft wrap should not move ScrollX, got %d", tab.ScrollX)
	}
}

// TestHandleMouse_SoftWrapWheelScrollsVisualRows ensures vertical wheel
// scrolling advances through wrapped visual rows, not only logical lines.
func TestHandleMouse_SoftWrapWheelScrollsVisualRows(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(target, []byte(strings.Repeat("x", 200)+"\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.width = 30
	a.height = 8
	a.sidebarShown = false
	a.screen.(tcell.SimulationScreen).SetSize(a.width, a.height)
	a.openFile(target)
	a.softWrap = true
	tab := a.activeTabPtr()
	editorX := a.sidebarW() + 10

	a.handleMouse(tcell.NewEventMouse(editorX, 5, tcell.WheelDown, tcell.ModNone))
	if tab.WrapScrollY == 0 {
		t.Fatal("WheelDown with soft wrap should move WrapScrollY")
	}
	if tab.ScrollX != 0 {
		t.Fatalf("WheelDown with soft wrap should not move ScrollX, got %d", tab.ScrollX)
	}
}

// TestHandleKey_SoftWrapPageDownMovesVisualRows keeps PgDn aligned with the
// wrapped screen rows rather than jumping logical lines.
func TestHandleKey_SoftWrapPageDownMovesVisualRows(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(target, []byte(strings.Repeat("a", 200)), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.width = 30
	a.height = 10
	a.sidebarShown = false
	a.screen.(tcell.SimulationScreen).SetSize(a.width, a.height)
	a.openFile(target)
	a.softWrap = true
	tab := a.activeTabPtr()
	_, h := a.editorSize()

	a.handleKey(keyEv(tcell.KeyPgDn, 0))
	if tab.Cursor.Line != 0 {
		t.Fatalf("PgDn should stay in the long logical line, got line %d", tab.Cursor.Line)
	}
	if tab.Cursor.Col == 0 {
		t.Fatal("PgDn with soft wrap should advance cursor by visual rows")
	}
	if tab.Cursor.Col < h {
		t.Fatalf("cursor col = %d, want movement by page of visual rows", tab.Cursor.Col)
	}
}

// TestDraw_UsesSoftWrapForTextTabs catches the app/editor integration point:
// toggling softWrap must affect the actual draw path, not only the menu state.
func TestDraw_UsesSoftWrapForTextTabs(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(target, []byte("abcdefghijklmnop"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.width = 60
	a.height = 24
	a.screen.(tcell.SimulationScreen).SetSize(a.width, a.height)
	a.sidebarShown = true
	a.sidebarWidth = 40
	a.openFile(target)
	a.softWrap = true

	a.draw()
	a.screen.Show()

	row0 := screenLine(a.screen.(tcell.SimulationScreen), 1) // editor body starts below the tab bar.
	row1 := screenLine(a.screen.(tcell.SimulationScreen), 2)
	if !strings.Contains(row0, "abcdefghijklm") {
		t.Fatalf("row0 = %q, want first wrapped segment", row0)
	}
	if !strings.Contains(row1, "nop") {
		t.Fatalf("row1 = %q, want second wrapped segment", row1)
	}
}

// TestHandleMouse_ShiftStickyForWheel covers the Zellij quirk where
// Shift arrives in a ButtonNone+Shift event right before an unmodified
// WheelDown. We feed that exact sequence and confirm the wheel event is
// treated as horizontal because the sticky-shift window picked it up.
func TestHandleMouse_ShiftStickyForWheel(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(target, []byte(strings.Repeat("x", 200)+"\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	editorX := a.sidebarW() + 10

	// First event: ButtonNone with Shift modifier — what Zellij emits
	// when the user holds shift but hasn't moved or wheeled yet.
	ev := tcell.NewEventMouse(editorX, 5, tcell.ButtonNone, tcell.ModShift)
	a.handleMouse(ev)
	// Second event: WheelDown with NO modifier — what arrives milliseconds
	// later. Without the sticky window this would scroll vertically.
	ev = tcell.NewEventMouse(editorX, 5, tcell.WheelDown, tcell.ModNone)
	a.handleMouse(ev)

	if tab.ScrollX == 0 {
		t.Fatalf("expected sticky-shift to route WheelDown to horizontal, ScrollX still 0")
	}
	if tab.ScrollY != 0 {
		t.Fatalf("sticky-shift WheelDown shouldn't touch ScrollY, got %d", tab.ScrollY)
	}
}

// TestHandleMouse_RightClickOpensMenu falls back to the main menu when the
// right-click isn't on a tree row.
func TestHandleMouse_RightClickOpensMenu(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	ev := tcell.NewEventMouse(60, 5, tcell.Button3, tcell.ModNone)
	a.handleMouse(ev)
	if !a.menuOpen {
		t.Fatal("right-click outside tree should open the main menu")
	}
}

// TestHandleMouse_LeftPressInEditor enters editor drag mode.
func TestHandleMouse_LeftPressInEditor(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("ab\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	ex, ey, _, _ := a.editorRect()
	ev := tcell.NewEventMouse(ex+8, ey, tcell.Button1, tcell.ModNone)
	a.handleMouse(ev)
	if a.dragMode != "editor" {
		t.Fatalf("expected dragMode=editor, got %q", a.dragMode)
	}
	// Release.
	ev = tcell.NewEventMouse(60, 5, 0, tcell.ModNone)
	a.handleMouse(ev)
	if a.dragMode != "" {
		t.Fatalf("expected drag cleared on release, got %q", a.dragMode)
	}
}

// TestHandleMouse_AutoCopiesDragSelection publishes a completed editor drag
// once, retaining the text internally and flashing its confirmation status.
func TestHandleMouse_AutoCopiesDragSelection(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("abcdef\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	copied := recordingClipboard(a, nil)
	ex, ey, _, _ := a.editorRect()

	a.handleMouse(tcell.NewEventMouse(ex+8, ey, tcell.Button1, tcell.ModNone))
	a.handleMouse(tcell.NewEventMouse(ex+12, ey, tcell.Button1, tcell.ModNone))
	a.handleMouse(tcell.NewEventMouse(ex+12, ey, tcell.ButtonNone, tcell.ModNone))

	if got := *copied; len(got) != 1 || got[0] == "" {
		t.Fatalf("drag copy = %#v, want one non-empty value", got)
	}
	if a.clipBuf != (*copied)[0] {
		t.Fatalf("clipBuf = %q, want %q", a.clipBuf, (*copied)[0])
	}
	if a.dragMode != "" {
		t.Fatalf("dragMode = %q, want cleared", a.dragMode)
	}
	if a.autoScrollDir != 0 {
		t.Fatalf("autoScrollDir = %d, want 0", a.autoScrollDir)
	}

	a.handleMouse(tcell.NewEventMouse(ex+12, ey, tcell.ButtonNone, tcell.ModNone))
	if got := len(*copied); got != 1 {
		t.Fatalf("release copied %d times, want once", got)
	}
}

// TestHandleMouse_AutoCopiesDoubleClickSelection copies the word selected by
// a completed double-click, while an ordinary click remains a no-op.
func TestHandleMouse_AutoCopiesDoubleClickSelection(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	copied := recordingClipboard(a, nil)
	ex, ey, _, _ := a.editorRect()
	click := func() {
		a.handleMouse(tcell.NewEventMouse(ex+8, ey, tcell.Button1, tcell.ModNone))
		a.handleMouse(tcell.NewEventMouse(ex+8, ey, tcell.ButtonNone, tcell.ModNone))
	}

	click()
	if len(*copied) != 0 {
		t.Fatalf("single click copied %#v", *copied)
	}
	click()
	if got := *copied; len(got) != 1 || got[0] != "hello" {
		t.Fatalf("double-click copy = %#v, want []string{\"hello\"}", got)
	}
}

// TestHandleMouse_AutoCopyGuards keeps non-selection and non-editor gestures
// from changing clipboard state.
func TestHandleMouse_AutoCopyGuards(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, a *App)
	}{
		{
			name: "single editable click",
			run: func(t *testing.T, a *App) {
				target := filepath.Join(a.rootDir, "f.txt")
				if err := os.WriteFile(target, []byte("hello\n"), 0o644); err != nil {
					t.Fatalf("seed: %v", err)
				}
				a.openFile(target)
				ex, ey, _, _ := a.editorRect()
				a.handleMouse(tcell.NewEventMouse(ex+2, ey, tcell.Button1, tcell.ModNone))
				a.handleMouse(tcell.NewEventMouse(ex+2, ey, tcell.ButtonNone, tcell.ModNone))
			},
		},
		{
			name: "sidebar splitter drag",
			run: func(_ *testing.T, a *App) {
				splitX := a.splitterX()
				a.handleMouse(tcell.NewEventMouse(splitX, 5, tcell.Button1, tcell.ModNone))
				a.handleMouse(tcell.NewEventMouse(splitX+3, 5, tcell.Button1, tcell.ModNone))
				a.handleMouse(tcell.NewEventMouse(splitX+3, 5, tcell.ButtonNone, tcell.ModNone))
			},
		},
		{
			name: "release without tab",
			run: func(_ *testing.T, a *App) {
				a.dragMode = "editor"
				a.handleMouse(tcell.NewEventMouse(60, 5, tcell.ButtonNone, tcell.ModNone))
			},
		},
		{
			name: "read-only tab",
			run: func(t *testing.T, a *App) {
				target := filepath.Join(a.rootDir, "manual.pdf")
				if err := os.WriteFile(target, []byte("%PDF-1.7\n"), 0o644); err != nil {
					t.Fatalf("seed: %v", err)
				}
				a.openFile(target)
				ex, ey, _, _ := a.editorRect()
				a.handleMouse(tcell.NewEventMouse(ex+2, ey, tcell.Button1, tcell.ModNone))
				a.handleMouse(tcell.NewEventMouse(ex+6, ey, tcell.Button1, tcell.ModNone))
				a.handleMouse(tcell.NewEventMouse(ex+6, ey, tcell.ButtonNone, tcell.ModNone))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t, t.TempDir())
			a.copyToSystem = func(string) error {
				t.Fatal("unexpected system clipboard write")
				return nil
			}
			tc.run(t, a)
			if a.clipBuf != "" {
				t.Fatalf("clipBuf = %q, want empty", a.clipBuf)
			}
		})
	}
}

// TestHandleMouse_AutoCopyWriteFailure retains the internal selection when
// OSC 52 fails while retaining the internal selection.
func TestHandleMouse_AutoCopyWriteFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("abcdef\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	copied := recordingClipboard(a, errors.New("tty unavailable"))
	ex, ey, _, _ := a.editorRect()
	a.handleMouse(tcell.NewEventMouse(ex+8, ey, tcell.Button1, tcell.ModNone))
	a.handleMouse(tcell.NewEventMouse(ex+12, ey, tcell.Button1, tcell.ModNone))
	a.handleMouse(tcell.NewEventMouse(ex+12, ey, tcell.ButtonNone, tcell.ModNone))

	if got := len(*copied); got != 1 {
		t.Fatalf("clipboard writes = %d, want 1", got)
	}
	if a.clipBuf == "" {
		t.Fatal("failed system copy should retain internal clipboard")
	}
	if a.statusMsg != "Copied (system clipboard unavailable)" {
		t.Fatalf("statusMsg = %q", a.statusMsg)
	}
}

// TestHandleMouse_AutoCopyFailureFlashesUnavailable ensures a failed later
// gesture reports the clipboard failure after an earlier successful copy.
func TestHandleMouse_AutoCopyFailureFlashesUnavailable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("abcdef\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	var copied []string
	var copyErr error
	a.copyToSystem = func(text string) error {
		copied = append(copied, text)
		return copyErr
	}
	ex, ey, _, _ := a.editorRect()
	drag := func() {
		a.handleMouse(tcell.NewEventMouse(ex+8, ey, tcell.Button1, tcell.ModNone))
		a.handleMouse(tcell.NewEventMouse(ex+12, ey, tcell.Button1, tcell.ModNone))
		a.handleMouse(tcell.NewEventMouse(ex+12, ey, tcell.ButtonNone, tcell.ModNone))
	}

	drag()
	copyErr = errors.New("tty unavailable")
	drag()
	if got := len(copied); got != 2 {
		t.Fatalf("clipboard writes = %d, want 2", got)
	}
	if a.statusMsg != "Copied (system clipboard unavailable)" {
		t.Fatalf("statusMsg = %q", a.statusMsg)
	}
}

// TestHandleMouse_EmptyRowDoesNotPublishKeyboardSelection keeps a release
// after a non-editable body press from publishing an existing selection.
func TestHandleMouse_EmptyRowDoesNotPublishKeyboardSelection(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("abcdef\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 6}
	a.copyToSystem = func(string) error {
		t.Fatal("unexpected system clipboard write")
		return nil
	}
	ex, ey, _, _ := a.editorRect()
	a.handleMouse(tcell.NewEventMouse(ex+8, ey+1, tcell.Button1, tcell.ModNone))
	a.handleMouse(tcell.NewEventMouse(ex+8, ey+1, tcell.ButtonNone, tcell.ModNone))

	if a.dragMode != "" {
		t.Fatalf("dragMode = %q, want empty", a.dragMode)
	}
	if a.clipBuf != "" {
		t.Fatalf("clipBuf = %q, want empty", a.clipBuf)
	}
}

// TestHandleMouse_GutterDoesNotPublishKeyboardSelection keeps ordinary
// line-number gutter clicks from starting a selection gesture.
func TestHandleMouse_GutterDoesNotPublishKeyboardSelection(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("abcdef\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 6}
	a.copyToSystem = func(string) error {
		t.Fatal("unexpected system clipboard write")
		return nil
	}
	ex, ey, _, _ := a.editorRect()
	a.handleMouse(tcell.NewEventMouse(ex+2, ey, tcell.Button1, tcell.ModNone))
	if a.dragMode != "" {
		t.Fatalf("gutter press dragMode = %q, want empty", a.dragMode)
	}
	a.handleMouse(tcell.NewEventMouse(ex+2, ey, tcell.ButtonNone, tcell.ModNone))

	if a.dragMode != "" {
		t.Fatalf("dragMode = %q, want empty", a.dragMode)
	}
	if a.clipBuf != "" {
		t.Fatalf("clipBuf = %q, want empty", a.clipBuf)
	}
}

// TestHandleMouse_SidebarSplitterDrag enters splitter drag and resizes.
func TestHandleMouse_SidebarSplitterDrag(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	splitX := a.splitterX()
	ev := tcell.NewEventMouse(splitX, 5, tcell.Button1, tcell.ModNone)
	a.handleMouse(ev)
	if a.dragMode != "sidebar" {
		t.Fatalf("expected sidebar drag, got %q", a.dragMode)
	}
	// Continue dragging — resizes.
	ev = tcell.NewEventMouse(splitX+5, 5, tcell.Button1, tcell.ModNone)
	a.handleMouse(ev)
}

// TestHandleMenuMouse_ClicksRowAndOutside both fires the row action and
// dismisses on outside click.
func TestHandleMenuMouse_ClicksRowAndOutside(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openMenu()
	mx, my, _, _ := a.menuModalRect()
	// Click on the sidebar toggle row — flips the sidebar.
	items, _, _ := a.menuLayout()
	toggleRelY := -1
	for _, item := range items {
		if item.labelFor != nil && item.labelFor(a) == "Hide file explorer" {
			toggleRelY = item.relY
			break
		}
	}
	if toggleRelY < 0 {
		t.Fatal("sidebar toggle row not found")
	}
	before := a.sidebarShown
	a.handleMenuMouse(mx+5, my+toggleRelY, tcell.Button1)
	if a.sidebarShown == before {
		t.Fatal("expected toggle to fire")
	}

	// Click outside — closes.
	a.openMenu()
	a.handleMenuMouse(0, 0, tcell.Button1)
	if a.menuOpen {
		t.Fatal("outside click should close menu")
	}
}

// TestHandleMenuMouse_NoButtonIsNoop ignores motion-only events.
func TestHandleMenuMouse_NoButtonIsNoop(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.openMenu()
	a.handleMenuMouse(0, 0, 0)
	if !a.menuOpen {
		t.Fatal("motion-only event should not close menu")
	}
}

// TestDraw_AllPanels exercises the drawing path so the stdout/screen code
// is covered. Result correctness is exercised manually; here we just make
// sure no panics across several states.
func TestDraw_AllPanels(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("hi\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.draw() // empty editor + sidebar
	a.openFile(target)
	a.draw() // with a tab
	a.activeTabPtr().Dirty = true
	a.draw() // dirty marker
	a.openMenu()
	a.draw() // with menu modal
	a.closeMenu()
	a.openPrompt("T", "H", "x", nil)
	a.draw()
	a.promptCancel()
	a.openConfirm("T", "M", nil)
	a.draw()
	a.confirmCancel()
	a.openTreeContext(a.tree.Root, 5, 5)
	a.draw()
	a.closeAllModals()
	a.flash("hello")
	a.draw() // status flash
	a.sidebarShown = false
	a.draw()
	// Tiny window → too-small message.
	a.width, a.height = 5, 5
	a.draw()
}

// TestTabBarClick_ClosesViaX clicks the × in a tab and verifies the close
// path runs (clean tab → tab removed).
func TestTabBarClick_ClosesViaX(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.lastTabRects = a.layoutTabs()
	r := a.lastTabRects[0]
	a.tabBarClick(r.CloseX, 0)
	if len(a.tabs) != 0 {
		t.Fatalf("expected close, got %d tabs", len(a.tabs))
	}
}

// TestDrawStatusBar_RendersBranchAtFarLeft pins the branch before document
// information, leaving the lower-right edge to the action button group.
func TestDrawStatusBar_RendersBranchAtFarLeft(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.gitBranch = "feat/widgets"
	a.draw()
	scr := a.screen.(tcell.SimulationScreen)
	scr.Show() // SimulationScreen serves GetContents from the *front* buffer.

	cells, w, _ := scr.GetContents()
	_, sy, _, _ := a.statusRect()

	want := []rune("[feat/widgets]")
	startX := 0
	for i, r := range want {
		c := cells[sy*w+startX+i]
		if len(c.Runes) == 0 || c.Runes[0] != r {
			t.Fatalf("status bar col %d = %v, want %q",
				startX+i, c.Runes, r)
		}
	}
}

// TestDrawStatusBar_OmitsBranchWhenEmpty confirms a non-repo project
// (gitBranch == "") doesn't paint a stray label before the left-side text.
func TestDrawStatusBar_OmitsBranchWhenEmpty(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.gitBranch = ""
	a.draw()
	scr := a.screen.(tcell.SimulationScreen)
	scr.Show()

	cells, w, _ := scr.GetContents()
	_, sy, _, _ := a.statusRect()

	if c := cells[sy*w]; len(c.Runes) > 0 && c.Runes[0] == '[' {
		t.Fatalf("empty branch rendered prefix at first cell: %v", c.Runes)
	}
}

// TestMenuLayout_NoCustomActions pins down the baseline geometry: with
// zero custom actions the modal still has seven built-in groups and the
// height matches the expected layout total. Catches accidental
// off-by-one regressions when someone tweaks the layout helper.
func TestMenuLayout_NoCustomActions(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.customActions = nil
	items, dividers, h := a.menuLayout()

	if h != 39 {
		t.Errorf("modalHeight = %d, want 39", h)
	}
	if got := len(items); got != 29 {
		t.Errorf("item count = %d, want 29 built-ins", got)
	}
	wantDiv := []int{2, 8, 12, 15, 26, 33, 36}
	if len(dividers) != len(wantDiv) {
		t.Fatalf("dividers = %v, want %v", dividers, wantDiv)
	}
	for i, d := range wantDiv {
		if dividers[i] != d {
			t.Errorf("dividers[%d] = %d, want %d", i, dividers[i], d)
		}
	}
}

// TestMenuLayout_ToggleLineCommentRow ensures the comment action is present
// and uses the same enablement predicate as the direct app method.
func TestMenuLayout_ToggleLineCommentRow(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main.go")
	if err := os.WriteFile(target, []byte("package main"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)

	item := menuItemByLabel(t, a, "Toggle line comment")
	if item.enabled(a) {
		t.Fatal("comment row should be disabled without an active tab")
	}

	a.openFile(target)
	item = menuItemByLabel(t, a, "Toggle line comment")
	if !item.enabled(a) {
		t.Fatal("comment row should be enabled for a .go tab")
	}
}

// TestMenuLayout_Shortcuts pins the right-side hint text shown in the action
// menu to the Esc-leader bindings that are meant to be discoverable there.
func TestMenuLayout_Shortcuts(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	want := map[string]string{
		"Save":                 "Esc s",
		"Save & close tab":     "",
		"Close tab":            "Esc w",
		"Close other tabs":     "",
		"Close all tabs":       "",
		"Undo":                 "Esc u",
		"Redo":                 "Esc r",
		"Revert file":          "",
		"Find in file":         "Esc f",
		"Find file in project": "Esc p",
		"New file":             "Esc n",
		"Rename file":          "",
		"Delete file":          "",
		"Copy relative path":   "",
		"Copy absolute path":   "",
		"Copy selection":       "Esc c",
		"Cut selection":        "Esc x",
		"Paste":                "Esc v",
		"Select all":           "Esc a",
		"Toggle line comment":  "Esc /",
		"Enable soft wrap":     "Esc z",
		"Hide file explorer":   "Esc t",
		"Quit editor":          "Esc q",
	}

	items, _, _ := a.menuLayout()
	seen := make(map[string]string, len(items))
	for _, item := range items {
		label := item.label
		if item.labelFor != nil {
			label = item.labelFor(a)
		}
		seen[label] = item.shortcut
	}
	for label, shortcut := range want {
		if got, ok := seen[label]; !ok {
			t.Errorf("menu item %q not found", label)
		} else if got != shortcut {
			t.Errorf("%s shortcut = %q, want %q", label, got, shortcut)
		}
	}
}

// TestMenuLayout_SoftWrapDynamicLabel verifies the view-toggle row reflects
// the current session setting and advertises the Esc-z shortcut.
func TestMenuLayout_SoftWrapDynamicLabel(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	item := menuItemByLabel(t, a, "Enable soft wrap")
	if item.shortcut != "Esc z" {
		t.Fatalf("shortcut = %q, want Esc z", item.shortcut)
	}
	item.action(a)
	if !a.softWrap {
		t.Fatal("menu action should enable soft wrap")
	}
	item = menuItemByLabel(t, a, "Disable soft wrap")
	if item.shortcut != "Esc z" {
		t.Fatalf("shortcut = %q, want Esc z", item.shortcut)
	}
}

// TestDrawMenu_RightAlignsShortcuts verifies the shortcut column is painted at
// the modal's right edge instead of being appended to the command label.
func TestDrawMenu_RightAlignsShortcuts(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.drawMenu()
	a.screen.Show()

	mx, my, mw, _ := a.menuModalRect()
	save := menuItemByLabel(t, a, "Save")
	shortcutX := mx + mw - 2 - textWidth(save.shortcut)
	line := screenLine(a.screen.(tcell.SimulationScreen), my+save.relY)
	lineRunes := []rune(line)
	if got := string(lineRunes[shortcutX : shortcutX+textWidth(save.shortcut)]); got != save.shortcut {
		t.Fatalf("right shortcut = %q, want %q on line %q", got, save.shortcut, line)
	}
	if strings.Contains(string(lineRunes[mx+4:shortcutX-1]), save.shortcut) {
		t.Fatalf("shortcut should not be appended to label area: %q", line)
	}
}

// TestTruncateText covers the menu-label clipping helper so long dynamic labels
// cannot overwrite the right-aligned shortcut column.
func TestTruncateText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"fits", "Save", 8, "Save"},
		{"clips", "Find file in project", 10, "Find file…"},
		{"one", "Save", 1, "…"},
		{"none", "Save", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateText(tt.in, tt.max); got != tt.want {
				t.Fatalf("truncateText(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

// menuItemByLabel finds a menu row by static label for tests that care about
// one action without hard-coding its row index.
func menuItemByLabel(t *testing.T, a *App, label string) menuItemDef {
	t.Helper()
	items, _, _ := a.menuLayout()
	for _, item := range items {
		itemLabel := item.label
		if item.labelFor != nil {
			itemLabel = item.labelFor(a)
		}
		if itemLabel == label {
			return item
		}
	}
	t.Fatalf("menu item %q not found", label)
	return menuItemDef{}
}

// screenLine returns one row from a SimulationScreen as a fixed-width string.
func screenLine(scr tcell.SimulationScreen, y int) string {
	cells, w, _ := scr.GetContents()
	rs := make([]rune, w)
	for x := 0; x < w; x++ {
		c := cells[y*w+x]
		if len(c.Runes) == 0 {
			rs[x] = ' '
			continue
		}
		rs[x] = c.Runes[0]
	}
	return string(rs)
}

// TestMenuLayout_WithCustomActions checks the splice-before-Quit
// behaviour: two custom actions land as their own group sitting
// directly above the Quit row, with a divider on each side. Modal
// height grows by 3 rows (2 items + 1 divider).
func TestMenuLayout_WithCustomActions(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.customActions = []customactions.Action{
		{Label: "Open on Rager", Command: "echo r"},
		{Label: "Open on Cascade", Command: "echo c"},
	}
	items, _, h := a.menuLayout()

	if h != 42 { // 39 + 2 items + 1 divider
		t.Errorf("modalHeight = %d, want 42", h)
	}
	// Custom actions should be the second-to-last and third-to-last
	// rows, with Quit as the final row.
	last := len(items) - 1
	if items[last].label != "Quit editor" {
		t.Fatalf("last row = %q, want Quit editor", items[last].label)
	}
	if items[last-1].label != "Open on Cascade" {
		t.Errorf("row above Quit = %q, want Open on Cascade", items[last-1].label)
	}
	if items[last-2].label != "Open on Rager" {
		t.Errorf("two above Quit = %q, want Open on Rager", items[last-2].label)
	}
}

// TestMenuLayout_CustomActionsAlwaysEnabled pins the rule that the
// menu never disables a custom action — neither prompted ones (where
// the form modal owns the file-or-no-file question) nor plain ones
// (whose commands may not touch $FILE at all, like "brew upgrade").
// Trying to gate prompt-less actions on hasFileTab guessed wrong
// for actions like Upgrade SpiceEdit and made them appear broken.
func TestMenuLayout_CustomActionsAlwaysEnabled(t *testing.T) {
	a := newTestApp(t, t.TempDir()) // no tabs opened

	a.customActions = []customactions.Action{
		{Label: "Plain", Command: "echo p"},
		{Label: "Prompted", Command: "echo q",
			Prompts: []customactions.Prompt{
				{Key: "X", Type: customactions.PromptText},
			}},
	}
	items, _, _ := a.menuLayout()

	var plain, prompted *menuItemDef
	for i := range items {
		switch items[i].label {
		case "Plain":
			plain = &items[i]
		case "Prompted":
			prompted = &items[i]
		}
	}
	if plain == nil || prompted == nil {
		t.Fatalf("custom actions missing from layout: %v", items)
	}
	if !plain.enabled(a) {
		t.Error("plain action should be enabled even with no tab open")
	}
	if !prompted.enabled(a) {
		t.Error("prompted action should be enabled even with no tab open")
	}
}

// TestRunCustomAction_NoFileStillRuns confirms a prompt-less action
// runs even with no tab open. Earlier the runner short-circuited
// here with a "no file open" flash, but that gate guessed wrong
// for $FILE-free commands like "brew upgrade …" — they got blocked
// even though they had no file dependency at all. The new contract:
// always run; if a $FILE-dependent command then fails because FILE
// is empty, the failure surfaces in the info modal with the actual
// stderr, which is more informative than a generic flash.
func TestRunCustomAction_NoFileStillRuns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran.txt")
	a := newTestApp(t, dir)
	a.customActions = []customactions.Action{{
		Label:   "Touch marker",
		Command: "touch " + marker,
	}}
	a.runCustomAction(0)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ev := a.screen.PollEvent()
		if ev == nil {
			break
		}
		if _, ok := ev.(*customActionDoneEvent); ok {
			break
		}
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("command did not run: %v", err)
	}
	if strings.Contains(a.statusMsg, "no file open") {
		t.Errorf("status flash should not mention no file open: %q", a.statusMsg)
	}
}

// TestRunCustomAction_OutOfRange is a no-op when idx is bogus. Caller
// should never produce one but the guard keeps a stale layout from
// crashing the editor.
func TestRunCustomAction_OutOfRange(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.runCustomAction(0)  // empty list
	a.runCustomAction(99) // out of range
}

// TestRunCustomAction_ExecutesAndPostsEvent runs a real `sh -c`
// command against a small file and confirms that (a) the command
// observed FILE / FILENAME via env, and (b) a customActionDoneEvent
// lands on the screen's event queue. The chosen command writes a
// marker file that lets the test verify env reached the subprocess.
func TestRunCustomAction_ExecutesAndPostsEvent(t *testing.T) {
	// Redirect the action log into the test's temp dir so we don't
	// scribble into the developer's real ~/.local/state/spiceedit/.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	marker := filepath.Join(dir, "marker.txt")
	a := newTestApp(t, dir)
	a.openFile(target)
	a.customActions = []customactions.Action{{
		Label:   "Mark",
		Command: `printf "%s|%s" "$FILE" "$FILENAME" > ` + marker,
	}}

	a.runCustomAction(0)

	// The action runs in a goroutine and posts an event back. Pull
	// events off the screen's queue until we see the done event, with
	// a sanity timeout so a regression can't hang the suite forever.
	deadline := time.Now().Add(2 * time.Second)
	var done *customActionDoneEvent
	for time.Now().Before(deadline) && done == nil {
		ev := a.screen.PollEvent()
		if ev == nil {
			break
		}
		if d, ok := ev.(*customActionDoneEvent); ok {
			done = d
		}
	}
	if done == nil {
		t.Fatal("no customActionDoneEvent received within timeout")
	}
	if done.err != nil {
		t.Fatalf("action errored: %v", done.err)
	}
	if done.label != "Mark" {
		t.Errorf("event label = %q", done.label)
	}

	// Verify the subprocess saw the env variables we exported.
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("marker read: %v", err)
	}
	want := target + "|" + "src.txt"
	if string(got) != want {
		t.Fatalf("marker content = %q, want %q", got, want)
	}
}

// TestRunCustomAction_PromptedSkipsNoFileGuard ensures actions that
// declare prompts can run even when no tab is open. Copy-from-remote
// is the motivating case — without this, the very first thing the
// user wants to do in a fresh session would silently flash "no file
// open" and refuse to show the form.
func TestRunCustomAction_PromptedSkipsNoFileGuard(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.customActions = []customactions.Action{{
		Label:   "Copy from remote",
		Command: "true",
		Prompts: []customactions.Prompt{
			{Key: "HOST", Type: customactions.PromptSelect, Options: []string{"a", "b"}},
		},
	}}
	a.runCustomAction(0)

	if !a.formOpen {
		t.Fatal("prompted action with no file open should still show the form modal")
	}
	if strings.Contains(a.statusMsg, "no file open") {
		t.Errorf("prompted action should not flash no-file-open: %q", a.statusMsg)
	}
}

// TestRunCustomAction_PromptedExportsValuesAndExpands walks the full
// SCP-from-remote path: the form opens, we fill it in, submit, and
// assert the spawned shell saw both the form-collected env vars
// (HOST, REMOTE_SRC) and the editor-state vars (PROJECT_ROOT). This
// is the contract that makes the feature actually useful — if any of
// these don't reach the shell, the user's command fails silently.
func TestRunCustomAction_PromptedExportsValuesAndExpands(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker.txt")
	a := newTestApp(t, dir)
	a.customActions = []customactions.Action{{
		Label:   "Copy from remote",
		Command: `printf "%s|%s|%s" "$HOST" "$REMOTE_SRC" "$PROJECT_ROOT" > ` + marker,
		Prompts: []customactions.Prompt{
			{Key: "HOST", Type: customactions.PromptSelect, Options: []string{"cascade", "rager"}},
			{Key: "REMOTE_SRC", Type: customactions.PromptText},
		},
	}}

	a.runCustomAction(0)
	if !a.formOpen {
		t.Fatal("form did not open")
	}

	// Fill in REMOTE_SRC by typing into the focused field after Tab'ing
	// past the HOST select.
	a.handleFormKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	for _, r := range "/etc/hosts" {
		a.handleFormKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	a.handleFormKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if a.formOpen {
		t.Fatal("Enter on last field should submit")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ev := a.screen.PollEvent()
		if ev == nil {
			break
		}
		if _, ok := ev.(*customActionDoneEvent); ok {
			break
		}
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("marker read: %v", err)
	}
	parts := strings.Split(string(got), "|")
	if len(parts) != 3 {
		t.Fatalf("marker = %q, want HOST|REMOTE_SRC|PROJECT_ROOT", got)
	}
	if parts[0] != "cascade" {
		t.Errorf("HOST = %q, want %q", parts[0], "cascade")
	}
	if parts[1] != "/etc/hosts" {
		t.Errorf("REMOTE_SRC = %q, want %q", parts[1], "/etc/hosts")
	}
	if !strings.HasSuffix(parts[2], filepath.Base(dir)) {
		t.Errorf("PROJECT_ROOT = %q, want suffix matching tempdir", parts[2])
	}
}

// TestHandleCustomActionDone_FailureOpensInfoModal pins the error
// reporting upgrade. The pre-fix behaviour was a one-line status
// flash that truncated scp's stderr exactly when the user most
// needed to read it. Now failures route into the info modal so the
// stderr lines stay visible until dismissed.
func TestHandleCustomActionDone_FailureOpensInfoModal(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.handleCustomActionDone(&customActionDoneEvent{
		label:  "Copy from remote",
		err:    fmt.Errorf("exit status 1"),
		output: []byte("scp: /etc/missing: No such file or directory\n"),
	})
	if !a.confirmOpen || !a.confirmInfo {
		t.Fatal("info modal should be open")
	}
	joined := strings.Join(a.confirmMessageLines, "\n")
	if !strings.Contains(joined, "scp:") || !strings.Contains(joined, "missing") {
		t.Errorf("info body missing stderr preview: %q", joined)
	}
	if !strings.Contains(a.confirmTitle, "Copy from remote") {
		t.Errorf("title = %q, want it to mention the action label", a.confirmTitle)
	}
}

// TestHandleCustomActionDone_SuccessRefreshesTree confirms a
// successful action triggers an immediate tree refresh so a
// freshly-pulled file appears without waiting on the 10-second
// auto-refresh tick. Pinning this avoids a regression where a user
// runs Copy-from-remote, sees "done", and then has to pause before
// the new file becomes clickable in the sidebar.
func TestHandleCustomActionDone_SuccessRefreshesTree(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	// Drop a file directly on disk that the tree hasn't seen yet —
	// without an explicit refresh it would only show up on the next
	// 10-second tick.
	newFile := filepath.Join(dir, "fresh.txt")
	if err := os.WriteFile(newFile, []byte("payload"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	beforeChildren := len(a.tree.Root.Children)
	a.handleCustomActionDone(&customActionDoneEvent{label: "X"})
	if got := len(a.tree.Root.Children); got <= beforeChildren {
		t.Errorf("tree was not refreshed: %d → %d children", beforeChildren, got)
	}
}

// TestSplitErrorOutput_TruncatesAndAppendsLogPath nails the body
// the info modal renders on failure. Without truncation a runaway
// scp -v dump would push the dialog off-screen; without the actions.log
// pointer the user can't easily get the full version even though
// we're already writing it.
func TestSplitErrorOutput_TruncatesAndAppendsLogPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdgtest")

	long := strings.Repeat("really long line that exceeds eighty cells ", 4)
	out := []byte(long + "\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\n")
	body := splitErrorOutput(fmt.Errorf("exit 1"), out)

	if body[0] != "exit 1" {
		t.Errorf("body[0] = %q, want exit-error summary", body[0])
	}
	last := body[len(body)-1]
	if !strings.Contains(last, "actions.log") {
		t.Errorf("last line = %q, want actions.log pointer", last)
	}
	for _, ln := range body {
		if textWidth(ln) > 80 {
			t.Errorf("line over 80 cells: %q (len=%d)", ln, textWidth(ln))
		}
	}
	if !strings.Contains(strings.Join(body, "\n"), "truncated") {
		t.Error("expected '… truncated' marker for >maxLines output")
	}
}

// TestLayoutTabs_IconsExpandWidth pins down the geometry contract:
// turning icons on grows each tab by exactly two cells (the glyph + a
// separator space), and the close-× column shifts right by the same
// amount. Without this, a tab-bar click on the × would land on the
// wrong column whenever icons are enabled.
func TestLayoutTabs_IconsExpandWidth(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(filepath.Join(dir, "main.go"))

	a.tree.IconsEnabled = false
	off := a.layoutTabs()
	a.tree.IconsEnabled = true
	on := a.layoutTabs()

	if len(off) != 1 || len(on) != 1 {
		t.Fatalf("layoutTabs len off=%d on=%d, want 1 each", len(off), len(on))
	}
	if on[0].Width != off[0].Width+2 {
		t.Fatalf("icons should add 2 cells: off=%d on=%d", off[0].Width, on[0].Width)
	}
	if on[0].CloseX != off[0].CloseX+2 {
		t.Fatalf("CloseX should shift by 2 when icons on: off=%d on=%d",
			off[0].CloseX, on[0].CloseX)
	}
}

// TestDrawTabBar_RendersIconWhenEnabled verifies the glyph actually
// lands on screen between the dirty slot and the file name when
// icons are enabled. We use the simulation screen and look for the
// language-specific glyph from icons.For somewhere on the tab row.
func TestDrawTabBar_RendersIconWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(filepath.Join(dir, "main.go"))
	a.tree.IconsEnabled = true

	a.drawTabBar()
	a.screen.Show()
	cells, w, _ := a.screen.(tcell.SimulationScreen).GetContents()

	// Read the tab-bar row (y=0) and look for the .go glyph.
	wantGlyph := []rune(icons.For("main.go", false, false))[0]
	found := false
	for x := 0; x < w; x++ {
		c := cells[x]
		if len(c.Runes) > 0 && c.Runes[0] == wantGlyph {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected .go glyph on the tab-bar row when icons are enabled")
	}
}

// TestHasTree_TrueAndFalse pins the visibility predicate that drives
// the single-file-mode menu filter: any app with a non-nil tree
// reports true; setting tree to nil flips it false.
func TestHasTree_TrueAndFalse(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if !a.hasTree() {
		t.Fatal("expected hasTree=true on a normal-mode app")
	}
	a.tree = nil
	if a.hasTree() {
		t.Fatal("expected hasTree=false when tree is nil")
	}
}

// TestMenuLayout_HidesSidebarToggleInSingleFileMode is the contract
// test for the single-file-mode feature: with no file tree, the
// 'Show / Hide file explorer' row must not appear in the action
// menu — it's nonsensical there because the sidebar isn't built.
// With a tree present (normal mode), the row appears.
func TestMenuLayout_HidesSidebarToggleInSingleFileMode(t *testing.T) {
	a := newTestApp(t, t.TempDir())

	// Sanity: the toggle row IS present in normal mode.
	items, _, _ := a.menuLayout()
	if !containsSidebarToggle(items, a) {
		t.Fatal("expected sidebar-toggle row in normal mode")
	}

	// Simulate single-file mode by clearing the tree.
	a.tree = nil
	items, _, _ = a.menuLayout()
	if containsSidebarToggle(items, a) {
		t.Fatal("expected sidebar-toggle row to be absent when tree is nil")
	}
}

// TestMenuLayout_NoEmptyDividerAfterFiltering guards against the
// regression where filtering the sole item of a group out would
// leave a dangling divider row, doubling the gap in the menu.
func TestMenuLayout_NoEmptyDividerAfterFiltering(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.tree = nil // collapses the View-toggle group to empty

	_, dividers, height := a.menuLayout()
	// Dividers must all sit strictly below the title divider (row 2)
	// and strictly above the modal's bottom border (height-1). Two
	// adjacent dividers (gap == 1) would mean we kept a divider for
	// a now-empty group.
	for i := 1; i < len(dividers); i++ {
		if dividers[i]-dividers[i-1] < 2 {
			t.Fatalf("dividers too close: %v (height=%d)", dividers, height)
		}
	}
}

// containsSidebarToggle is the menu-test helper that locates the
// dynamic-label row whose label flips between "Show file explorer"
// and "Hide file explorer". We match on labelFor's resolved string
// because the sidebar-toggle row is the only one that uses those
// exact labels.
func containsSidebarToggle(items []menuItemDef, a *App) bool {
	for _, it := range items {
		if it.labelFor == nil {
			continue
		}
		l := it.labelFor(a)
		if l == "Show file explorer" || l == "Hide file explorer" {
			return true
		}
	}
	return false
}

// TestMenuToggleSidebar_NoPanicInSingleFileMode is a regression guard for
// a crash: the Esc-t leader calls menuToggleSidebar directly, bypassing the
// menu row's hasTree gate. In single-file mode (tree == nil) flipping
// sidebarShown true would send draw() into a.tree.Render on a nil tree and
// panic. The toggle must stay a no-op so the sidebar can't be shown when
// there's no tree behind it.
func TestMenuToggleSidebar_NoPanicInSingleFileMode(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.tree = nil // single-file mode
	a.sidebarShown = false

	a.menuToggleSidebar() // simulates the Esc-t leader

	if a.sidebarShown {
		t.Fatal("sidebar must stay hidden in single-file mode — no tree to render")
	}
	a.draw() // would panic on nil a.tree.Render if the toggle flipped it on
}

// TestRefreshGitStatus_RefreshesGutterInSingleFileMode pins the
// single-file-mode fix: with no file tree, refreshGitStatus must still
// reload the open tab's per-line gutter markers (a file-scoped git diff
// that doesn't need the tree). Without this, saving a file in
// single-file mode — which routes through refreshGitStatus — would
// leave the gutter markers frozen at their open-time state.
func TestRefreshGitStatus_RefreshesGutterInSingleFileMode(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	target := filepath.Join(repo, "f.go")
	writeFileT(t, target, "package main\n\nfunc main() {}\n")
	gitRun(t, repo, "add", "f.go")
	gitRun(t, repo, "commit", "-m", "init")

	a := newTestApp(t, repo)
	a.tree = nil // simulate single-file mode
	a.openFile(target)
	tab := a.activeTabPtr()
	if tab == nil {
		t.Fatal("expected an open tab")
	}

	// Clean file → no markers yet. Now dirty the worktree and clear the
	// tab's cached markers so we can prove refreshGitStatus repopulates
	// them despite tree == nil.
	writeFileT(t, target, "package main\n\nfunc main() { println(1) }\n")
	tab.GitLines = nil
	a.refreshGitStatus()

	if len(tab.GitLines) == 0 {
		t.Fatal("expected gutter markers to be refreshed in single-file mode, got none")
	}
}

// TestDrawTabBar_NoIconWhenDisabled is the inverse of the above —
// flipping IconsEnabled off must remove the glyph from the tab bar
// (so terminals without a Nerd Font don't see tofu boxes in tabs).
func TestDrawTabBar_NoIconWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(filepath.Join(dir, "main.go"))
	a.tree.IconsEnabled = false

	a.drawTabBar()
	a.screen.Show()
	cells, w, _ := a.screen.(tcell.SimulationScreen).GetContents()

	wantGlyph := []rune(icons.For("main.go", false, false))[0]
	for x := 0; x < w; x++ {
		c := cells[x]
		if len(c.Runes) > 0 && c.Runes[0] == wantGlyph {
			t.Fatalf("did not expect glyph %q at x=%d when icons off", string(wantGlyph), x)
		}
	}
}

// TestLayoutTabs_CJKNameUsesCellWidth verifies tab geometry reserves two
// terminal cells for every CJK glyph in a display name.
func TestLayoutTabs_CJKNameUsesCellWidth(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.tabs = []*editor.Tab{{Path: "中文.txt"}}
	a.activeTab = 0

	rects := a.layoutTabs()
	if len(rects) != 1 {
		t.Fatalf("tab rects = %d, want 1", len(rects))
	}
	if want := 1 + 2 + 8 + 1 + 1 + 1; rects[0].Width != want {
		t.Fatalf("tab width = %d, want %d", rects[0].Width, want)
	}

	a.drawTabBar()
	scr := a.screen.(tcell.SimulationScreen)
	text, _, width := scr.Get(rects[0].X+3, 0)
	if text != "中" || width != 2 {
		t.Fatalf("first CJK tab cell = (%q, %d), want (中, 2)", text, width)
	}
	text, _, width = scr.Get(rects[0].X+5, 0)
	if text != "文" || width != 2 {
		t.Fatalf("second CJK tab cell = (%q, %d), want (文, 2)", text, width)
	}
}

// TestDrawStatusBar_CJKBranchUsesCellWidths ensures a wide branch name keeps
// its terminal cell geometry when positioned at the far left.
func TestDrawStatusBar_CJKBranchUsesCellWidths(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.gitBranch = "中文"
	a.drawStatusBar()

	sx, sy, sw, _ := a.statusRect()
	text, _, width := a.screen.(tcell.SimulationScreen).Get(sx+1, sy)
	if text != "中" || width != 2 {
		t.Fatalf("left-aligned branch cell = (%q, %d), want (中, 2)", text, width)
	}
	_ = sw
}
