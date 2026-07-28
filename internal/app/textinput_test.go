// =============================================================================
// File: internal/app/textinput_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-28
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestDrawTextInput_CJKScrollsAndKeepsCaretVisible verifies a narrow input
// scrolls in terminal cells rather than splitting a CJK glyph.
func TestDrawTextInput_CJKScrollsAndKeepsCaretVisible(t *testing.T) {
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("screen init: %v", err)
	}
	defer scr.Fini()
	scr.SetSize(12, 1)

	text := []rune("ab中文测试")
	scroll := 0
	drawTextInput(scr, 0, 0, 4, text, len(text), &scroll, tcell.StyleDefault, true)
	if scroll == 0 {
		t.Fatal("expected horizontally scrolled input")
	}
	if _, _, visible := scr.GetCursor(); !visible {
		t.Fatal("expected visible input caret")
	}
}

// TestTextInputCursorAt_SecondCJKCellReturnsClusterStart verifies both cells
// of a double-width glyph map to its first rune.
func TestTextInputCursorAt_SecondCJKCellReturnsClusterStart(t *testing.T) {
	if got := textInputCursorAt([]rune("中a"), 0, 1); got != 0 {
		t.Fatalf("cursor = %d, want 0", got)
	}
}
