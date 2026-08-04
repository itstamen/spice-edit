# Large Text Editing Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep rapid navigation and editing responsive by batching compatible input events and caching syntax-highlight and soft-wrap work.

**Architecture:** The single-threaded event loop consumes bounded bursts of compatible queued events and draws one frame per burst while preserving event order. Each text tab caches one width-and-revision soft-wrap layout and one relative, prefetched syntax-highlight window; shared invalidation helpers cover every text mutation path.

**Tech Stack:** Go 1.24, tcell v2.13.9, Chroma v2.24.0, Go benchmarks, race-enabled tests.

## Global Constraints

- Preserve every key, mouse-wheel, and editing event; final state must match sequential handling.
- Process at most 64 events or 8 milliseconds before flushing a frame.
- Flush before modal, resize, command, tab-switch, file-operation, asynchronous-completion, and quit boundaries.
- Keep the event loop and caches single-threaded.
- Keep PageUp/PageDown distance, cursor, selection, undo, find, save, image, and terminal-graphics behavior unchanged.
- Keep one wrap layout and one highlight window per tab; do not add an unbounded cache.
- Do not replace the string-per-line buffer or add asynchronous highlighting.
- Preserve unrelated Kitty draft files in the worktree.

---

### Task 1: Batch compatible queued events

**Files:**
- Modify: `internal/app/app.go:323-430, 760-775, 1039-1065`
- Create: `internal/app/eventburst_test.go`

**Interfaces:**
- Produces: `pendingEventSource` with `PollEvent() tcell.Event` and `HasPendingEvent() bool`.
- Produces: `(*App).eventCanShareFrame(tcell.Event) bool`.
- Produces: `(*App).processEventBurst(tcell.Event, pendingEventSource, time.Time) (tcell.Event, int)`.
- Adds: `eventNow func() time.Time` test seam on `App`.
- Consumes: existing `handleEvent`, `flushFrame`, `anyModalOpen`, `lastEscape`, and `dragMode` state.

- [x] **Step 1: Write failing burst-limit and boundary tests**

Create `internal/app/eventburst_test.go` with the repository header, a queued screen, and these helpers:

```go
package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/editor"
)

type queuedScreen struct {
	tcell.Screen
	events []tcell.Event
}

func (s *queuedScreen) PollEvent() tcell.Event {
	if len(s.events) == 0 {
		return nil
	}
	ev := s.events[0]
	s.events = s.events[1:]
	return ev
}

func (s *queuedScreen) HasPendingEvent() bool { return len(s.events) > 0 }

func repeatedKeys(key tcell.Key, count int) []tcell.Event {
	events := make([]tcell.Event, count)
	for i := range events {
		events[i] = tcell.NewEventKey(key, 0, tcell.ModNone)
	}
	return events
}

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
```

Add:

```go
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
```

- [x] **Step 2: Write the failing Run frame-count test**

Add:

```go
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
```

Add the concrete classifier test:

```go
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
```

Add state-preservation coverage:

```go
func TestProcessEventBurstPreservesEditingAndSelection(t *testing.T) {
	t.Run("typing", func(t *testing.T) {
		a := newTestApp(t, t.TempDir())
		tab := testTabWithLines(t, 20)
		a.tabs = []*editor.Tab{tab}
		source := &queuedScreen{events: []tcell.Event{
			tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone),
			tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone),
		}}
		a.processEventBurst(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone), source, time.Now().Add(time.Hour))
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
		a.processEventBurst(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModShift), source, time.Now().Add(time.Hour))
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
		a.processEventBurst(tcell.NewEventMouse(x, 5, tcell.WheelDown, tcell.ModNone), source, time.Now().Add(time.Hour))
		if tab.ScrollY != 9 {
			t.Fatalf("ScrollY=%d, want 9", tab.ScrollY)
		}
	})
}
```

- [x] **Step 3: Run tests and confirm failure**

Run:

```bash
go test ./internal/app -run 'Test(ProcessEventBurst|RunBatchesNavigationFrames|EventCanShareFrame)'
```

Expected: FAIL because the burst interfaces do not exist.

- [x] **Step 4: Implement bounded event bursts**

Add near `Run`:

```go
const (
	maxEventBurst         = 64
	maxEventBurstDuration = 8 * time.Millisecond
)

type pendingEventSource interface {
	PollEvent() tcell.Event
	HasPendingEvent() bool
}

func (a *App) eventTime() time.Time {
	if a.eventNow != nil {
		return a.eventNow()
	}
	return time.Now()
}
```

