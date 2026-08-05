// =============================================================================
// File: internal/editor/tab.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/termtext"
	"github.com/cloudmanic/spice-edit/internal/theme"
)

// defaultGutterWidth is the line-number column width for files up to 9999
// lines: five digits plus a one-cell pad on the right, with the git
// change-bar sitting in the blank cell at the far-left of the right-aligned
// number. Larger files grow the gutter via gutterWidthFor so the marker
// never overlaps the first digit.
const defaultGutterWidth = 6

const (
	// highlightPrefetchLines keeps nearby scrolls inside one syntax cache window.
	highlightPrefetchLines = 256

	// highlightWholeFileLines avoids cache-boundary recolouring for normal-sized
	// files. Larger files retain a bounded moving window.
	highlightWholeFileLines = 2048
)

// gutterWidthFor returns the line-number column width for a buffer of
// lineCount lines. It keeps defaultGutterWidth for files that fit and grows
// by one cell per extra digit so the git change-bar always has a blank
// leading cell to sit in. Without this, a 10000-line file would render
// "10000" as "▌0000" with the bar overwriting the first digit, because the
// right-aligned number fills every cell the marker shares.
func gutterWidthFor(lineCount int) int {
	if lineCount <= 0 {
		return defaultGutterWidth
	}
	if w := len(strconv.Itoa(lineCount)) + 2; w > defaultGutterWidth {
		return w
	}
	return defaultGutterWidth
}

// GitLineChange describes the marker rendered in the editor gutter for a line.
type GitLineChange int

const (
	GitLineNone GitLineChange = iota
	GitLineModified
	GitLineAdded
	GitLineDeleted
)

// Tab is a single open file. It owns the on-disk path, the in-memory buffer,
// the per-tab view state (scroll position, cursor, selection anchor), the
// cached syntax-highlight styles, and a dirty flag.
type Tab struct {
	Path        string // Empty for an unsaved/scratch tab.
	Buffer      *Buffer
	Cursor      Position // Where new typed text appears.
	Anchor      Position // Selection anchor; equals Cursor when nothing is selected.
	ScrollY     int      // Index of the first visible line.
	WrapScrollY int      // Index of the first visible soft-wrapped visual row.
	ScrollX     int      // Index of the first visible column (rune-indexed).
	Dirty       bool
	Styles      [][]tcell.Style
	StyleStale  bool
	GitLines    map[int]GitLineChange

	// textRevision changes after every successful text mutation. The wrap
	// layout is valid only for the width and revision it was built from.
	textRevision uint64
	wrapCache    wrapLayout

	// The relative syntax window covers [highlightStart, highlightEnd). Nearby
	// scrolls reuse it until content, path, theme, or window coverage changes.
	highlightStart    int
	highlightEnd      int
	highlightTheme    theme.Theme
	hasHighlightTheme bool

	// Mtime is the file's modification time as of the last successful
	// read or write. The app's periodic disk-reconcile loop compares it
	// against the live mtime to detect external edits.
	Mtime time.Time

	// DiskGone is set when the most recent disk check found the file
	// missing. It exists so we only flash the "deleted on disk" warning
	// once, instead of re-flashing every reconcile tick.
	DiskGone bool

	// cursorMoved is set by every method that changes Cursor; Render
	// consumes it to decide whether to scroll the viewport so the cursor
	// is visible. Without this flag, mouse-wheel scrolling is fought by
	// every redraw — EnsureVisible would snap us back to the cursor.
	cursorMoved bool

	// Undo / redo stacks plus the original on-open snapshot used by
	// RevertFile. See undo.go for the push / coalescing rules and the
	// public Undo / Redo / RevertFile entry points.
	undoStack     []snapshot
	redoStack     []snapshot
	undoOriginal  snapshot
	lastUndoGroup undoGroup
	lastUndoAt    time.Time

	// Mode is "" for editable text, imageMode for an inline image preview,
	// or externalMode for a file delegated to the host application. Read-only
	// tabs reuse Tab so tab switching and modal routing stay shared.
	Mode     string
	Image    image.Image // populated when Mode == imageMode
	ImageFmt string      // "png" / "jpeg" / "gif" — for the status bar

	// imageID remains stable for the lifetime of an image tab, while
	// imageRevision changes after each successful image reload. They remain
	// private so graphics consumers must use the immutable ImageView snapshot.
	imageID       uint64
	imageRevision uint64

	// Find state — populated when the user opens the find bar and
	// types a query. The UI layer (App) owns the bar geometry and
	// keystroke routing; the tab owns the query, the resolved match
	// list, and the index of the "current" match so the query
	// survives switching tabs and re-opening the bar.
	FindQuery   string
	FindMatches []Match
	FindIndex   int // -1 = no current match; otherwise an index into FindMatches.

	// IndentUnit is the string the editor inserts when the user presses
	// Tab. Detected on file open (DetectIndent) so the editor matches
	// whatever the file already does — a tab-indented Go file gets a
	// real tab; a 2-space-indented file gets two spaces. Mixed-style
	// files take the dominant signal.
	IndentUnit string
}

