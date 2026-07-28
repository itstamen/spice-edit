// =============================================================================
// File: internal/termtext/termtext_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-28
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package termtext

import (
	"reflect"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// newScreen creates a SimulationScreen suitable for terminal-cell assertions.
func newScreen(t *testing.T, w, h int) tcell.SimulationScreen {
	t.Helper()
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatalf("screen init: %v", err)
	}
	scr.SetSize(w, h)
	return scr
}

// TestClusters_ReportRangesAndWidths keeps rune-indexed callers aligned with
// CJK and combining clusters.
func TestClusters_ReportRangesAndWidths(t *testing.T) {
	got := Clusters("中e\u0301か\u3099")
	want := []Cluster{
		{Text: "中", StartRune: 0, EndRune: 1, Width: 2},
		{Text: "e\u0301", StartRune: 1, EndRune: 3, Width: 1},
		{Text: "か\u3099", StartRune: 3, EndRune: 5, Width: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Clusters() = %#v, want %#v", got, want)
	}
}

// TestDrawString_DoesNotSplitWideCluster proves a CJK character owns two
// cells and does not overwrite the following glyph.
func TestDrawString_DoesNotSplitWideCluster(t *testing.T) {
	scr := newScreen(t, 3, 1)
	defer scr.Fini()

	if used := DrawString(scr, 0, 0, 3, "中a", tcell.StyleDefault); used != 3 {
		t.Fatalf("DrawString used = %d, want 3", used)
	}
	text, _, width := scr.Get(0, 0)
	if text != "中" || width != 2 {
		t.Fatalf("first cell = (%q, %d), want (中, 2)", text, width)
	}
	text, _, width = scr.Get(2, 0)
	if text != "a" || width != 1 {
		t.Fatalf("third cell = (%q, %d), want (a, 1)", text, width)
	}

	scr.Clear()
	if used := DrawString(scr, 0, 0, 1, "中a", tcell.StyleDefault); used != 0 {
		t.Fatalf("narrow DrawString used = %d, want 0", used)
	}
	text, _, _ = scr.Get(0, 0)
	if text != " " {
		t.Fatalf("partial CJK cluster was drawn as %q", text)
	}
}

// TestVisualCoordinates_RespectWholeClusters checks cursor and mouse mapping
// at both combining-rune and wide-cell interior positions.
func TestVisualCoordinates_RespectWholeClusters(t *testing.T) {
	text := "a中e\u0301b"
	for _, tc := range []struct {
		runeIndex int
		want      int
	}{
		{0, 0},
		{1, 1},
		{2, 3},
		{3, 3},
		{4, 4},
		{5, 5},
	} {
		if got := VisualCol(text, tc.runeIndex); got != tc.want {
			t.Errorf("VisualCol(%d) = %d, want %d", tc.runeIndex, got, tc.want)
		}
	}
	if got := RuneIndexAtVisual("中a", 1); got != 0 {
		t.Errorf("RuneIndexAtVisual(second CJK cell) = %d, want 0", got)
	}
	if got := RuneIndexAtOrAfterVisual("中a", 1); got != 1 {
		t.Errorf("RuneIndexAtOrAfterVisual(second CJK cell) = %d, want 1", got)
	}
}

// TestTruncate_KeepsClustersWhole reserves one cell for an ellipsis without
// slicing through a two-cell glyph.
func TestTruncate_KeepsClustersWhole(t *testing.T) {
	if got := Truncate("中文", 1); got != "…" {
		t.Fatalf("Truncate one cell = %q, want ellipsis", got)
	}
	if got := Truncate("a中b", 3); got != "a…" {
		t.Fatalf("Truncate CJK boundary = %q, want a…", got)
	}
	if got := Truncate("e\u0301x", 1); got != "…" {
		t.Fatalf("Truncate combining cluster = %q, want ellipsis", got)
	}
}
