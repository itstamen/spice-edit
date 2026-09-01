// =============================================================================
// File: internal/app/externalfile_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
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

// clickTreePath renders the file tree, locates path in its hit-test rows, and
// dispatches the same sidebar click used by the production mouse handler.
func clickTreePath(t *testing.T, a *App, path string) {
	t.Helper()
	a.draw()
	sx, sy, _, sh := a.sidebarRect()
	for y := sy; y < sy+sh; y++ {
		node, ok := a.tree.HitTest(1, y-sy)
		if ok && node.Path == path {
			a.sidebarClick(sx+1, y)
			return
		}
	}
	t.Fatalf("tree path %q was not visible", path)
}

// rightClickTreePath dispatches a tree context click for one visible path.
func rightClickTreePath(t *testing.T, a *App, path string) {
	t.Helper()
	a.draw()
	sx, sy, _, sh := a.sidebarRect()
	for y := sy; y < sy+sh; y++ {
		node, ok := a.tree.HitTest(1, y-sy)
		if ok && node.Path == path {
			if !a.tryTreeContextClick(sx+1, y) {
				t.Fatalf("right-click tree path %q was not handled", path)
			}
			return
		}
	}
	t.Fatalf("tree path %q was not visible", path)
}

// activateContextItem selects and runs the named context-menu item just as
// keyboard activation would. It keeps tests focused on the menu callback
// rather than its drawing coordinates.
func activateContextItem(t *testing.T, a *App, label string) {
	t.Helper()
	for i, item := range a.contextItems {
		if item.label != label {
			continue
		}
		if !item.isEnabled(a) {
			t.Fatalf("context item %q is disabled", label)
		}
		a.contextHover = i
		a.contextActivate()
		return
	}
	t.Fatalf("context item %q not found", label)
}