// NewTab opens path as editable text, an inline image, or a read-only external
// placeholder. Missing paths remain empty text tabs for the New File workflow.
func NewTab(path string) (*Tab, error) {
	kind, err := classifyFile(path)
	if err != nil {
		return nil, err
	}
	switch kind {
	case fileKindImage:
		t, imageErr := newImageTab(path)
		if imageErr != nil {
			return newExternalTab(path), nil
		}
		return t, nil
	case fileKindExternal:
		return newExternalTab(path), nil
	}
	var data []byte
	var mtime time.Time
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		data = b
		// Record the on-disk mtime so the app can detect external edits
		// later. A missing file leaves mtime as the zero value, which is
		// fine — the reconcile loop handles that case explicitly.
		if info, statErr := os.Stat(path); statErr == nil {
			mtime = info.ModTime()
		}
	}
	t := &Tab{
		Path:       path,
		Buffer:     NewBuffer(string(data)),
		StyleStale: true,
		Mtime:      mtime,
	}
	t.IndentUnit = DetectIndent(t.Buffer.Lines, path)
	// Record the on-open buffer state so RevertFile has somewhere to
	// rewind to even after the user has typed away.
	t.initUndo()
	return t, nil
}

// newImageTab decodes path as an image and returns a Tab in image
// preview mode. The buffer is left empty (image tabs ignore it) but
// allocated so any code that pokes at t.Buffer doesn't have to nil-check.
func newImageTab(path string) (*Tab, error) {
	img, format, err := decodeImageFile(path)
	if err != nil {
		return nil, err
	}
	var mtime time.Time
	if info, statErr := os.Stat(path); statErr == nil {
		mtime = info.ModTime()
	}
	t := &Tab{
		Path:          path,
		Buffer:        NewBuffer(""),
		Mtime:         mtime,
		Mode:          imageMode,
		Image:         img,
		ImageFmt:      format,
		imageID:       allocateImageID(),
		imageRevision: 1,
	}
	// Capture the empty original snapshot so CanRevert / RevertFile
	// behave sensibly even though image tabs are read-only — they'll
	// just always report "nothing to revert".
	t.initUndo()
	return t, nil
}

// IsImage reports whether the tab is an image-preview, not a text editor.
// Callers use this to skip text-only behaviour (cursor placement, key
// dispatch, save, etc.) without having to know about Mode strings.
func (t *Tab) IsImage() bool {
	return t.Mode == imageMode
}

// ImageView returns a read-only identity, revision, and image snapshot for a
// graphics presenter. Non-image or incomplete image tabs do not have a view.
func (t *Tab) ImageView() (ImageView, bool) {
	if t == nil || !t.IsImage() || t.Image == nil {
		return ImageView{}, false
	}
	return ImageView{
		ID:       t.imageID,
		Revision: t.imageRevision,
		Image:    t.Image,
	}, true
}

// DisplayName returns the basename of Path, or "untitled" for unsaved tabs.
func (t *Tab) DisplayName() string {
	if t.Path == "" {
		return "untitled"
	}
	return filepath.Base(t.Path)
}

// SetPath updates the lexer-sensitive filename and invalidates highlighting.
func (t *Tab) SetPath(path string) {
	if t.Path == path {
		return
	}
	t.Path = path
	t.StyleStale = true
}

// invalidateWrapLayout drops the width-sensitive visual-row index.
func (t *Tab) invalidateWrapLayout() {
	t.wrapCache = wrapLayout{}
}

// markTextChanged invalidates every cache derived from buffer content.
func (t *Tab) markTextChanged() {
	t.textRevision++
	t.invalidateWrapLayout()
	t.StyleStale = true
}

// highlightWindowBounds returns the bounded cache range around the viewport.
// Normal-sized files stay fully highlighted so scrolling never crosses a
// syntax-cache boundary.
func (t *Tab) highlightWindowBounds(start, end int) (int, int) {
	lineCount := t.Buffer.LineCount()
	if lineCount <= highlightWholeFileLines {
		return 0, lineCount
	}
	cacheStart := start - highlightPrefetchLines
	if cacheStart < 0 {
		cacheStart = 0
	}
	cacheEnd := end + highlightPrefetchLines
	if cacheEnd > lineCount {
		cacheEnd = lineCount
	}
	return cacheStart, cacheEnd
}

// rebuildHighlightWindow replaces the syntax cache after content, path,
// theme, or a non-overlapping viewport jump makes every old row unusable.
func (t *Tab) rebuildHighlightWindow(start, end int, th theme.Theme) {
	windowStart, rows := highlightVisibleWindow(
		t.Path, t.Buffer.Lines, start, end-start, th,
	)
	t.Styles = rows
	t.highlightStart = windowStart
	t.highlightEnd = windowStart + len(rows)
	t.highlightTheme = th
	t.hasHighlightTheme = true
	t.StyleStale = false
}

// extendHighlightWindow adds only missing rows, then trims the opposite edge
// back to the desired bounded range. Existing overlapping style rows remain
// intact so visible text does not change colour at a cache boundary.
func (t *Tab) extendHighlightWindow(start, end int, th theme.Theme) {
	if start < t.highlightStart {
		_, leading := highlightVisibleWindow(
			t.Path, t.Buffer.Lines, start, t.highlightStart-start, th,
		)
		rows := make([][]tcell.Style, 0, len(leading)+len(t.Styles))
		rows = append(rows, leading...)
		rows = append(rows, t.Styles...)
		t.Styles = rows
		t.highlightStart = start
	}
	if end > t.highlightEnd {
		appendStart := t.highlightEnd
		_, trailing := highlightVisibleWindow(
			t.Path, t.Buffer.Lines, appendStart, end-appendStart, th,
		)
		t.Styles = append(t.Styles, trailing...)
		t.highlightEnd = appendStart + len(trailing)
	}
	if t.highlightStart < start {
		t.Styles = t.Styles[start-t.highlightStart:]
		t.highlightStart = start
	}
	if t.highlightEnd > end {
		t.Styles = t.Styles[:end-t.highlightStart]
		t.highlightEnd = end
	}
	t.highlightTheme = th
	t.hasHighlightTheme = true
	t.StyleStale = false
}

