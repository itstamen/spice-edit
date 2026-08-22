// =============================================================================
// File: internal/app/leader_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
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

	"github.com/cloudmanic/spice-edit/internal/editor"
)

// sendLeader dispatches one Esc-leader sequence through the normal key path.
func sendLeader(a *App, r rune) {
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, r))
}

// TestLeaderActionFor_AllBindingsResolve walks the binding table and
// verifies every entry returns a non-nil action. Catches accidentally
// dropping a method reference when the table is reshuffled.
func TestLeaderActionFor_AllBindingsResolve(t *testing.T) {
	for _, b := range leaderBindings() {
		if leaderActionFor(b.key) == nil {
			t.Errorf("binding %q resolved to nil", b.key)
		}
	}
}

// TestLeaderActionFor_UnboundReturnsNil pins down the contract that
// leaderActionFor reports a miss with nil so handleKey can distinguish
// "leader fired" from "key was unbound — fall through".
func TestLeaderActionFor_UnboundReturnsNil(t *testing.T) {
	if leaderActionFor('y') != nil {
		t.Fatal("'y' should not be a leader binding (no editor action mapped)")
	}
}

// TestHandleKey_LeaderSave saves the active tab via Esc, s. The buffer
// is dirtied before the leader fires so the assertion is meaningful:
// a successful save flips the dirty flag back to false.
func TestHandleKey_LeaderSave(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(target, []byte(""), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.handleKey(keyEv(tcell.KeyRune, 'x')) // dirty the buffer
	if !a.activeTabPtr().Dirty {
		t.Fatal("expected dirty buffer before save")
	}

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 's'))

	if a.activeTabPtr().Dirty {
		t.Fatal("Esc-s should have saved the buffer (dirty still true)")
	}
}

// TestHandleKey_LeaderUndoRedo round-trips an edit through Esc-u and
// Esc-r. Pins down both bindings at once and the fact that the leader
// state resets between sequences (we re-arm Esc each time).
func TestHandleKey_LeaderUndoRedo(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(target, []byte(""), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.handleKey(keyEv(tcell.KeyRune, 'a'))

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'u'))
	if a.activeTabPtr().Buffer.Lines[0] != "" {
		t.Fatalf("Esc-u should have undone the insert, got %q", a.activeTabPtr().Buffer.Lines[0])
	}

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'r'))
	if a.activeTabPtr().Buffer.Lines[0] != "a" {
		t.Fatalf("Esc-r should have redone the insert, got %q", a.activeTabPtr().Buffer.Lines[0])
	}
}

// TestHandleKey_LeaderToggleSidebar flips sidebarShown via Esc-t. The
// toggle is the simplest leader action with no preconditions, so it's
// the most stable smoke test that the dispatch wiring is intact.
func TestHandleKey_LeaderToggleSidebar(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	before := a.sidebarShown
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 't'))
	if a.sidebarShown == before {
		t.Fatalf("Esc-t should toggle sidebar (still %v)", a.sidebarShown)
	}
}

// TestHandleKey_LeaderToggleSoftWrap flips softWrap via Esc-z and clears
// horizontal scroll on open text tabs.
func TestHandleKey_LeaderToggleSoftWrap(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(target, []byte(strings.Repeat("x", 100)), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	tab.ScrollX = 20

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'z'))

	if !a.softWrap {
		t.Fatal("Esc-z should enable soft wrap")
	}
	if tab.ScrollX != 0 {
		t.Fatalf("Esc-z should clear ScrollX, got %d", tab.ScrollX)
	}
	if a.statusMsg != "Soft wrap enabled" {
		t.Fatalf("status = %q, want enabled message", a.statusMsg)
	}

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'z'))
	if a.softWrap {
		t.Fatal("second Esc-z should disable soft wrap")
	}
}

// TestHandleKey_LeaderOpenWithDefaultApp verifies Esc-o reaches the same
// opener path used by the main menu.
func TestHandleKey_LeaderOpenWithDefaultApp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	opened := make(chan string, 1)
	a.openPath = func(path string) error {
		opened <- path
		return nil
	}

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'o'))

	select {
	case got := <-opened:
		if got != target {
			t.Fatalf("opened path = %q, want %q", got, target)
		}
	case <-time.After(time.Second):
		t.Fatal("Esc-o did not invoke opener")
	}
	a.handleEvent(waitForDefaultOpenDone(t, a))
}

// TestHandleKey_LeaderShowInFileManager verifies Esc-e reaches the shared
// reveal action and selects the active file.
func TestHandleKey_LeaderShowInFileManager(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	calls := make(chan revealCall, 1)
	a.revealPath = func(path string, selectFile bool) error {
		calls <- revealCall{path: path, selectFile: selectFile}
		return nil
	}

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'e'))

	select {
	case got := <-calls:
		if got.path != target || !got.selectFile {
			t.Fatalf("reveal call = %#v, want path=%q select=true", got, target)
		}
	case <-time.After(time.Second):
		t.Fatal("Esc-e did not invoke file manager reveal")
	}
	a.handleEvent(waitForRevealDone(t, a))
}

// TestHandleKey_LeaderToggleLineComment binds Esc-/ to the same action menu
// path, giving keyboard users a fast toggle without adding Ctrl shortcuts.
func TestHandleKey_LeaderToggleLineComment(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main.go")
	if err := os.WriteFile(target, []byte("one\ntwo"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, '/'))

	if got := a.activeTabPtr().Buffer.String(); got != "// one\ntwo" {
		t.Fatalf("Esc-/ should comment the cursor line, got %q", got)
	}
}

