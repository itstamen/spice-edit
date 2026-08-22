// =============================================================================
// File: internal/app/eventburst_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-04
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/customactions"
	"github.com/cloudmanic/spice-edit/internal/editor"
)

type queuedScreen struct {
	tcell.Screen
	events []tcell.Event
}

// PollEvent returns the next queued event without blocking.
func (s *queuedScreen) PollEvent() tcell.Event {
	if len(s.events) == 0 {
		return nil
	}
	ev := s.events[0]
	s.events = s.events[1:]
	return ev
}

// HasPendingEvent reports whether PollEvent can return immediately.
func (s *queuedScreen) HasPendingEvent() bool { return len(s.events) > 0 }

// repeatedKeys builds a deterministic queue of identical key events.
func repeatedKeys(key tcell.Key, count int) []tcell.Event {
	events := make([]tcell.Event, count)
	for i := range events {
		events[i] = tcell.NewEventKey(key, 0, tcell.ModNone)
	}
	return events
}

// testTabWithLines builds an in-memory text tab large enough for navigation.
func testTabWithLines(t *testing.T, count int) *editor.Tab {
	t.Helper()
	lines := make([]string, count)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	tab, err := editor.NewTab("")
	if err != nil {
		t.Fatal(err)
	}
	tab.Buffer = editor.NewBuffer(strings.Join(lines, "\n"))
	return tab
}

// TestProcessEventBurstAppliesLimitWithoutDroppingKeys verifies one burst
// stops at the hard event limit while preserving all remaining queued input.
func TestProcessEventBurstAppliesLimitWithoutDroppingKeys(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	tab := testTabWithLines(t, 200)
	a.tabs = []*editor.Tab{tab}
	source := &queuedScreen{events: repeatedKeys(tcell.KeyDown, 99)}
	first := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)

	deferred, handled := a.processEventBurst(first, source, time.Now().Add(time.Hour))

	if deferred != nil || handled != maxEventBurst {
		t.Fatalf("deferred=%T handled=%d, want nil/%d", deferred, handled, maxEventBurst)
	}
	if tab.Cursor.Line != maxEventBurst {
		t.Fatalf("cursor line = %d, want %d", tab.Cursor.Line, maxEventBurst)
	}
	if got := len(source.events); got != 100-maxEventBurst {
		t.Fatalf("queued events = %d, want %d", got, 100-maxEventBurst)
	}
}

// TestProcessEventBurstDefersLeaderBoundary keeps Esc on the next frame so a
// queued command cannot execute before the user sees the preceding page.
func TestProcessEventBurstDefersLeaderBoundary(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.tabs = []*editor.Tab{testTabWithLines(t, 200)}
	esc := tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone)
	source := &queuedScreen{events: []tcell.Event{esc}}

	deferred, handled := a.processEventBurst(
		tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone),
		source,
		time.Now().Add(time.Hour),
	)

	if deferred != esc || handled != 1 {
		t.Fatalf("deferred=%T handled=%d, want Esc/1", deferred, handled)
	}
	if !a.lastEscape.IsZero() {
		t.Fatal("deferred Esc was processed before the page frame")
	}
}

// TestProcessEventBurstStopsAtDeadline verifies the time limit prevents a
// continuous input stream from starving frame presentation.
func TestProcessEventBurstStopsAtDeadline(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.tabs = []*editor.Tab{testTabWithLines(t, 200)}
	start := time.Unix(1, 0)
	a.eventNow = func() time.Time { return start.Add(maxEventBurstDuration + time.Millisecond) }
	source := &queuedScreen{events: repeatedKeys(tcell.KeyDown, 10)}

	_, handled := a.processEventBurst(
		tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone),
		source,
		start.Add(maxEventBurstDuration),
	)

	if handled != 1 || len(source.events) != 10 {
		t.Fatalf("handled=%d queued=%d, want 1/10", handled, len(source.events))
	}
}

// TestEventCanShareFrame pins which input classes may defer intermediate
// frames and which events form visible state boundaries.
func TestEventCanShareFrame(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	tests := []struct {
		name string
		ev   tcell.Event
		want bool
	}{
		{"page-down", tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone), true},
		{"shift-up", tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModShift), true},
		{"rune", tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone), true},
		{"backspace", tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone), true},
		{"wheel-down", tcell.NewEventMouse(10, 10, tcell.WheelDown, tcell.ModNone), true},
		{"wheel-left", tcell.NewEventMouse(10, 10, tcell.WheelLeft, tcell.ModNone), true},
		{"hover", tcell.NewEventMouse(10, 10, tcell.ButtonNone, tcell.ModNone), true},
		{"escape", tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone), false},
		{"ctrl-rune", tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModCtrl), false},
		{"left-click", tcell.NewEventMouse(10, 10, tcell.Button1, tcell.ModNone), false},
		{"resize", tcell.NewEventResize(100, 40), false},
		{"completion", &finderRebuiltEvent{when: time.Now()}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.eventCanShareFrame(tt.ev); got != tt.want {
				t.Fatalf("eventCanShareFrame()=%v, want %v", got, tt.want)
			}
		})
	}
}

