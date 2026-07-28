// =============================================================================
// File: internal/app/textinput.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-28
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/termtext"
)

// drawTextInput renders one rune-indexed, single-line text input. It keeps
// scroll at a grapheme boundary, draws complete clusters only, and positions
// the terminal caret by terminal cells rather than by rune count.
func drawTextInput(scr tcell.Screen, x, y, width int, text []rune, cursor int, scroll *int, style tcell.Style, showCursor bool) {
	if scroll == nil || width <= 0 {
		return
	}
	value := string(text)
	*scroll = textInputScroll(value, cursor, *scroll, width)
	scrollVisual := termtext.VisualCol(value, *scroll)
	for _, cluster := range termtext.Clusters(value) {
		if cluster.EndRune <= *scroll {
			continue
		}
		cell := termtext.VisualCol(value, cluster.StartRune) - scrollVisual
		if cell < 0 {
			continue
		}
		if cell+cluster.Width > width {
			break
		}
		termtext.DrawCluster(scr, x+cell, y, width-cell, cluster, style)
	}
	if !showCursor {
		return
	}
	caret := termtext.VisualCol(value, clampTextInputIndex(cursor, len(text))) - scrollVisual
	if caret >= 0 && caret < width {
		scr.ShowCursor(x+caret, y)
	}
}

// textInputCursorAt maps a click inside a visible text input back to its
// rune-indexed cursor position. A click in the second cell of a CJK glyph
// returns that glyph's starting rune, never a phantom position.
func textInputCursorAt(text []rune, scroll, localCell int) int {
	value := string(text)
	if localCell < 0 {
		localCell = 0
	}
	scroll = normalizeTextInputScroll(value, scroll)
	targetVisual := termtext.VisualCol(value, scroll) + localCell
	return termtext.RuneIndexAtVisual(value, targetVisual)
}

// textInputScroll chooses a grapheme boundary that keeps cursor visible in
// width cells. Scroll stays rune-indexed because text editing and undo use
// rune positions throughout the app.
func textInputScroll(value string, cursor, scroll, width int) int {
	if width <= 0 {
		return 0
	}
	cursor = clampTextInputIndex(cursor, len([]rune(value)))
	scroll = normalizeTextInputScroll(value, scroll)
	cursorVisual := termtext.VisualCol(value, cursor)
	scrollVisual := termtext.VisualCol(value, scroll)
	if cursorVisual < scrollVisual {
		return cursorStartBoundary(value, cursor)
	}
	if cursorVisual >= scrollVisual+width {
		targetVisual := cursorVisual - width + 1
		return normalizeTextInputScroll(value, termtext.RuneIndexAtOrAfterVisual(value, targetVisual))
	}
	return scroll
}

// normalizeTextInputScroll advances an interior rune index to its cluster
// end. A viewport may therefore omit a glyph but can never begin midway
// through its visual cells.
func normalizeTextInputScroll(value string, runeIndex int) int {
	runeCount := len([]rune(value))
	if runeIndex <= 0 {
		return 0
	}
	if runeIndex >= runeCount {
		return runeCount
	}
	for _, cluster := range termtext.Clusters(value) {
		if runeIndex > cluster.StartRune && runeIndex < cluster.EndRune {
			return cluster.EndRune
		}
	}
	return runeIndex
}

// cursorStartBoundary keeps a cursor inside a grapheme visually attached to
// that glyph's first cell while preserving the stored rune index elsewhere.
func cursorStartBoundary(value string, runeIndex int) int {
	for _, cluster := range termtext.Clusters(value) {
		if runeIndex > cluster.StartRune && runeIndex < cluster.EndRune {
			return cluster.StartRune
		}
	}
	return clampTextInputIndex(runeIndex, len([]rune(value)))
}

// clampTextInputIndex bounds a rune index for input render and hit-test code.
func clampTextInputIndex(index, runeCount int) int {
	if index < 0 {
		return 0
	}
	if index > runeCount {
		return runeCount
	}
	return index
}