Add `eventNow func() time.Time` beside the other App test seams.

Implement classification:

```go
func (a *App) eventCanShareFrame(ev tcell.Event) bool {
	if ev == nil || a.anyModalOpen() || !a.lastEscape.IsZero() {
		return false
	}
	switch e := ev.(type) {
	case *tcell.EventKey:
		switch e.Key() {
		case tcell.KeyPgUp, tcell.KeyPgDn,
			tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight,
			tcell.KeyHome, tcell.KeyEnd,
			tcell.KeyEnter, tcell.KeyTab,
			tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyDelete:
			return true
		case tcell.KeyRune:
			return e.Modifiers()&(tcell.ModCtrl|tcell.ModAlt|tcell.ModMeta) == 0
		}
	case *tcell.EventMouse:
		buttons := e.Buttons()
		wheels := tcell.WheelUp | tcell.WheelDown | tcell.WheelLeft | tcell.WheelRight
		return buttons&wheels != 0 || (buttons == tcell.ButtonNone && a.dragMode == "")
	}
	return false
}
```

Implement processing:

```go
func (a *App) processEventBurst(first tcell.Event, source pendingEventSource, deadline time.Time) (tcell.Event, int) {
	a.handleEvent(first)
	handled := 1
	if !a.eventCanShareFrame(first) {
		return nil, handled
	}
	for !a.quit && handled < maxEventBurst && a.eventTime().Before(deadline) && source.HasPendingEvent() {
		next := source.PollEvent()
		if next == nil {
			break
		}
		if !a.eventCanShareFrame(next) {
			return next, handled
		}
		a.handleEvent(next)
		handled++
	}
	return nil, handled
}
```

Replace `Run` with:

```go
func (a *App) Run() error {
	a.width, a.height = a.screen.Size()
	a.flushFrame()

	var deferred tcell.Event
	for !a.quit {
		ev := deferred
		deferred = nil
		if ev == nil {
			ev = a.screen.PollEvent()
		}
		if ev == nil {
			break
		}
		deadline := a.eventTime().Add(maxEventBurstDuration)
		deferred, _ = a.processEventBurst(ev, a.screen, deadline)
		a.flushFrame()
	}
	return nil
}
```

- [x] **Step 5: Run focused and full app tests**

Run:

```bash
gofmt -w internal/app/app.go internal/app/eventburst_test.go
go test ./internal/app -run 'Test(ProcessEventBurst|RunBatchesNavigationFrames|EventCanShareFrame|HandleKey|HandleMouse|FlushFrame)'
go test ./internal/app
```

Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add internal/app/app.go internal/app/eventburst_test.go
git commit -m "Batch rapid editor events"
```

---

### Task 2: Cache and index soft-wrap layouts

**Files:**
- Modify: `internal/editor/tab.go:59-115, 269-450, 557-930`
- Modify: `internal/editor/wrap.go`
- Modify: `internal/editor/comment.go:97-130`
- Modify: `internal/editor/undo.go:78-89`
- Create: `internal/editor/wrap_test.go`
- Test: `internal/editor/tab_test.go`

**Interfaces:**
- Produces: `wrapLayout` with `width int`, `revision uint64`, `rows []wrapRow`, and `lineStarts []int`.
- Produces: `(*Tab).wrapLayoutFor(int) *wrapLayout`.
- Produces: `(*Tab).markTextChanged()` and `(*Tab).invalidateWrapLayout()`.
- Adds: `textRevision uint64` and `wrapCache wrapLayout` to `Tab`.

- [x] **Step 1: Write failing cache and index tests**

Create `internal/editor/wrap_test.go`:

```go
package editor

import "testing"

func TestWrapLayoutForReusesWidthAndRevision(t *testing.T) {
	tab := &Tab{Buffer: NewBuffer("alpha beta gamma\ndelta epsilon")}
	first := tab.wrapLayoutFor(8)
	firstRows := first.rows
	firstPtr := &firstRows[0]
	secondRows := tab.wrapLayoutFor(8).rows
	if &secondRows[0] != firstPtr {
		t.Fatal("same width and revision rebuilt wrap layout")
	}
	if len(first.lineStarts) != tab.Buffer.LineCount()+1 {
		t.Fatalf("lineStarts=%d, want %d", len(first.lineStarts), tab.Buffer.LineCount()+1)
	}

	third := tab.wrapLayoutFor(12)
	if &third.rows[0] == firstPtr || third.width != 12 {
		t.Fatal("width change did not rebuild wrap layout")
	}
}