// assertDefaultAppNotOpened gives the asynchronous opener enough time to run
// and fails when a confirmation or inline-open path launches it unexpectedly.
func assertDefaultAppNotOpened(t *testing.T, opened <-chan string) {
	t.Helper()
	select {
	case got := <-opened:
		t.Fatalf("default application opened unexpectedly: %s", got)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestSidebarClick_ExternalRequiresConfirmation verifies that a tree click
// opens confirmation without creating a placeholder and only launches after
// an explicit Yes.
func TestSidebarClick_ExternalRequiresConfirmation(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "manual.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	opened := make(chan string, 1)
	a.openPath = func(path string) error {
		opened <- path
		return nil
	}

	clickTreePath(t, a, target)

	if !a.confirmOpen || a.confirmHover != 0 {
		t.Fatalf("confirm open=%v hover=%d, want open with No selected", a.confirmOpen, a.confirmHover)
	}
	if a.confirmTitle != "Open with default app?" {
		t.Fatalf("confirm title = %q", a.confirmTitle)
	}
	if !strings.Contains(a.confirmMessage, "manual.pdf") {
		t.Fatalf("confirm message = %q", a.confirmMessage)
	}
	if len(a.tabs) != 0 || a.activeTabPtr() != nil {
		t.Fatalf("external tree click created a tab: count=%d active=%#v", len(a.tabs), a.activeTabPtr())
	}
	if a.activeFolder != dir || a.tree.ActiveFile != target {
		t.Fatalf("tree context = folder %q file %q, want %q %q", a.activeFolder, a.tree.ActiveFile, dir, target)
	}
	assertDefaultAppNotOpened(t, opened)

	a.confirmHover = 1
	a.confirmYes()
	select {
	case got := <-opened:
		if got != target {
			t.Fatalf("opened path = %q, want %q", got, target)
		}
	case <-time.After(time.Second):
		t.Fatal("opener did not run after confirmation")
	}
	a.handleEvent(waitForDefaultOpenDone(t, a))
	if len(a.tabs) != 0 || a.activeTabPtr() != nil {
		t.Fatalf("confirm Yes created a tab: count=%d active=%#v", len(a.tabs), a.activeTabPtr())
	}
}

// TestSidebarClick_ExternalCancelAndRepeat keeps the current text tab active
// after No and requires a fresh confirmation on every later tree click.
func TestSidebarClick_ExternalCancelAndRepeat(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "manual.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	opened := make(chan string, 1)
	a.openPath = func(path string) error {
		opened <- path
		return nil
	}
	a.openFile(textPath)
	original := a.activeTabPtr()

	clickTreePath(t, a, target)
	a.confirmCancel()
	if a.confirmOpen {
		t.Fatal("confirmation remained open after cancel")
	}
	if len(a.tabs) != 1 || a.activeTabPtr() != original {
		t.Fatalf("cancel changed tabs: count=%d active=%#v want=%#v", len(a.tabs), a.activeTabPtr(), original)
	}
	if a.tree.SelectedPath != target {
		t.Fatalf("cancelled external selection = %q, want %q", a.tree.SelectedPath, target)
	}
	assertDefaultAppNotOpened(t, opened)

	clickTreePath(t, a, target)
	if !a.confirmOpen || len(a.tabs) != 1 || a.activeTabPtr() != original {
		t.Fatalf("repeat changed editor state: confirm=%v tabs=%d active=%#v", a.confirmOpen, len(a.tabs), a.activeTabPtr())
	}
	a.confirmCancel()
	assertDefaultAppNotOpened(t, opened)
}

// TestTreeClicks_SelectExactNode checks that both mouse buttons select the
// clicked folder or supported/unsupported file without right-clicking into a
// different editor tab.
func TestTreeClicks_SelectExactNode(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "docs")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	imagePath := writeImageFixture(t, dir, "photo.png")
	externalPath := filepath.Join(dir, "manual.pdf")
	if err := os.WriteFile(externalPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)

	clickTreePath(t, a, folder)
	if a.tree.SelectedPath != folder {
		t.Fatalf("folder selection = %q, want %q", a.tree.SelectedPath, folder)
	}
	clickTreePath(t, a, textPath)
	if a.tree.SelectedPath != textPath {
		t.Fatalf("text selection = %q, want %q", a.tree.SelectedPath, textPath)
	}
	clickTreePath(t, a, imagePath)
	if a.tree.SelectedPath != imagePath {
		t.Fatalf("image selection = %q, want %q", a.tree.SelectedPath, imagePath)
	}

	active := a.activeTabPtr()
	tabCount := len(a.tabs)
	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "folder", path: folder},
		{name: "text", path: textPath},
		{name: "image", path: imagePath},
		{name: "unsupported", path: externalPath},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rightClickTreePath(t, a, tt.path)
			if a.tree.SelectedPath != tt.path {
				t.Fatalf("right-click selection = %q, want %q", a.tree.SelectedPath, tt.path)
			}
			if len(a.tabs) != tabCount || a.activeTabPtr() != active {
				t.Fatal("right-click should not activate or open a tab")
			}
		})
	}
}

// TestSidebarClick_TextAndImageOpenTabs ensures supported inline file types
// retain their existing editable and preview tabs.
func TestSidebarClick_TextAndImageOpenTabs(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	imagePath := writeImageFixture(t, dir, "photo.png")
	a := newTestApp(t, dir)
	opened := make(chan string, 1)
	a.openPath = func(path string) error {
		opened <- path
		return nil
	}

	clickTreePath(t, a, textPath)
	if a.confirmOpen || len(a.tabs) != 1 || a.activeTabPtr().IsReadOnly() {
		t.Fatalf("text tree click confirm=%v tabs=%d readOnly=%v", a.confirmOpen, len(a.tabs), a.activeTabPtr().IsReadOnly())
	}

	clickTreePath(t, a, imagePath)
	if a.confirmOpen || len(a.tabs) != 2 || !a.activeTabPtr().IsImage() {
		t.Fatalf("image tree click confirm=%v tabs=%d image=%v", a.confirmOpen, len(a.tabs), a.activeTabPtr().IsImage())
	}
	assertDefaultAppNotOpened(t, opened)
}

// TestSidebarClick_ExistingExternalTabIsNotActivated keeps tree confirmation
// from switching to a placeholder created by a non-tree entry point.
func TestSidebarClick_ExistingExternalTabIsNotActivated(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "manual.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatal(err)
	}
	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	opened := make(chan string, 2)
	a.openPath = func(path string) error {
		opened <- path
		return nil
	}

	a.openFile(target)
	if got := <-opened; got != target {
		t.Fatalf("initial opened path = %q, want %q", got, target)
	}
	a.handleEvent(waitForDefaultOpenDone(t, a))
	a.openFile(textPath)
	original := a.activeTabPtr()

	clickTreePath(t, a, target)
	if !a.confirmOpen {
		t.Fatal("tree click did not open confirmation")
	}
	if len(a.tabs) != 2 || a.activeTabPtr() != original {
		t.Fatalf("tree click activated external placeholder: tabs=%d active=%#v", len(a.tabs), a.activeTabPtr())
	}
	assertDefaultAppNotOpened(t, opened)
}