// ensureHighlightWindow keeps a prefetched relative style window around the
// visible logical lines, rebuilding only when the cached inputs no longer fit.
func (t *Tab) ensureHighlightWindow(start, height int, th theme.Theme) {
	if height <= 0 || t.Buffer == nil {
		t.Styles = nil
		return
	}
	if start < 0 {
		start = 0
	}
	lineCount := t.Buffer.LineCount()
	if start >= lineCount {
		t.Styles = nil
		t.highlightStart = 0
		t.highlightEnd = 0
		return
	}
	end := start + height
	if end > lineCount {
		end = lineCount
	}
	if !t.StyleStale && t.hasHighlightTheme && t.highlightTheme == th &&
		start >= t.highlightStart && end <= t.highlightEnd {
		return
	}

	cacheStart, cacheEnd := t.highlightWindowBounds(start, end)
	cacheValid := !t.StyleStale && t.hasHighlightTheme && t.highlightTheme == th && len(t.Styles) > 0
	cacheOverlaps := cacheStart < t.highlightEnd && cacheEnd > t.highlightStart
	if !cacheValid || !cacheOverlaps {
		t.rebuildHighlightWindow(cacheStart, cacheEnd, th)
		return
	}
	t.extendHighlightWindow(cacheStart, cacheEnd, th)
}

// highlightStylesForLine maps an absolute logical line into the relative
// syntax window.
func (t *Tab) highlightStylesForLine(line int) []tcell.Style {
	idx := line - t.highlightStart
	if idx < 0 || idx >= len(t.Styles) {
		return nil
	}
	return t.Styles[idx]
}

// Save writes the buffer to disk and clears Dirty. It is an error to call
// Save on an untitled tab — callers should prompt for a path first. Mtime
// is refreshed so the disk-reconcile loop doesn't immediately think the
// file we just wrote was changed by someone else. Read-only tabs return an
// error because they have no editable text buffer to persist.
func (t *Tab) Save() error {
	if t.IsReadOnly() {
		return fmt.Errorf("read-only tabs cannot be saved")
	}
	if t.Path == "" {
		return fmt.Errorf("no path set for tab")
	}
	if err := os.WriteFile(t.Path, []byte(t.Buffer.String()), 0644); err != nil {
		return err
	}
	t.Dirty = false
	t.DiskGone = false
	if info, err := os.Stat(t.Path); err == nil {
		t.Mtime = info.ModTime()
	}
	// Save is a natural logical-step boundary: the next typing burst is
	// clearly a separate intent, so don't let it merge into whatever was
	// in flight before the save.
	t.breakUndoGroup()
	return nil
}

// Reload re-reads the file from disk into the buffer. Cursor and anchor
// are clamped to the new content (so the user keeps roughly their place
// instead of getting snapped to line 0); ScrollY is left alone and gets
// clamped on the next render. Dirty is cleared and the syntax cache is
// invalidated. Image tabs decode the file again instead of replacing the text
// buffer, while external tabs remain delegated to the host application.
func (t *Tab) Reload() error {
	if t.Path == "" {
		return fmt.Errorf("no path set for tab")
	}
	if t.IsImage() {
		img, format, err := decodeImageFile(t.Path)
		if err != nil {
			return err
		}
		info, err := os.Stat(t.Path)
		if err != nil {
			return err
		}
		t.Image = img
		t.ImageFmt = format
		t.imageRevision++
		t.Mtime = info.ModTime()
		t.DiskGone = false
		return nil
	}
	if t.IsExternal() {
		return fmt.Errorf("external tabs are read-only")
	}
	data, err := os.ReadFile(t.Path)
	if err != nil {
		return err
	}
	info, err := os.Stat(t.Path)
	if err != nil {
		return err
	}
	t.Buffer = NewBuffer(string(data))
	t.Cursor = t.Buffer.Clamp(t.Cursor)
	t.Anchor = t.Cursor // drop any selection — line indices may have shifted.
	t.Dirty = false
	t.DiskGone = false
	t.Mtime = info.ModTime()
	t.markTextChanged()
	t.cursorMoved = true
	// Reload re-establishes "what's on disk" as the new baseline. Any
	// prior undo history is meaningless now (the line indices may have
	// shifted, and the user explicitly asked to take the disk version),
	// so reset both stacks and the revert anchor.
	t.initUndo()
	return nil
}

// HasSelection reports whether the tab currently has a non-empty selection.
func (t *Tab) HasSelection() bool {
	return t.Cursor != t.Anchor
}

// SelectionText returns the currently selected text, or "" if nothing is
// selected. The text is always returned in document order.
func (t *Tab) SelectionText() string {
	if !t.HasSelection() {
		return ""
	}
	return t.Buffer.Substring(t.Anchor, t.Cursor)
}

// DeleteSelection removes the selected range and collapses the cursor to the
// start of the selection. A no-op when nothing is selected.
func (t *Tab) DeleteSelection() {
	if t.IsReadOnly() || !t.HasSelection() {
		return
	}
	// Selection deletes are always their own undo step — they can wipe
	// out a lot in one stroke, and merging them into adjacent typing
	// would make the next undo recover content the user thought was
	// just-deleted.
	t.pushUndo(undoGroupStructural)
	pos := t.Buffer.DeleteRange(t.Anchor, t.Cursor)
	t.Cursor = pos
	t.Anchor = pos
	t.Dirty = true
	t.markTextChanged()
	t.cursorMoved = true
}