func TestWrapLayoutInvalidatesAfterTextEdit(t *testing.T) {
	tab := &Tab{Buffer: NewBuffer("alpha beta gamma")}
	firstRows := tab.wrapLayoutFor(8).rows
	firstPtr := &firstRows[0]
	revision := tab.textRevision
	tab.InsertRune('x')
	second := tab.wrapLayoutFor(8)
	if &second.rows[0] == firstPtr {
		t.Fatal("text edit reused stale wrap layout")
	}
	if tab.textRevision != revision+1 {
		t.Fatalf("revision=%d, want %d", tab.textRevision, revision+1)
	}
}

func TestVisualRowForPositionUsesLineStarts(t *testing.T) {
	lines := make([]string, 10000)
	for i := range lines {
		lines[i] = "abcdefghij"
	}
	tab := &Tab{Buffer: &Buffer{Lines: lines}}
	row, cell := tab.visualRowForPosition(Position{Line: 9999, Col: 7}, 5)
	layout := tab.wrapLayoutFor(5)
	if row < layout.lineStarts[9999] || row >= layout.lineStarts[10000] || cell != 2 {
		t.Fatalf("row=%d cell=%d starts=%v", row, cell, layout.lineStarts[9999:10001])
	}
}
```

Expand the imports to `os`, `path/filepath`, and `testing`, then add:

```go
func assertWrapInvalidated(t *testing.T, tab *Tab, mutate func()) {
	t.Helper()
	rows := tab.wrapLayoutFor(8).rows
	beforePtr := &rows[0]
	beforeRevision := tab.textRevision
	mutate()
	if tab.textRevision != beforeRevision+1 {
		t.Fatalf("revision=%d, want %d", tab.textRevision, beforeRevision+1)
	}
	after := tab.wrapLayoutFor(8).rows
	if &after[0] == beforePtr {
		t.Fatal("mutation reused stale wrap rows")
	}
}

func TestTextMutationsInvalidateWrap(t *testing.T) {
	tests := []struct {
		name  string
		setup func() *Tab
		run   func(*Tab)
	}{
		{"insert-string", func() *Tab { return &Tab{Buffer: NewBuffer("ab")} }, func(tab *Tab) { tab.InsertString("x") }},
		{"insert-rune", func() *Tab { return &Tab{Buffer: NewBuffer("ab")} }, func(tab *Tab) { tab.InsertRune('x') }},
		{"backspace", func() *Tab { return &Tab{Buffer: NewBuffer("ab"), Cursor: Position{Col: 1}} }, func(tab *Tab) { tab.Backspace() }},
		{"delete", func() *Tab { return &Tab{Buffer: NewBuffer("ab")} }, func(tab *Tab) { tab.Delete() }},
		{"selection", func() *Tab { return &Tab{Buffer: NewBuffer("ab"), Cursor: Position{Col: 1}} }, func(tab *Tab) { tab.DeleteSelection() }},
		{"comment", func() *Tab { return &Tab{Path: "x.go", Buffer: NewBuffer("one")} }, func(tab *Tab) { tab.ToggleLineComment() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tab := tt.setup()
			assertWrapInvalidated(t, tab, func() { tt.run(tab) })
		})
	}
}

func TestSnapshotMutationsInvalidateWrap(t *testing.T) {
	tab := &Tab{Buffer: NewBuffer("ab")}
	tab.initUndo()
	tab.InsertRune('x')
	assertWrapInvalidated(t, tab, func() { tab.Undo() })
	assertWrapInvalidated(t, tab, func() { tab.Redo() })
	assertWrapInvalidated(t, tab, func() { tab.RevertFile() })
}