// TestUnsupportedFileContextActions_TargetClickedNode proves file-tree
// actions retain their clicked-node target rather than falling back to the
// active tab. Unsupported files never need to become tabs to be manageable.
func TestUnsupportedFileContextActions_TargetClickedNode(t *testing.T) {
	dir := t.TempDir()
	activePath := filepath.Join(dir, "active.txt")
	if err := os.WriteFile(activePath, []byte("active"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{
		"open":     filepath.Join(dir, "open.pdf"),
		"rename":   filepath.Join(dir, "rename.pdf"),
		"delete":   filepath.Join(dir, "delete.pdf"),
		"relative": filepath.Join(dir, "relative.pdf"),
		"absolute": filepath.Join(dir, "absolute.pdf"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("%PDF-1.7"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a := newTestApp(t, dir)
	a.openFile(activePath)
	active := a.activeTabPtr()
	opened := make(chan string, 1)
	a.openPath = func(path string) error {
		opened <- path
		return nil
	}

	rightClickTreePath(t, a, paths["open"])
	activateContextItem(t, a, "Open with default app")
	if a.confirmOpen || len(a.tabs) != 1 || a.activeTabPtr() != active {
		t.Fatal("context default-open changed tab state or asked for confirmation")
	}
	select {
	case got := <-opened:
		if got != paths["open"] {
			t.Fatalf("opened path = %q, want clicked node %q", got, paths["open"])
		}
	case <-time.After(time.Second):
		t.Fatal("context default-open did not invoke opener")
	}
	a.handleEvent(waitForDefaultOpenDone(t, a))

	rightClickTreePath(t, a, paths["rename"])
	activateContextItem(t, a, "Rename file")
	if !a.promptOpen {
		t.Fatal("rename action did not open prompt")
	}
	renamed := filepath.Join(dir, "renamed.pdf")
	a.promptValue = []rune(filepath.Base(renamed))
	a.promptCursor = len(a.promptValue)
	a.promptSubmit()
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("renamed clicked node missing: %v", err)
	}
	if _, err := os.Stat(paths["rename"]); !os.IsNotExist(err) {
		t.Fatalf("original clicked node still exists: %v", err)
	}
	if len(a.tabs) != 1 || a.activeTabPtr() != active {
		t.Fatal("rename changed active tab")
	}

	rightClickTreePath(t, a, paths["delete"])
	activateContextItem(t, a, "Delete file")
	if !a.confirmOpen || a.confirmTitle != "Delete file" {
		t.Fatalf("delete confirm = open %v title %q", a.confirmOpen, a.confirmTitle)
	}
	a.confirmHover = 1
	a.confirmYes()
	if _, err := os.Stat(paths["delete"]); !os.IsNotExist(err) {
		t.Fatalf("clicked node was not deleted: %v", err)
	}
	if len(a.tabs) != 1 || a.activeTabPtr() != active {
		t.Fatal("delete changed active tab")
	}

	var copied []string
	a.copyToSystem = func(path string) error {
		copied = append(copied, path)
		return nil
	}
	rightClickTreePath(t, a, paths["relative"])
	activateContextItem(t, a, "Copy relative path")
	rightClickTreePath(t, a, paths["absolute"])
	activateContextItem(t, a, "Copy absolute path")
	wantCopied := []string{filepath.Base(paths["relative"]), paths["absolute"]}
	if len(copied) != len(wantCopied) {
		t.Fatalf("clipboard writes = %v, want %v", copied, wantCopied)
	}
	for i, want := range wantCopied {
		if copied[i] != want {
			t.Errorf("clipboard write %d = %q, want clicked node %q", i, copied[i], want)
		}
	}
	if len(a.tabs) != 1 || a.activeTabPtr() != active {
		t.Fatal("copy action changed active tab")
	}
}

// TestOpenFile_ExternalInvokesOpenerAndReusesTab covers automatic handoff on
// first open and repeated explicit selection without duplicate tabs.
func TestOpenFile_ExternalInvokesOpenerAndReusesTab(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "manual.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	opened := make(chan string, 2)
	a.openPath = func(path string) error {
		opened <- path
		return nil
	}

	a.openFile(target)
	if got := <-opened; got != target {
		t.Fatalf("opened path = %q, want %q", got, target)
	}
	a.handleEvent(waitForDefaultOpenDone(t, a))
	if len(a.tabs) != 1 || !a.activeTabPtr().IsExternal() {
		t.Fatalf("tabs = %d, external=%v", len(a.tabs), a.activeTabPtr().IsExternal())
	}

	a.openFile(target)
	if got := <-opened; got != target {
		t.Fatalf("reopened path = %q, want %q", got, target)
	}
	a.handleEvent(waitForDefaultOpenDone(t, a))
	if len(a.tabs) != 1 {
		t.Fatalf("reopening external file created %d tabs", len(a.tabs))
	}
}

// TestOpenFile_TextAndImageStayInline ensures automatic desktop handoff is
// limited to unsupported files.
func TestOpenFile_TextAndImageStayInline(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	imagePath := writeImageFixture(t, dir, "photo.png")
	a := newTestApp(t, dir)
	opened := make(chan string, 1)
	a.openPath = func(path string) error {
		opened <- path
		return nil
	}

	a.openFile(textPath)
	a.openFile(imagePath)
	select {
	case got := <-opened:
		t.Fatalf("inline file launched external opener: %s", got)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestMenuOpenWithDefaultAppPreservesDirtyBuffer verifies explicit external
// opening uses the disk path without saving or resetting editor state.
func TestMenuOpenWithDefaultAppPreservesDirtyBuffer(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(target, []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.activeTabPtr().InsertString("dirty")
	opened := make(chan string, 1)
	a.openPath = func(path string) error {
		opened <- path
		return nil
	}

	a.menuOpenWithDefaultApp()
	if got := <-opened; got != target {
		t.Fatalf("opened path = %q, want %q", got, target)
	}
	a.handleEvent(waitForDefaultOpenDone(t, a))
	if !a.activeTabPtr().Dirty {
		t.Fatal("external open cleared dirty state")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "disk" {
		t.Fatalf("disk contents = %q, want unchanged", got)
	}
}

// TestExternalTabPredicatesAndStatus locks down the read-only action surface
// while retaining path-based file operations.
func TestExternalTabPredicatesAndStatus(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "manual.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.openPath = func(string) error { return nil }
	a.openFile(target)
	a.handleEvent(waitForDefaultOpenDone(t, a))

	if a.hasSavableTab() || a.hasFindable() || a.hasCommentableTab() {
		t.Fatal("external tab exposed editable-text actions")
	}
	if !a.hasFileTab() {
		t.Fatal("external tab should retain path-based file actions")
	}

}

// TestMenuLayout_OpenWithDefaultApp checks discoverability and enablement.
func TestMenuLayout_OpenWithDefaultApp(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	item := menuItemByLabel(t, a, "Open with default app")
	if item.shortcut != "Esc o" {
		t.Fatalf("shortcut = %q, want Esc o", item.shortcut)
	}
	if item.enabled(a) {
		t.Fatal("open-with-default should be disabled without a file tab")
	}

	target := filepath.Join(a.rootDir, "notes.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFile(target)
	item = menuItemByLabel(t, a, "Open with default app")
	if !item.enabled(a) {
		t.Fatal("open-with-default should be enabled for a file-backed tab")
	}
}

// TestExternalStatusText is split from predicate assertions so it can use the
// concrete simulation-screen type without obscuring action failures.
func TestExternalStatusText(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "manual.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.openPath = func(string) error { return nil }
	a.openFile(target)
	a.handleEvent(waitForDefaultOpenDone(t, a))
	a.statusUntil = time.Time{}
	a.draw()
	a.screen.Show()

	line := screenLine(a.screen.(tcell.SimulationScreen), a.height-1)
	if !strings.Contains(line, "EXTERNAL") || !strings.Contains(line, "manual.pdf") {
		t.Fatalf("status line = %q", line)
	}
}
