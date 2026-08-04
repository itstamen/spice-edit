// =============================================================================
// File: internal/editor/wrap.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-01
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

// wrapSegment is one visual row produced from a logical buffer line. Rune
// ranges are half-open and remain indexed into the original line so rendering,
// selection, and find highlighting can keep using the editor's logical model.
type wrapSegment struct {
	startRune   int
	endRune     int
	startVisual int
	width       int
}

// wrapRow is one visual row in the soft-wrap layout for the whole buffer.
type wrapRow struct {
	line int
	wrapSegment
}

// wrapLayout caches the visual rows and the starting visual-row index for
// every logical line at one editor width and text revision.
type wrapLayout struct {
	width      int
	revision   uint64
	rows       []wrapRow
	lineStarts []int
}

// wrapLine splits runes into grapheme-safe visual rows no wider than contentW
// when possible. A single grapheme or tab wider than contentW is kept whole in
// its own segment so rendering never slices a cluster across terminal rows.
func wrapLine(runes []rune, contentW int) []wrapSegment {
	if contentW < 1 {
		contentW = 1
	}
	if len(runes) == 0 {
		return []wrapSegment{{}}
	}

	var segments []wrapSegment
	startRune, startVisual, segWidth, visualCol := 0, 0, 0, 0
	for _, cluster := range lineClusters(runes) {
		width := clusterVisualWidth(cluster, visualCol)
		if segWidth > 0 && width > 0 && segWidth+width > contentW {
			segments = append(segments, wrapSegment{
				startRune:   startRune,
				endRune:     cluster.StartRune,
				startVisual: startVisual,
				width:       segWidth,
			})
			startRune = cluster.StartRune
			startVisual = visualCol
			segWidth = 0
		}
		segWidth += width
		visualCol += width
	}
	segments = append(segments, wrapSegment{
		startRune:   startRune,
		endRune:     len(runes),
		startVisual: startVisual,
		width:       segWidth,
	})
	return segments
}

// wrapLayoutFor returns the cached soft-wrap layout for contentW, rebuilding
// it only after the width or text revision changes.
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

// wrapRows returns the visual rows from the width-and-revision cache.
func (t *Tab) wrapRows(contentW int) []wrapRow {
	return t.wrapLayoutFor(contentW).rows
}

// visualRowForPosition returns the soft-wrapped row and cell for a logical
// buffer position.
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

// positionAtVisualRow maps a soft-wrapped row and cell back to a logical
// position, snapping clicks inside wide graphemes and tabs to the cluster start.
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

// clampWrapScroll keeps the soft-wrap viewport inside the visual row layout.
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