// TestHandleEvent_BracketedPaste collects terminal paste keys until the end
// marker, then inserts the normalized payload as one undoable operation.
func TestHandleEvent_BracketedPaste(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	tab, err := editor.NewTab("")
	if err != nil {
		t.Fatal(err)
	}
	tab.IndentUnit = "  "
	a.tabs = []*editor.Tab{tab}
	a.clipBuf = "leader action must not run"
	a.readFromSystem = func() (string, error) { return "", fmt.Errorf("system clipboard unavailable") }

	a.handleEvent(tcell.NewEventPaste(true))
	a.handleEvent(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyCtrlJ, 0, tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyCtrlM, 0, tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyCtrlJ, 0, tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))

	if got := tab.Buffer.String(); got != "" {
		t.Fatalf("buffer mutated before paste end: %q", got)
	}
	if !a.lastEscape.IsZero() {
		t.Fatal("pasted Esc sequence armed a leader action")
	}

	a.handleEvent(tcell.NewEventPaste(false))

	if got := tab.Buffer.String(); got != "a\n\tbvc\nd" {
		t.Fatalf("buffer = %q, want exact pasted text", got)
	}
	if !tab.Undo() {
		t.Fatal("paste should be one undoable insertion")
	}
	if got := tab.Buffer.String(); got != "" {
		t.Fatalf("undo buffer = %q, want initial state", got)
	}
}