func TestReloadInvalidatesWrap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.json")
	if err := os.WriteFile(path, []byte("one\ntwo"), 0o644); err != nil {
		t.Fatal(err)
	}
	tab, err := NewTab(path)
	if err != nil {
		t.Fatal(err)
	}
	assertWrapInvalidated(t, tab, func() {
		if err := os.WriteFile(path, []byte("three\nfour"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := tab.Reload(); err != nil {
			t.Fatal(err)
		}
	})
}
```

- [x] **Step 2: Run tests and confirm failure**

```bash
go test ./internal/editor -run 'Test(WrapLayout|VisualRowForPosition|TextMutationInvalidatesWrap)'
```

Expected: FAIL because the cache, revision, and line index do not exist.

- [x] **Step 3: Implement the layout cache**

In `internal/editor/wrap.go` add:

```go
type wrapLayout struct {
	width      int
	revision   uint64
	rows       []wrapRow
	lineStarts []int
}

func (t *Tab) wrapLayoutFor(contentW int) *wrapLayout {
	if contentW < 1 {
		contentW = 1
	}
	if t == nil {
		return &wrapLayout{width: contentW, rows: []wrapRow{{}}, lineStarts: []int{0}}
	}
	if t.Buffer == nil {
		t.wrapCache = wrapLayout{
			width: contentW, revision: t.textRevision,
			rows: []wrapRow{{}}, lineStarts: []int{0},
		}
		return &t.wrapCache
	}
	if t.wrapCache.rows != nil &&
		t.wrapCache.width == contentW &&
		t.wrapCache.revision == t.textRevision {
		return &t.wrapCache
	}

	lineCount := t.Buffer.LineCount()
	rows := make([]wrapRow, 0, lineCount)
	lineStarts := make([]int, lineCount+1)
	for lineIdx := 0; lineIdx < lineCount; lineIdx++ {
		lineStarts[lineIdx] = len(rows)
		for _, segment := range wrapLine(t.Buffer.LineRunes(lineIdx), contentW) {
			rows = append(rows, wrapRow{line: lineIdx, wrapSegment: segment})
		}
	}
	lineStarts[lineCount] = len(rows)
	if len(rows) == 0 {
		rows = append(rows, wrapRow{})
	}
	t.wrapCache = wrapLayout{
		width: contentW, revision: t.textRevision,
		rows: rows, lineStarts: lineStarts,
	}
	return &t.wrapCache
}

func (t *Tab) wrapRows(contentW int) []wrapRow {
	return t.wrapLayoutFor(contentW).rows
}
```

Change `visualRowForPosition` to search only the current logical line:

```go
func (t *Tab) visualRowForPosition(pos Position, contentW int) (int, int) {
	pos = t.Buffer.Clamp(pos)
	layout := t.wrapLayoutFor(contentW)
	start := layout.lineStarts[pos.Line]
	end := layout.lineStarts[pos.Line+1]
	for rowIdx := start; rowIdx < end; rowIdx++ {
		row := layout.rows[rowIdx]
		if cursorInSegment(pos.Col, row.wrapSegment, rowIdx == end-1) {
			runes := t.Buffer.LineRunes(pos.Line)
			return rowIdx, LineVisualCol(runes, pos.Col) - row.startVisual
		}
	}
	return end - 1, layout.rows[end-1].width
}
```

Replace `positionAtVisualRow` with:

```go
func (t *Tab) positionAtVisualRow(rowIdx, targetCell, contentW int) Position {
	rows := t.wrapLayoutFor(contentW).rows
	if rowIdx < 0 {
		rowIdx = 0
	}
	if rowIdx >= len(rows) {
		rowIdx = len(rows) - 1
	}
	if targetCell < 0 {
		targetCell = 0
	}
	row := rows[rowIdx]
	runes := t.Buffer.LineRunes(row.line)
	targetVisual := row.startVisual + targetCell
	col := RuneColAtVisual(runes, targetVisual)
	if col < row.startRune {
		col = row.startRune
	}
	if col > row.endRune {
		col = row.endRune
	}
	return Position{Line: row.line, Col: col}
}
```

Replace `clampWrapScroll` with:

```go
func (t *Tab) clampWrapScroll(contentW, viewH int) []wrapRow {
	rows := t.wrapLayoutFor(contentW).rows
	if t.WrapScrollY < 0 {
		t.WrapScrollY = 0
	}
	overscroll := viewH / 2
	if overscroll < 3 {
		overscroll = 3
	}
	max := len(rows) - viewH + overscroll
	if max < 0 {
		max = 0
	}
	if t.WrapScrollY > max {
		t.WrapScrollY = max
	}
	if t.WrapScrollY < len(rows) {
		t.ScrollY = rows[t.WrapScrollY].line
	}
	return rows
}
```

- [x] **Step 4: Add shared content invalidation**

Add to `Tab`:

```go
textRevision uint64
wrapCache    wrapLayout
```

Add:

```go
func (t *Tab) invalidateWrapLayout() {
	t.wrapCache = wrapLayout{}
}

func (t *Tab) markTextChanged() {
	t.textRevision++
	t.invalidateWrapLayout()
	t.StyleStale = true
}
```

Replace every successful text mutation's direct `StyleStale = true` with `markTextChanged()`. Call it after Reload buffer replacement, line-comment edits, and `applySnapshot`. No-op mutation paths must leave revision unchanged.

- [x] **Step 5: Run regressions**

```bash
gofmt -w internal/editor/tab.go internal/editor/wrap.go internal/editor/wrap_test.go internal/editor/comment.go internal/editor/undo.go
go test ./internal/editor -run 'Test(Wrap|VisualRow|RenderSoftWrap|MoveCursorSoftWrap|HitTestSoftWrap|ToggleLineComment|Undo|Redo|Revert|Reload)'
go test ./internal/editor
```

Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add internal/editor/tab.go internal/editor/wrap.go internal/editor/wrap_test.go internal/editor/comment.go internal/editor/undo.go internal/editor/tab_test.go
git commit -m "Cache soft-wrap layouts"
```

---

### Task 3: Cache relative syntax-highlight windows

**Files:**
- Modify: `internal/editor/highlight.go:21-110`
- Modify: `internal/editor/highlight_test.go:164-230`
- Modify: `internal/editor/tab.go:59-100, 630-830`
- Modify: `internal/editor/tab_test.go:1335-1398`
- Modify: `internal/app/fileops.go:148-160, 320-340`
- Test: `internal/app/fileops_test.go`

**Interfaces:**
- Produces: `highlightVisibleWindow(string, []string, int, int, theme.Theme) (int, [][]tcell.Style)`.
- Produces: `(*Tab).ensureHighlightWindow(int, int, theme.Theme)`.
- Produces: `(*Tab).highlightStylesForLine(int) []tcell.Style`.
- Produces: `(*Tab).SetPath(string)`.

- [x] **Step 1: Write failing relative-window tests**

Add to `internal/editor/highlight_test.go`:

```go
func TestHighlightVisibleWindowReturnsOnlyRequestedRows(t *testing.T) {
	lines := make([]string, 100000)
	for i := range lines {
		lines[i] = "{\"value\": 1}"
	}
	start, rows := highlightVisibleWindow("large.json", lines, 50000, 40, theme.Default())
	if start != 50000 || len(rows) != 40 {
		t.Fatalf("start=%d rows=%d, want 50000/40", start, len(rows))
	}
}

func TestHighlightVisibleWindowClampsEdges(t *testing.T) {
	lines := []string{"one", "two", "three"}
	start, rows := highlightVisibleWindow("x.txt", lines, -4, 20, theme.Default())
	if start != 0 || len(rows) != 3 {
		t.Fatalf("start=%d rows=%d, want 0/3", start, len(rows))
	}
}
```

Add these helpers and render-cache tests to `internal/editor/tab_test.go`:

```go
func highlightCacheTestTab(lineCount int) *Tab {
	lines := make([]string, lineCount)
	for i := range lines {
		lines[i] = fmt.Sprintf(`{"line": %d, "value": "text"}`, i)
	}
	return &Tab{Path: "large.json", Buffer: &Buffer{Lines: lines}, StyleStale: true}
}

func highlightCacheScreen(t *testing.T) tcell.SimulationScreen {
	t.Helper()
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatal(err)
	}
	scr.SetSize(80, 20)
	t.Cleanup(scr.Fini)
	return scr
}

func TestTab_RenderReusesHighlightInsidePrefetchWindow(t *testing.T) {
	tab := highlightCacheTestTab(700)
	scr := highlightCacheScreen(t)
	tab.Render(scr, theme.Default(), 0, 0, 80, 20)
	first := reflect.ValueOf(tab.Styles).Pointer()

	tab.ScrollY = 40
	tab.cursorMoved = false
	tab.Render(scr, theme.Default(), 0, 0, 80, 20)
	if got := reflect.ValueOf(tab.Styles).Pointer(); got != first {
		t.Fatal("scroll inside prefetch window rebuilt styles")
	}
}

func TestTab_RenderRebuildsHighlightOutsidePrefetchWindow(t *testing.T) {
	tab := highlightCacheTestTab(700)
	scr := highlightCacheScreen(t)
	tab.Render(scr, theme.Default(), 0, 0, 80, 20)
	firstRows := tab.Styles
	firstPtr := &firstRows[0]

	tab.ScrollY = tab.highlightEnd + 1
	tab.cursorMoved = false
	tab.Render(scr, theme.Default(), 0, 0, 80, 20)
	if &tab.Styles[0] == firstPtr || tab.ScrollY < tab.highlightStart {
		t.Fatal("scroll outside prefetch window did not rebuild styles")
	}
}

func TestTab_RenderRebuildsHighlightForThemeAndPath(t *testing.T) {
	tab := highlightCacheTestTab(700)
	scr := highlightCacheScreen(t)
	base := theme.Default()
	tab.Render(scr, base, 0, 0, 80, 20)
	firstRows := tab.Styles
	firstPtr := &firstRows[0]

	changed := base
	changed.SynString = tcell.ColorRed
	tab.Render(scr, changed, 0, 0, 80, 20)
	if &tab.Styles[0] == firstPtr {
		t.Fatal("theme change reused stale styles")
	}

	themeRows := tab.Styles
	themePtr := &themeRows[0]
	tab.SetPath("data.yaml")
	if !tab.StyleStale {
		t.Fatal("path change did not invalidate lexer cache")
	}
	tab.Render(scr, changed, 0, 0, 80, 20)
	if &tab.Styles[0] == themePtr {
		t.Fatal("path change reused stale styles")
	}
}
```

Add a file-rename regression to `internal/app/fileops_test.go`:

```go
func TestDoRenameFileInvalidatesHighlightPath(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "data.txt")
	if err := os.WriteFile(oldPath, []byte(`{"value": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, root)
	a.openFile(oldPath)
	a.tabs[0].StyleStale = false

	a.doRenameFile(oldPath, "data.json")

	if got := a.tabs[0].Path; got != filepath.Join(root, "data.json") {
		t.Fatalf("tab path=%q", got)
	}
	if !a.tabs[0].StyleStale {
		t.Fatal("lexer-sensitive rename did not invalidate highlight cache")
	}
}
```

Extend `TestDoRenameFolder_RewritesDescendantTabPaths` with:

```go
a.tabs[0].StyleStale = false
a.doRenameFolder(oldDir, "renamed")
if !a.tabs[0].StyleStale {
	t.Fatal("folder rename did not invalidate lexer-sensitive path cache")
}
```

Replace its existing `a.doRenameFolder(oldDir, "renamed")` line rather than calling the rename twice.

- [x] **Step 2: Run tests and confirm failure**

```bash
go test ./internal/editor ./internal/app -run 'Test(HighlightVisibleWindow|Tab_RenderReuse|Tab_RenderRebuild|Rename.*Style)'
```

Expected: FAIL because the relative window and path setter do not exist.

- [x] **Step 3: Implement the relative highlighter**

In `internal/editor/highlight.go` add:

```go
func highlightVisibleWindow(filename string, lines []string, startLine, height int, t theme.Theme) (int, [][]tcell.Style) {
	if height <= 0 || len(lines) == 0 || startLine >= len(lines) {
		return 0, nil
	}
	if startLine < 0 {
		startLine = 0
	}
	endLine := startLine + height
	if endLine > len(lines) {
		endLine = len(lines)
	}
	leadStart := startLine - highlightLeadLines
	if leadStart < 0 {
		leadStart = 0
	}
	leadEnd := endLine + highlightLeadLines
	if leadEnd > len(lines) {
		leadEnd = len(lines)
	}
	leadStyles := highlightSource(filename, strings.Join(lines[leadStart:leadEnd], "\n"), t)
	rows := make([][]tcell.Style, endLine-startLine)
	for line := startLine; line < endLine; line++ {
		idx := line - leadStart
		if idx >= len(leadStyles) {
			break
		}
		rows[line-startLine] = leadStyles[idx]
	}
	return startLine, rows
}
```

Keep `HighlightVisible` as a compatibility wrapper:

```go
func HighlightVisible(filename string, lines []string, startLine, height int, t theme.Theme) [][]tcell.Style {
	styles := make([][]tcell.Style, len(lines))
	start, rows := highlightVisibleWindow(filename, lines, startLine, height, t)
	if start < 0 || start >= len(styles) {
		return styles
	}
	copy(styles[start:], rows)
	return styles
}
```

`Tab.RenderSoftWrap` must use `highlightVisibleWindow` through `ensureHighlightWindow`, not this absolute compatibility wrapper.

- [x] **Step 4: Implement the prefetched Tab cache**

Add:

```go
const highlightPrefetchLines = 256
```

Replace `lastHighlightScrollY` and `lastHighlightHeight` with:

```go
highlightStart    int
highlightEnd      int
highlightTheme    theme.Theme
hasHighlightTheme bool
```

Implement:

```go
func (t *Tab) ensureHighlightWindow(start, height int, th theme.Theme) {
	if height <= 0 || t.Buffer == nil {
		t.Styles = nil
		return
	}
	end := start + height
	if end > t.Buffer.LineCount() {
		end = t.Buffer.LineCount()
	}
	if !t.StyleStale && t.hasHighlightTheme && t.highlightTheme == th &&
		start >= t.highlightStart && end <= t.highlightEnd {
		return
	}
	cacheStart := start - highlightPrefetchLines
	if cacheStart < 0 {
		cacheStart = 0
	}
	cacheEnd := end + highlightPrefetchLines
	if cacheEnd > t.Buffer.LineCount() {
		cacheEnd = t.Buffer.LineCount()
	}
	windowStart, rows := highlightVisibleWindow(
		t.Path, t.Buffer.Lines, cacheStart, cacheEnd-cacheStart, th,
	)
	t.Styles = rows
	t.highlightStart = windowStart
	t.highlightEnd = windowStart + len(rows)
	t.highlightTheme = th
	t.hasHighlightTheme = true
	t.StyleStale = false
}

