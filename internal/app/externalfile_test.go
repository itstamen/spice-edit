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