// TestHandleEvent_BracketedPastePrefersSystemClipboard makes bracketed paste
// use the host clipboard when it is readable instead of the captured terminal
// payload. This is important when a terminal transforms pasted key events.
func TestHandleEvent_BracketedPastePrefersSystemClipboard(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	tab, err := editor.NewTab("")
	if err != nil {
		t.Fatal(err)
	}
	a.tabs = []*editor.Tab{tab}
	a.readFromSystem = func() (string, error) { return "system\r\ntext", nil }

	a.handleEvent(tcell.NewEventPaste(true))
	for _, r := range "captured" {
		a.handleEvent(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	a.handleEvent(tcell.NewEventPaste(false))

	if got := tab.Buffer.String(); got != "system\ntext" {
		t.Fatalf("buffer = %q, want normalized system clipboard text", got)
	}
}

// pasteEventText drives a complete bracketed-paste lifecycle through the app
// event router, including command keys that must remain inert as pasted data.
// It forces the captured-terminal fallback so its callers do not depend on
// the host clipboard contents.
func pasteEventText(a *App, prefix, suffix string) {
	a.readFromSystem = func() (string, error) { return "", fmt.Errorf("system clipboard unavailable") }
	a.handleEvent(tcell.NewEventPaste(true))
	for _, r := range prefix {
		a.handleEvent(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	a.handleEvent(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	a.handleEvent(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	for _, r := range suffix {
		a.handleEvent(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	a.handleEvent(tcell.NewEventPaste(false))
}

// TestHandleEvent_BracketedPasteTargetsTextInputs proves paste completion is
// routed to the focused single-line UI state without editing the background
// buffer or treating pasted newline/Esc keys as modal commands.
func TestHandleEvent_BracketedPasteTargetsTextInputs(t *testing.T) {
	tests := []struct {
		name  string
		open  func(*App)
		value func(*App) string
	}{
		{
			name: "prompt",
			open: func(a *App) {
				a.openPrompt("Name", "", "pre", nil)
			},
			value: func(a *App) string { return string(a.promptValue) },
		},
		{
			name: "find",
			open: func(a *App) {
				a.openFind()
				a.findValue = []rune("pre")
				a.findCursor = len(a.findValue)
			},
			value: func(a *App) string { return string(a.findValue) },
		},
		{
			name: "finder",
			open: func(a *App) {
				a.finderOpen = true
				a.finderQuery = []rune("pre")
				a.finderCursor = len(a.finderQuery)
			},
			value: func(a *App) string { return string(a.finderQuery) },
		},
		{
			name: "form text",
			open: func(a *App) {
				a.openForm("Action", []customactions.Prompt{{Key: "NAME", Type: customactions.PromptText, Default: "pre"}}, nil)
			},
			value: func(a *App) string { return string(a.formText[0]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestApp(t, t.TempDir())
			tab, err := editor.NewTab("")
			if err != nil {
				t.Fatal(err)
			}
			tab.Buffer = editor.NewBuffer("background")
			a.tabs = []*editor.Tab{tab}
			tt.open(a)

			pasteEventText(a, "alpha", "beta")

			if got := tab.Buffer.String(); got != "background" {
				t.Fatalf("background buffer = %q", got)
			}
			if got := tt.value(a); got != "prealpha beta" {
				t.Fatalf("input value = %q, want %q", got, "prealpha beta")
			}
			if !a.promptOpen && !a.findOpen && !a.finderOpen && !a.formOpen {
				t.Fatal("pasted command key closed the text input")
			}
		})
	}
}

// TestHandleEvent_BracketedPasteDiscardsNonTextOverlays keeps paste payloads
// from leaking through menus, confirmations, and select-only form rows.
func TestHandleEvent_BracketedPasteDiscardsNonTextOverlays(t *testing.T) {
	tests := []struct {
		name string
		open func(*App)
	}{
		{"menu", func(a *App) { a.openMenu() }},
		{"confirm", func(a *App) { a.openConfirm("Delete", "sure?", nil) }},
		{"dirty", func(a *App) { a.openDirtyClose("Save", "changed", nil, nil) }},
		{"context", func(a *App) { a.contextOpen = true }},
		{"select form", func(a *App) {
			a.openForm("Action", []customactions.Prompt{{Key: "HOST", Type: customactions.PromptSelect, Options: []string{"one", "two"}}}, nil)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestApp(t, t.TempDir())
			tab, err := editor.NewTab("")
			if err != nil {
				t.Fatal(err)
			}
			tab.Buffer = editor.NewBuffer("background")
			a.tabs = []*editor.Tab{tab}
			tt.open(a)

			pasteEventText(a, "alpha", "beta")

			if got := tab.Buffer.String(); got != "background" {
				t.Fatalf("background buffer = %q", got)
			}
		})
	}
}

// TestProcessEventBurstPreservesEditingAndSelection ensures batching changes
// only frame frequency, not editor state transitions.
func TestProcessEventBurstPreservesEditingAndSelection(t *testing.T) {
	t.Run("typing", func(t *testing.T) {
		a := newTestApp(t, t.TempDir())
		tab := testTabWithLines(t, 20)
		a.tabs = []*editor.Tab{tab}
		source := &queuedScreen{events: []tcell.Event{
			tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone),
			tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone),
		}}

		a.processEventBurst(
			tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone),
			source,
			time.Now().Add(time.Hour),
		)

		if got := tab.Buffer.Lines[0]; !strings.HasPrefix(got, "xyz") {
			t.Fatalf("first line=%q, want xyz prefix", got)
		}
	})

	t.Run("shift-selection", func(t *testing.T) {
		a := newTestApp(t, t.TempDir())
		tab := testTabWithLines(t, 20)
		a.tabs = []*editor.Tab{tab}
		source := &queuedScreen{events: []tcell.Event{
			tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModShift),
			tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModShift),
		}}

		a.processEventBurst(
			tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModShift),
			source,
			time.Now().Add(time.Hour),
		)

		if tab.Anchor.Line != 0 || tab.Cursor.Line != 3 {
			t.Fatalf("anchor=%+v cursor=%+v", tab.Anchor, tab.Cursor)
		}
	})

	t.Run("wheel", func(t *testing.T) {
		a := newTestApp(t, t.TempDir())
		tab := testTabWithLines(t, 200)
		a.tabs = []*editor.Tab{tab}
		x := a.sidebarW() + 5
		source := &queuedScreen{events: []tcell.Event{
			tcell.NewEventMouse(x, 5, tcell.WheelDown, tcell.ModNone),
			tcell.NewEventMouse(x, 5, tcell.WheelDown, tcell.ModNone),
		}}

		a.processEventBurst(
			tcell.NewEventMouse(x, 5, tcell.WheelDown, tcell.ModNone),
			source,
			time.Now().Add(time.Hour),
		)

		if tab.ScrollY != 9 {
			t.Fatalf("ScrollY=%d, want 9", tab.ScrollY)
		}
	})
}

// TestRunBatchesNavigationFrames exercises the full Run loop and proves 100
// navigation events do not force 100 intermediate terminal frames.
func TestRunBatchesNavigationFrames(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	a.tabs = []*editor.Tab{testTabWithLines(t, 5000)}
	events := repeatedKeys(tcell.KeyDown, 100)
	events = append(events,
		tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone),
	)
	queued := &queuedScreen{Screen: a.screen, events: events}
	a.screen = queued
	fixedNow := time.Unix(1, 0)
	a.eventNow = func() time.Time { return fixedNow }
	frames := 0
	a.showFrame = func() { frames++ }

	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	if a.tabs[0].Cursor.Line != 100 {
		t.Fatalf("cursor line = %d, want 100", a.tabs[0].Cursor.Line)
	}
	if frames >= 20 {
		t.Fatalf("frames = %d, want a bounded burst count", frames)
	}
}