func (t *Tab) highlightStylesForLine(line int) []tcell.Style {
	idx := line - t.highlightStart
	if idx < 0 || idx >= len(t.Styles) {
		return nil
	}
	return t.Styles[idx]
}

func (t *Tab) SetPath(path string) {
	if t.Path == path {
		return
	}
	t.Path = path
	t.StyleStale = true
}
```

Call `ensureHighlightWindow` from `RenderSoftWrap` and replace absolute `Styles[lineIdx]` lookups with `highlightStylesForLine(lineIdx)`. Update both rename flows in `internal/app/fileops.go` to call `SetPath`.

- [x] **Step 5: Run regressions**

```bash
gofmt -w internal/editor/highlight.go internal/editor/highlight_test.go internal/editor/tab.go internal/editor/tab_test.go internal/app/fileops.go internal/app/fileops_test.go
go test ./internal/editor -run 'Test(Highlight|Tab_Render|RenderSoftWrap)'
go test ./internal/app -run 'Test.*Rename'
go test ./internal/editor ./internal/app
```

Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add internal/editor/highlight.go internal/editor/highlight_test.go internal/editor/tab.go internal/editor/tab_test.go internal/app/fileops.go internal/app/fileops_test.go
git commit -m "Cache syntax highlight windows"
```

---