// InsertString inserts s at the cursor (replacing any selection first) and
// advances the cursor past the inserted text. Always recorded as a
// structural undo step — pasted text or "\n" presses shouldn't merge
// with the surrounding typing burst. No-op on read-only tabs.
func (t *Tab) InsertString(s string) {
	if t.IsReadOnly() {
		return
	}
	if t.HasSelection() {
		// DeleteSelection records its own structural step. Don't push a
		// second one here or the user would have to undo twice to get
		// back to the pre-paste-with-selection state.
		t.DeleteSelection()
	} else {
		t.pushUndo(undoGroupStructural)
	}
	t.Cursor = t.Buffer.InsertString(t.Cursor, s)
	t.Anchor = t.Cursor
	t.Dirty = true
	t.markTextChanged()
	t.cursorMoved = true
}

// InsertRune inserts a single typed character at the cursor. Coalesces
// with adjacent runes inside the undo window so a typed word collapses
// into a single undo step rather than one entry per keystroke. No-op
// on read-only tabs.
func (t *Tab) InsertRune(r rune) {
	if t.IsReadOnly() {
		return
	}
	if t.HasSelection() {
		// First-rune-after-selection: let DeleteSelection capture the
		// pre-state, then run the insert without a second push.
		t.DeleteSelection()
	} else {
		t.pushUndo(undoGroupTyping)
	}
	t.Cursor = t.Buffer.InsertString(t.Cursor, string(r))
	t.Anchor = t.Cursor
	t.Dirty = true
	t.markTextChanged()
	t.cursorMoved = true
}

// Backspace deletes the character before the cursor (or the selection if any).
// Coalesces with adjacent backspaces inside the undo window. No-op on
// read-only tabs.
func (t *Tab) Backspace() {
	if t.IsReadOnly() {
		return
	}
	if t.HasSelection() {
		t.DeleteSelection()
		return
	}
	if t.Cursor.Line == 0 && t.Cursor.Col == 0 {
		return
	}
	t.pushUndo(undoGroupBackspace)
	var prev Position
	if t.Cursor.Col == 0 {
		prev.Line = t.Cursor.Line - 1
		prev.Col = len([]rune(t.Buffer.Lines[prev.Line]))
	} else {
		prev = Position{Line: t.Cursor.Line, Col: t.Cursor.Col - 1}
	}
	t.Cursor = t.Buffer.DeleteRange(prev, t.Cursor)
	t.Anchor = t.Cursor
	t.Dirty = true
	t.markTextChanged()
	t.cursorMoved = true
}

// Delete removes the character after the cursor (or the selection if any).
// Coalesces with adjacent forward-deletes inside the undo window. No-op
// on read-only tabs.
func (t *Tab) Delete() {
	if t.IsReadOnly() {
		return
	}
	if t.HasSelection() {
		t.DeleteSelection()
		return
	}
	end := t.Buffer.EndPos()
	if t.Cursor == end {
		return
	}
	t.pushUndo(undoGroupDelete)
	var next Position
	line := []rune(t.Buffer.Lines[t.Cursor.Line])
	if t.Cursor.Col >= len(line) {
		next = Position{Line: t.Cursor.Line + 1, Col: 0}
	} else {
		next = Position{Line: t.Cursor.Line, Col: t.Cursor.Col + 1}
	}
	t.Cursor = t.Buffer.DeleteRange(t.Cursor, next)
	t.Anchor = t.Cursor
	t.Dirty = true
	t.markTextChanged()
	t.cursorMoved = true
}

// MoveCursor shifts the cursor by (dLine, dCol). When extend is true the
// anchor is left in place so the user is extending a selection.
func (t *Tab) MoveCursor(dLine, dCol int, extend bool) {
	cur := t.Cursor
	if dLine != 0 {
		cur.Line += dLine
		if cur.Line < 0 {
			cur.Line = 0
		}
		if cur.Line >= t.Buffer.LineCount() {
			cur.Line = t.Buffer.LineCount() - 1
		}
		runes := []rune(t.Buffer.Lines[cur.Line])
		if cur.Col > len(runes) {
			cur.Col = len(runes)
		}
	}
	if dCol != 0 {
		cur.Col += dCol
		if cur.Col < 0 {
			// Wrap to the end of the previous line.
			if cur.Line > 0 {
				cur.Line--
				cur.Col = len([]rune(t.Buffer.Lines[cur.Line]))
			} else {
				cur.Col = 0
			}
		} else {
			runes := []rune(t.Buffer.Lines[cur.Line])
			if cur.Col > len(runes) {
				if cur.Line < t.Buffer.LineCount()-1 {
					cur.Line++
					cur.Col = 0
				} else {
					cur.Col = len(runes)
				}
			}
		}
	}
	t.Cursor = cur
	if !extend {
		t.Anchor = cur
	}
	t.cursorMoved = true
	// Cursor moved on the user's explicit command — close any open
	// coalescing window so the next typing burst is a fresh undo step.
	t.breakUndoGroup()
}

// MoveCursorSoftWrap shifts the cursor vertically by wrapped visual rows.
func (t *Tab) MoveCursorSoftWrap(dRows, viewW int, extend bool) {
	contentW := viewW - gutterWidthFor(t.Buffer.LineCount()) - 1
	rowIdx, targetCell := t.visualRowForPosition(t.Cursor, contentW)
	t.Cursor = t.positionAtVisualRow(rowIdx+dRows, targetCell, contentW)
	if !extend {
		t.Anchor = t.Cursor
	}
	t.cursorMoved = true
	t.breakUndoGroup()
}