// TestLeaderDeleteCurrentLine removes every line touched by a selection via
// the Esc-d leader binding.
func TestLeaderDeleteCurrentLine(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "lines.txt")
	if err := os.WriteFile(target, []byte("one\ntwo\nthree"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 1, Col: 1}

	sendLeader(a, 'd')

	if got, want := tab.Buffer.String(), "three"; got != want {
		t.Fatalf("Esc-d buffer = %q, want %q", got, want)
	}
}

// TestLeaderDeleteCurrentLine_ReadOnlyIsNoOp keeps the leader route inert for
// tabs that cannot accept text edits.
func TestLeaderDeleteCurrentLine_ReadOnlyIsNoOp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "blocked.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.7\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	tab := a.activeTabPtr()
	want := tab.Buffer.String()

	sendLeader(a, 'd')

	if got := tab.Buffer.String(); got != want {
		t.Fatalf("Esc-d changed read-only buffer to %q, want %q", got, want)
	}
}

// TestHandleKey_LeaderClipboard verifies Esc clipboard bindings share the
// guarded menu actions and Select All does not publish selection text.
func TestHandleKey_LeaderClipboard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clipboard.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(path)
	tab := a.activeTabPtr()
	copied := recordingClipboard(a, nil)

	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 5}
	sendLeader(a, 'c')
	if got := *copied; len(got) != 1 || got[0] != "hello" {
		t.Fatalf("copy writes = %#v, want []string{\"hello\"}", got)
	}
	if got := tab.Buffer.String(); got != "hello" {
		t.Fatalf("copy text = %q, want unchanged", got)
	}

	tab.Anchor = editor.Position{Line: 0, Col: 0}
	tab.Cursor = editor.Position{Line: 0, Col: 5}
	sendLeader(a, 'x')
	if got := *copied; len(got) != 2 || got[1] != "hello" {
		t.Fatalf("cut writes = %#v, want second hello", got)
	}
	if got := tab.Buffer.String(); got != "" {
		t.Fatalf("cut text = %q, want empty", got)
	}

	a.clipBuf = "hello"
	sendLeader(a, 'v')
	if got := tab.Buffer.String(); got != "hello" {
		t.Fatalf("paste text = %q, want hello", got)
	}

	sendLeader(a, 'a')
	if got := tab.SelectionText(); got != "hello" {
		t.Fatalf("Select All selection = %q, want hello", got)
	}
	if got := len(*copied); got != 2 {
		t.Fatalf("Select All writes = %d, want 2", got)
	}
}

// TestHandleKey_LeaderSelectAllInertWithoutEditableTab keeps direct leader
// dispatch safe when no editable text target is active.
func TestHandleKey_LeaderSelectAllInertWithoutEditableTab(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(t *testing.T, a *App)
	}{
		{name: "no tab", open: func(_ *testing.T, _ *App) {}},
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
			tc.open(t, a)
			sendLeader(a, 'a')
			if tab := a.activeTabPtr(); tab != nil && tab.HasSelection() {
				t.Fatal("Esc-a should not select external tab")
			}
		})
	}
}

// TestHandleKey_LeaderQuit sets a.quit via Esc-q. We test this directly
// rather than through Run() so we don't have to drive the event loop —
// the quit flag is what Run() polls each tick.
func TestHandleKey_LeaderQuit(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'q'))
	if !a.quit {
		t.Fatal("Esc-q should set a.quit = true")
	}
}

// TestHandleKey_LeaderUnboundFallsThrough is the regression test for the
// "stray Esc shouldn't swallow the next keystroke" property: pressing
// Esc and then an unbound letter must still deliver that letter to the
// active tab. Without the fall-through, an accidental Esc tap would
// silently eat the user's next character.
func TestHandleKey_LeaderUnboundFallsThrough(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(target, []byte(""), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyRune, 'y'))

	if got := a.activeTabPtr().Buffer.Lines[0]; got != "y" {
		t.Fatalf("unbound key after Esc should reach the editor, got %q", got)
	}
}

// TestHandleKey_LeaderTimesOut verifies the leader window expires:
// after doubleEscMs has passed since the last Esc, a bound letter must
// reach the editor as a normal keystroke instead of firing the action.
func TestHandleKey_LeaderTimesOut(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(target, []byte(""), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)

	a.handleKey(keyEv(tcell.KeyEsc, 0))
	// Backdate the Esc timestamp past the leader window so the next 's'
	// is treated as a plain keystroke rather than Save.
	a.lastEscape = time.Now().Add(-2 * doubleEscMs)
	a.handleKey(keyEv(tcell.KeyRune, 's'))

	if got := a.activeTabPtr().Buffer.Lines[0]; got != "s" {
		t.Fatalf("expired leader window: 's' should insert literally, got %q", got)
	}
}

// TestHandleKey_EscDoubleTapStillOpensMenu makes sure adding the leader
// table didn't break the existing double-Esc-opens-menu gesture. The
// second Esc inside the leader window must still be interpreted as
// "open the menu," not as an unbound leader keystroke.
func TestHandleKey_EscDoubleTapStillOpensMenu(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	a.handleKey(keyEv(tcell.KeyEsc, 0))
	if !a.menuOpen {
		t.Fatal("double-Esc should still open the menu after leader was added")
	}
}