### Task 4: Add benchmarks and complete verification

**Files:**
- Create: `internal/editor/performance_test.go`
- Modify: `docs/superpowers/plans/2026-08-04-large-text-performance.md`
- Verify: `docs/superpowers/specs/2026-08-04-large-text-performance-design.md`

**Interfaces:**
- Produces: `BenchmarkRenderLargeJSONCached`.
- Produces: `BenchmarkMoveCursorSoftWrapCached`.
- Produces: `BenchmarkHighlightVisibleWindowLargeFile`.

- [x] **Step 1: Add benchmark fixtures**

Create `internal/editor/performance_test.go` with the repository header:

```go
package editor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

func benchmarkJSON(lineCount int) string {
	var out strings.Builder
	out.Grow(lineCount * 96)
	out.WriteString("[\n")
	for i := 0; i < lineCount; i++ {
		fmt.Fprintf(&out, "  {\"id\": %d, \"title\": \"城市生命线 %d\", \"enabled\": true},\n", i, i)
	}
	out.WriteString("]\n")
	return out.String()
}

func benchmarkScreen(b *testing.B, width, height int) tcell.SimulationScreen {
	b.Helper()
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		b.Fatal(err)
	}
	scr.SetSize(width, height)
	b.Cleanup(scr.Fini)
	return scr
}
```

