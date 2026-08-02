// =============================================================================
// File: internal/app/reopen_test.go
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

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/editor"
)

// TestOpenFile_ReopenCleanTextReloadsWithoutMtimeChange verifies repeated open
// reads disk contents even when the file timestamp matches the tab snapshot.
func TestOpenFile_ReopenCleanTextReloadsWithoutMtimeChange(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	mtime := tab.Mtime

	if err := os.WriteFile(target, []byte("new disk contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	a.openFile(target)

	if got := tab.Buffer.String(); got != "new disk contents" {
		t.Fatalf("buffer = %q, want latest disk contents", got)
	}
	if len(a.tabs) != 1 {
		t.Fatalf("reopen created %d tabs", len(a.tabs))
	}
	if tab.Dirty {
		t.Fatal("clean reload marked tab dirty")
	}
	if got := a.statusMsg; got != "Reloaded notes.txt from disk" {
		t.Fatalf("status = %q", got)
	}
}

// TestOpenFile_ReopenDirtyTextCancelPreservesEditorState ensures the default
// No/Esc path never discards unsaved content or its undo history.
func TestOpenFile_ReopenDirtyTextCancelPreservesEditorState(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	tab.Cursor = editor.Position{Line: 0, Col: 3}
	tab.Anchor = tab.Cursor
	tab.InsertString(" local")
	tab.Anchor = editor.Position{Line: 0, Col: 1}
	tab.ScrollX = 2
	tab.ScrollY = 0

	beforeBuffer := tab.Buffer.String()
	beforeCursor := tab.Cursor
	beforeAnchor := tab.Anchor
	beforeScrollX := tab.ScrollX
	beforeScrollY := tab.ScrollY
	if !tab.CanUndo() {
		t.Fatal("test setup did not create undo history")
	}
	if err := os.WriteFile(target, []byte("new disk contents"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.openFile(target)

	if !a.confirmOpen || a.confirmHover != 0 {
		t.Fatalf("confirm open=%v hover=%d, want open with No selected", a.confirmOpen, a.confirmHover)
	}
	if a.confirmTitle != "Reload file from disk?" {
		t.Fatalf("confirm title = %q", a.confirmTitle)
	}
	if !strings.Contains(a.confirmMessage, "notes.txt") {
		t.Fatalf("confirm message = %q", a.confirmMessage)
	}
	a.handleConfirmKey(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))

	if a.confirmOpen {
		t.Fatal("confirmation remained open after Esc")
	}
	if got := tab.Buffer.String(); got != beforeBuffer {
		t.Fatalf("buffer = %q, want preserved %q", got, beforeBuffer)
	}
	if !tab.Dirty || !tab.CanUndo() {
		t.Fatalf("dirty=%v canUndo=%v, want preserved", tab.Dirty, tab.CanUndo())
	}
	if tab.Cursor != beforeCursor || tab.Anchor != beforeAnchor {
		t.Fatalf("cursor/anchor changed: cursor=%v anchor=%v", tab.Cursor, tab.Anchor)
	}
	if tab.ScrollX != beforeScrollX || tab.ScrollY != beforeScrollY {
		t.Fatalf("scroll changed: x=%d y=%d", tab.ScrollX, tab.ScrollY)
	}
}

// TestOpenFile_ReopenDirtyTextConfirmReloadsDisk verifies Yes discards the
// dirty buffer, installs current disk contents, and resets edit history.
func TestOpenFile_ReopenDirtyTextConfirmReloadsDisk(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	tab.InsertString("local ")
	if err := os.WriteFile(target, []byte("new disk contents"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.openFile(target)
	a.confirmHover = 1
	a.confirmYes()

	if got := tab.Buffer.String(); got != "new disk contents" {
		t.Fatalf("buffer = %q, want latest disk contents", got)
	}
	if tab.Dirty || tab.CanUndo() || tab.CanRedo() {
		t.Fatalf("dirty=%v undo=%v redo=%v, want reset", tab.Dirty, tab.CanUndo(), tab.CanRedo())
	}
	if got := a.statusMsg; got != "Reloaded notes.txt from disk" {
		t.Fatalf("status = %q", got)
	}
}

// TestOpenFile_ReopenReloadFailureKeepsOldBuffer verifies a missing disk file
// reports the error without replacing the tab's last readable contents.
func TestOpenFile_ReopenReloadFailureKeepsOldBuffer(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}

	a.openFile(target)

	if got := tab.Buffer.String(); got != "old" {
		t.Fatalf("buffer = %q, want old contents preserved", got)
	}
	if !strings.HasPrefix(a.statusMsg, "Could not reload notes.txt:") {
		t.Fatalf("status = %q", a.statusMsg)
	}
}
