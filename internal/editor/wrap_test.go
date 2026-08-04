// =============================================================================
// File: internal/editor/wrap_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-04
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"os"
	"path/filepath"
	"testing"
)

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