// MoveCursorTo sets the cursor to a specific buffer position. Position is
// clamped within the buffer; extend=true preserves the selection anchor.
func (t *Tab) MoveCursorTo(p Position, extend bool) {
	p = t.Buffer.Clamp(p)
	t.Cursor = p
	if !extend {
		t.Anchor = p
	}
	t.cursorMoved = true
	t.breakUndoGroup()
}

// MoveLineHome moves the cursor to column 0 of the current line.
func (t *Tab) MoveLineHome(extend bool) {
	t.Cursor.Col = 0
	if !extend {
		t.Anchor = t.Cursor
	}
	t.cursorMoved = true
	t.breakUndoGroup()
}

// MoveLineEnd moves the cursor to the last column of the current line.
func (t *Tab) MoveLineEnd(extend bool) {
	t.Cursor.Col = len([]rune(t.Buffer.Lines[t.Cursor.Line]))
	if !extend {
		t.Anchor = t.Cursor
	}
	t.cursorMoved = true
	t.breakUndoGroup()
}

// SelectAll selects the entire buffer (anchor at start, cursor at end).
func (t *Tab) SelectAll() {
	t.Anchor = Position{Line: 0, Col: 0}
	t.Cursor = t.Buffer.EndPos()
	t.cursorMoved = true
	t.breakUndoGroup()
}

// EnsureVisible scrolls the viewport so the cursor is on screen. The
// caller passes the editor area's width and height because the Tab itself
// doesn't know its render rect.
func (t *Tab) EnsureVisible(viewW, viewH int) {
	contentW := viewW - gutterWidthFor(t.Buffer.LineCount()) - 1
	if contentW < 1 {
		contentW = 1
	}
	if t.Cursor.Line < t.ScrollY {
		t.ScrollY = t.Cursor.Line
	}
	if t.Cursor.Line >= t.ScrollY+viewH {
		t.ScrollY = t.Cursor.Line - viewH + 1
	}
	if t.Cursor.Line >= 0 && t.Cursor.Line < t.Buffer.LineCount() {
		runes := t.Buffer.LineRunes(t.Cursor.Line)
		t.ScrollX = normalizedRuneCol(runes, t.ScrollX)
		cursorVisual := LineVisualCol(runes, t.Cursor.Col)
		scrollVisual := LineVisualCol(runes, t.ScrollX)
		if cursorVisual < scrollVisual {
			t.ScrollX = runeColAtOrAfterVisual(runes, cursorVisual)
			scrollVisual = LineVisualCol(runes, t.ScrollX)
		}
		if cursorVisual >= scrollVisual+contentW {
			targetVisual := cursorVisual - contentW + 1
			t.ScrollX = runeColAtOrAfterVisual(runes, targetVisual)
		}
	}
	if t.ScrollY < 0 {
		t.ScrollY = 0
	}
	if t.ScrollX < 0 {
		t.ScrollX = 0
	}
}

// Render draws the editor's content with soft wrap disabled. App calls
// RenderSoftWrap when the session-level view toggle is enabled.
func (t *Tab) Render(scr tcell.Screen, th theme.Theme, x, y, w, h int) {
	t.RenderSoftWrap(scr, th, x, y, w, h, false)
}