- [x] **Step 2: Add cached and cache-miss benchmarks**

Add:

```go
func BenchmarkRenderLargeJSONCached(b *testing.B) {
	tab, _ := NewTab("")
	tab.SetPath("large.json")
	tab.Buffer = NewBuffer(benchmarkJSON(100000))
	tab.markTextChanged()
	scr := benchmarkScreen(b, 120, 40)
	tab.Render(scr, theme.Default(), 0, 0, 120, 40)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tab.ScrollY = (i * 3) % 200
		tab.Render(scr, theme.Default(), 0, 0, 120, 40)
	}
}

func BenchmarkMoveCursorSoftWrapCached(b *testing.B) {
	tab, _ := NewTab("")
	tab.Buffer = NewBuffer(benchmarkJSON(100000))
	tab.markTextChanged()
	tab.wrapLayoutFor(80)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tab.MoveCursorSoftWrap(1, 87, false)
	}
}

func BenchmarkHighlightVisibleWindowLargeFile(b *testing.B) {
	lines := strings.Split(benchmarkJSON(100000), "\n")
	th := theme.Default()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		highlightVisibleWindow("large.json", lines, 50000, 552, th)
	}
}
```

Add the cache-miss benchmark:

```go
func BenchmarkRenderLargeJSONCacheMiss(b *testing.B) {
	tab, _ := NewTab("")
	tab.SetPath("large.json")
	tab.Buffer = NewBuffer(benchmarkJSON(100000))
	tab.markTextChanged()
	scr := benchmarkScreen(b, 120, 40)
	tab.Render(scr, theme.Default(), 0, 0, 120, 40)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			tab.ScrollY = 0
		} else {
			tab.ScrollY = 1000
		}
		tab.cursorMoved = false
		tab.Render(scr, theme.Default(), 0, 0, 120, 40)
	}
}
```