// RenderSoftWrap draws the editor's content (line numbers, code with syntax
// highlighting, selection, cursor) into the rectangle (x, y, w, h). Image tabs
// delegate to RenderImageFallback instead of drawing text.
func (t *Tab) RenderSoftWrap(scr tcell.Screen, th theme.Theme, x, y, w, h int, softWrap bool) {
	if t.IsExternal() {
		t.RenderExternal(scr, th, x, y, w, h)
		return
	}
	if t.IsImage() {
		t.RenderImageFallback(scr, th, x, y, w, h)
		return
	}
	// Only re-center on the cursor if the cursor moved this tick. Doing it
	// every render fights the user when they scroll with the wheel.
	if t.cursorMoved {
		if softWrap {
			t.EnsureVisibleSoftWrap(w, h)
		} else {
			t.EnsureVisible(w, h)
		}
		t.cursorMoved = false
	}
	gw := gutterWidthFor(t.Buffer.LineCount())
	contentX := x + gw + 1
	contentW := w - gw - 1
	if contentW < 1 {
		contentW = 1
	}

	var wrapRows []wrapRow
	var highlightStart, highlightHeight int
	if softWrap {
		t.ScrollX = 0
		wrapRows = t.clampWrapScroll(contentW, h)
		if t.WrapScrollY < len(wrapRows) {
			highlightStart = wrapRows[t.WrapScrollY].line
			lastRow := t.WrapScrollY + h - 1
			if lastRow >= len(wrapRows) {
				lastRow = len(wrapRows) - 1
			}
			highlightHeight = wrapRows[lastRow].line - highlightStart + 1
		}
	} else {
		t.clampScroll(h)
		highlightStart = t.ScrollY
		highlightHeight = h
	}
	t.ensureHighlightWindow(highlightStart, highlightHeight, th)

	bg := th.BG
	bgStyle := tcell.StyleDefault.Background(bg).Foreground(th.Text)

	// Paint the entire editor rectangle with the base background first so
	// any cells we don't draw (short lines, blank rows) still get themed.
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			scr.SetContent(cx, cy, ' ', nil, bgStyle)
		}
	}

	selStart, selEnd := PosOrdered(t.Anchor, t.Cursor)
	hasSel := t.HasSelection()

	cursorVisible := false
	cursorX, cursorY := 0, 0
	row := 0
	if !softWrap {
		for lineIdx := t.ScrollY; lineIdx < t.Buffer.LineCount() && row < h; lineIdx++ {
			runes := []rune(t.Buffer.Lines[lineIdx])
			segments := []wrapSegment{{
				startRune:   0,
				endRune:     len(runes),
				startVisual: 0,
				width:       LineVisualCol(runes, len(runes)),
			}}
			for segIdx, segment := range segments {
				if row >= h {
					break
				}
				if lineIdx >= t.Buffer.LineCount() {
					break
				}
				cy := y + row
				isCursorLine := lineIdx == t.Cursor.Line

				// Pick the row background — a hair lighter on the cursor's line so
				// the eye can catch where the caret is from across the screen.
				lineBg := bg
				if isCursorLine {
					lineBg = th.LineHL
				}
				lineBgStyle := tcell.StyleDefault.Background(lineBg).Foreground(th.Text)

				// Re-paint this row with its (possibly highlighted) bg.
				for cx := x; cx < x+w; cx++ {
					scr.SetContent(cx, cy, ' ', nil, lineBgStyle)
				}

				// Gutter / line number, right-aligned with one trailing space.
				// Wrapped continuation rows keep the gutter blank so the logical
				// line number is not repeated as if it were a real buffer line.
				numStr := fmt.Sprintf("%*d", gw-1, lineIdx+1)
				gutterStyle := tcell.StyleDefault.Background(lineBg).Foreground(th.Muted)
				if isCursorLine {
					gutterStyle = gutterStyle.Foreground(th.AccentSoft)
				}
				if segIdx == 0 {
					if marker, ok := t.GitLines[lineIdx]; ok && marker != GitLineNone {
						scr.SetContent(x, cy, gitLineMarkerRune(marker), nil, gutterStyle.Foreground(gitLineMarkerColor(th, marker)))
					}
					for i, r := range numStr {
						if i == 0 && t.GitLines[lineIdx] != GitLineNone {
							continue
						}
						scr.SetContent(x+i, cy, r, nil, gutterStyle)
					}
				}

				// Line content, with syntax styles, selection bg, and line bg. We
				// walk graphemes from the start of the line so tab stops anchor to
				// column 0 and every wide glyph owns all of its terminal cells.
				styles := t.highlightStylesForLine(lineIdx)
				lineScrollX := 0
				if !softWrap {
					lineScrollX = normalizedRuneCol(runes, t.ScrollX)
				}
				scrollVisual := LineVisualCol(runes, lineScrollX)
				if softWrap {
					scrollVisual = segment.startVisual
				}
				visualCol := 0 // visual cell offset from the start of the LINE
				for _, cluster := range lineClusters(runes) {
					width := clusterVisualWidth(cluster, visualCol)
					screenCol := visualCol - scrollVisual
					inSegment := cluster.EndRune > segment.startRune && cluster.StartRune < segment.endRune
					if inSegment && cluster.EndRune > lineScrollX && screenCol >= 0 && screenCol+width <= contentW {
						style := t.clusterStyle(lineIdx, cluster, styles, selStart, selEnd, hasSel, lineBg, th)
						if cluster.Text == "\t" {
							drawTabCells(scr, contentX+screenCol, cy, width, style)
						} else {
							termtext.DrawCluster(scr, contentX+screenCol, cy, contentW-screenCol, cluster, style)
						}
					}
					visualCol += width
				}

				// Overflow affordance: paint a muted '‹' / '›' over the leftmost /
				// rightmost content cell when the line extends past the viewport
				// in that direction. Without this hint a terminal user has no way
				// to tell that more content exists off-screen — there's no
				// scrollbar to clue them in. visualCol now equals the total
				// visual width of the line; scrollVisual is the visual cell
				// corresponding to ScrollX.
				overflowStyle := tcell.StyleDefault.Background(lineBg).Foreground(th.Muted)
				if !softWrap && lineScrollX > 0 {
					scr.SetContent(contentX, cy, '‹', nil, overflowStyle)
				}
				if !softWrap && visualCol-scrollVisual > contentW {
					scr.SetContent(contentX+contentW-1, cy, '›', nil, overflowStyle)
				}
				if isCursorLine && cursorInSegment(t.Cursor.Col, segment, segIdx == len(segments)-1) {
					cursorRunes := t.Buffer.LineRunes(t.Cursor.Line)
					cursorVisual := LineVisualCol(cursorRunes, t.Cursor.Col)
					cx := contentX + (cursorVisual - scrollVisual)
					if cx >= contentX && cx < contentX+contentW {
						cursorX, cursorY = cx, cy
						cursorVisible = true
					}
				}
				row++
			}
		}
	}
	if softWrap {
		row = 0
		for wrapIdx := t.WrapScrollY; wrapIdx < len(wrapRows) && row < h; wrapIdx++ {
			wrapRow := wrapRows[wrapIdx]
			lineIdx := wrapRow.line
			runes := []rune(t.Buffer.Lines[lineIdx])
			segment := wrapRow.wrapSegment
			cy := y + row
			isCursorLine := lineIdx == t.Cursor.Line

			lineBg := bg
			if isCursorLine {
				lineBg = th.LineHL
			}
			lineBgStyle := tcell.StyleDefault.Background(lineBg).Foreground(th.Text)
			for cx := x; cx < x+w; cx++ {
				scr.SetContent(cx, cy, ' ', nil, lineBgStyle)
			}

			numStr := fmt.Sprintf("%*d", gw-1, lineIdx+1)
			gutterStyle := tcell.StyleDefault.Background(lineBg).Foreground(th.Muted)
			if isCursorLine {
				gutterStyle = gutterStyle.Foreground(th.AccentSoft)
			}
			firstSegment := wrapIdx == 0 || wrapRows[wrapIdx-1].line != lineIdx
			if firstSegment {
				if marker, ok := t.GitLines[lineIdx]; ok && marker != GitLineNone {
					scr.SetContent(x, cy, gitLineMarkerRune(marker), nil, gutterStyle.Foreground(gitLineMarkerColor(th, marker)))
				}
				for i, r := range numStr {
					if i == 0 && t.GitLines[lineIdx] != GitLineNone {
						continue
					}
					scr.SetContent(x+i, cy, r, nil, gutterStyle)
				}
			}

			styles := t.highlightStylesForLine(lineIdx)
			visualCol := 0
			for _, cluster := range lineClusters(runes) {
				width := clusterVisualWidth(cluster, visualCol)
				screenCol := visualCol - segment.startVisual
				inSegment := cluster.EndRune > segment.startRune && cluster.StartRune < segment.endRune
				if inSegment && screenCol >= 0 && screenCol+width <= contentW {
					style := t.clusterStyle(lineIdx, cluster, styles, selStart, selEnd, hasSel, lineBg, th)
					if cluster.Text == "\t" {
						drawTabCells(scr, contentX+screenCol, cy, width, style)
					} else {
						termtext.DrawCluster(scr, contentX+screenCol, cy, contentW-screenCol, cluster, style)
					}
				}
				visualCol += width
			}

			lastSegment := wrapIdx == len(wrapRows)-1 || wrapRows[wrapIdx+1].line != lineIdx
			if isCursorLine && cursorInSegment(t.Cursor.Col, segment, lastSegment) {
				cursorRunes := t.Buffer.LineRunes(t.Cursor.Line)
				cursorVisual := LineVisualCol(cursorRunes, t.Cursor.Col)
				cx := contentX + (cursorVisual - segment.startVisual)
				if cx >= contentX && cx < contentX+contentW {
					cursorX, cursorY = cx, cy
					cursorVisible = true
				}
			}
			row++
		}
	}

	// Position the hardware cursor at its visual column (so a cursor
	// past a tab lands at the tab's *end* cell, not just rune-Col cells
	// to the right of ScrollX).
	if softWrap {
		if cursorVisible {
			scr.ShowCursor(cursorX, cursorY)
		} else {
			scr.HideCursor()
		}
		return
	}
	cy := y + (t.Cursor.Line - t.ScrollY)
	cursorRunes := t.Buffer.LineRunes(t.Cursor.Line)
	cursorVisual := LineVisualCol(cursorRunes, t.Cursor.Col)
	scrollVisual := LineVisualCol(cursorRunes, t.ScrollX)
	cx := contentX + (cursorVisual - scrollVisual)
	if cy >= y && cy < y+h && cx >= contentX && cx < contentX+contentW {
		scr.ShowCursor(cx, cy)
	} else {
		scr.HideCursor()
	}
}