- [x] **Step 3: Run benchmarks and record evidence**

```bash
gofmt -w internal/editor/performance_test.go
go test -run '^$' -bench 'Benchmark(RenderLargeJSON|MoveCursorSoftWrap|HighlightVisibleWindow)' -benchmem ./internal/editor
```

Expected: all benchmarks complete. Record `ns/op`, `B/op`, and `allocs/op` under this step. Cached render and cached wrap movement must allocate substantially less than cache-miss paths.

Apple M3, darwin/arm64 evidence from August 4, 2026:

```text
BenchmarkRenderLargeJSONCached-8              595817 ns/op     752540 B/op    10925 allocs/op
BenchmarkRenderLargeJSONCacheMiss-8         15391298 ns/op   20332855 B/op   260688 allocs/op
BenchmarkMoveCursorSoftWrapCached-8               53.50 ns/op       76 B/op        2 allocs/op
BenchmarkHighlightVisibleWindowLargeFile-8  19760878 ns/op   26854612 B/op   329992 allocs/op
```

- [x] **Step 4: Run focused regressions**

```bash
go test ./internal/app -run 'Test(ProcessEventBurst|RunBatchesNavigationFrames|EventCanShareFrame)'
go test ./internal/editor -run 'Test(HighlightVisibleWindow|WrapLayout|VisualRowForPosition|Tab_RenderReuse|Tab_RenderRebuild|RenderSoftWrap)'
```

Expected: PASS.

- [x] **Step 5: Run full tests and builds**

```bash
git diff --check
make test
make build
make build-linux
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/spice-edit-app-windows.test.exe ./internal/app
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/spiceedit-windows-amd64.exe .
```

Expected: no diff errors; race-enabled tests and all builds pass.

Completed on August 4, 2026: `make test`, `make build`, `make build-linux`, the Windows app test compile, and the Windows binary build all passed.

- [x] **Step 6: Verify the supplied JSON manually**

Run:

```bash
./bin/spiceedit '/Users/itstamen/workspace/doc_factory/2608珊瑚海/slide_jobs.json'
```

Rapidly exercise PageUp/PageDown, arrows, wheel scrolling, shifted selection, insertion, Backspace, and Delete with soft wrap off and on. Confirm the editor remains responsive and does not replay every intermediate page. Use a disposable copy for destructive editing tests.

Verified on August 4, 2026 with disposable copies of the supplied 20,210-byte, 557-line JSON. Rapid PageDown and arrow bursts completed with soft wrap disabled and enabled. Separate continuous 400-rune insert, 400-backspace, and 200-delete bursts remained responsive in both modes. Automated burst tests cover wheel scrolling and shifted selection state preservation.

- [x] **Step 7: Commit benchmark evidence**

```bash
git add internal/editor/performance_test.go docs/superpowers/plans/2026-08-04-large-text-performance.md
git commit -m "Benchmark large text editing"
```

- [x] **Step 8: Audit final state**

```bash
git status --short --branch
git log -5 --oneline
```

Expected: only the pre-existing Kitty draft files remain uncommitted. Do not push unless the user explicitly requests it.