// EnsureVisibleSoftWrap keeps the logical cursor line inside the visible
// viewport. V1 soft wrap deliberately keeps ScrollY logical-line based; precise
// visual-row scrolling inside one long wrapped line is deferred.
func (t *Tab) EnsureVisibleSoftWrap(viewW, viewH int) {
	contentW := viewW - gutterWidthFor(t.Buffer.LineCount()) - 1
	rowIdx, _ := t.visualRowForPosition(t.Cursor, contentW)
	if rowIdx < t.WrapScrollY {
		t.WrapScrollY = rowIdx
	}
	if rowIdx >= t.WrapScrollY+viewH {
		t.WrapScrollY = rowIdx - viewH + 1
	}
	t.clampWrapScroll(contentW, viewH)
	t.ScrollX = 0
}

// cursorInSegment reports whether a logical cursor column belongs to a wrapped
// segment. End-of-line belongs to the final segment.
func cursorInSegment(col int, segment wrapSegment, last bool) bool {
	if last {
		return col >= segment.startRune && col <= segment.endRune
	}
	return col >= segment.startRune && col < segment.endRune
}

// clusterStyle resolves syntax, selection, and find styles for an entire
// grapheme. Any selected or matched member rune styles the whole cluster so
// combining marks and wide glyphs cannot show mixed half-cell highlights.
func (t *Tab) clusterStyle(line int, cluster termtext.Cluster, styles []tcell.Style, selStart, selEnd Position, hasSel bool, lineBg tcell.Color, th theme.Theme) tcell.Style {
	style := tcell.StyleDefault.Background(lineBg).Foreground(th.Text)
	if cluster.StartRune < len(styles) {
		style = styles[cluster.StartRune].Background(lineBg)
	}
	if hasSel {
		for runeCol := cluster.StartRune; runeCol < cluster.EndRune; runeCol++ {
			position := Position{Line: line, Col: runeCol}
			if !PosLess(position, selStart) && PosLess(position, selEnd) {
				style = style.Background(th.Selection)
				break
			}
		}
	}
	for runeCol := cluster.StartRune; runeCol < cluster.EndRune; runeCol++ {
		if matchIndex := t.matchAtRune(line, runeCol); matchIndex >= 0 {
			if matchIndex == t.FindIndex {
				return style.Background(th.FindCurrent).Foreground(th.BG)
			}
			return style.Background(th.FindMatch)
		}
	}
	return style
}

// drawTabCells fills a tab stop with spaces. Tabs are layout whitespace, not
// wide glyphs, so painting their cells individually preserves the existing
// tab-stop appearance while still using grapheme geometry around them.
func drawTabCells(scr tcell.Screen, x, y, width int, style tcell.Style) {
	for cell := 0; cell < width; cell++ {
		scr.SetContent(x+cell, y, ' ', nil, style)
	}
}

// gitLineMarkerRune returns the gutter glyph for a git line change.
func gitLineMarkerRune(change GitLineChange) rune {
	if change == GitLineDeleted {
		return '▁'
	}
	return '▌'
}

// gitLineMarkerColor returns the gutter color for a git line change.
func gitLineMarkerColor(th theme.Theme, change GitLineChange) tcell.Color {
	if change == GitLineAdded {
		return th.GitAdded
	}
	if change == GitLineDeleted {
		return th.GitDeleted
	}
	return th.GitModified
}

// HitTest converts screen coordinates within this tab's render area to a
// buffer position. ok=false means the click was outside any line.
func (t *Tab) HitTest(localX, localY, w, h int) (Position, bool) {
	if localY < 0 || localY >= h {
		return Position{}, false
	}
	contentX := gutterWidthFor(t.Buffer.LineCount()) + 1
	line := t.ScrollY + localY
	if line < 0 || line >= t.Buffer.LineCount() {
		return Position{}, false
	}
	if localX < contentX {
		// Click in the gutter — treat as click at column 0 of that line.
		return Position{Line: line, Col: 0}, true
	}
	runes := []rune(t.Buffer.Lines[line])
	// Convert the click's screen column back to a rune column. Tabs and wide
	// graphemes expand to multiple cells, so we walk terminal visual columns
	// instead of subtracting rune indexes.
	lineScrollX := normalizedRuneCol(runes, t.ScrollX)
	scrollVisual := LineVisualCol(runes, lineScrollX)
	targetVisual := scrollVisual + (localX - contentX)
	col := RuneColAtVisual(runes, targetVisual)
	if col > len(runes) {
		col = len(runes)
	}
	if col < 0 {
		col = 0
	}
	return Position{Line: line, Col: col}, true
}

// HitTestSoftWrap converts coordinates to a buffer position using visual rows.
func (t *Tab) HitTestSoftWrap(localX, localY, w, h int) (Position, bool) {
	if localY < 0 || localY >= h {
		return Position{}, false
	}
	contentX := gutterWidthFor(t.Buffer.LineCount()) + 1
	contentW := w - contentX
	rows := t.clampWrapScroll(contentW, h)
	rowIdx := t.WrapScrollY + localY
	if rowIdx < 0 || rowIdx >= len(rows) {
		return Position{}, false
	}
	if localX < contentX {
		return Position{Line: rows[rowIdx].line, Col: rows[rowIdx].startRune}, true
	}
	return t.positionAtVisualRow(rowIdx, localX-contentX, contentW), true
}

// Scroll moves the viewport by delta lines (negative = up). Render runs
// clampScroll afterwards so the user never scrolls into pure void; here we
// just adjust the raw value.
func (t *Tab) Scroll(deltaLines int) {
	t.ScrollY += deltaLines
	if t.ScrollY < 0 {
		t.ScrollY = 0
	}
}

// ScrollSoftWrap moves the viewport by wrapped visual rows.
func (t *Tab) ScrollSoftWrap(deltaRows, viewW, viewH int) {
	contentW := viewW - gutterWidthFor(t.Buffer.LineCount()) - 1
	t.WrapScrollY += deltaRows
	t.clampWrapScroll(contentW, viewH)
}

// ScrollH moves the viewport horizontally by delta terminal cells (negative
// = left). Clamped at zero; the right side is naturally bounded by
// Render's contentW window — scrolling past the longest visible line just
// shows blank space, which is fine. Lives next to Scroll so the app's
// mouse-wheel dispatcher can treat horizontal and vertical wheels
// symmetrically.
func (t *Tab) ScrollH(deltaCols int) {
	if t.Buffer == nil || t.Buffer.LineCount() == 0 {
		return
	}
	line := t.Cursor.Line
	if line < 0 || line >= t.Buffer.LineCount() {
		line = 0
	}
	runes := t.Buffer.LineRunes(line)
	currentVisual := LineVisualCol(runes, normalizedRuneCol(runes, t.ScrollX))
	targetVisual := currentVisual + deltaCols
	if targetVisual < 0 {
		targetVisual = 0
	}
	t.ScrollX = runeColAtOrAfterVisual(runes, targetVisual)
}

// clampScroll keeps ScrollY inside a sensible range for the current viewport
// height. The max is "last line still on screen, plus a small overscroll
// pad" so the user can scroll the bottom of the file up to the middle of
// the viewport — which feels much better than abruptly stopping when the
// last line hits the bottom row.
func (t *Tab) clampScroll(viewH int) {
	if t.ScrollY < 0 {
		t.ScrollY = 0
	}
	overscroll := viewH / 2
	if overscroll < 3 {
		overscroll = 3
	}
	max := t.Buffer.LineCount() - viewH + overscroll
	if max < 0 {
		max = 0
	}
	if t.ScrollY > max {
		t.ScrollY = max
	}
}
