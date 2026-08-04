// =============================================================================
// File: internal/editor/performance_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-04
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

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
		tab.cursorMoved = false
		tab.Render(scr, theme.Default(), 0, 0, 120, 40)
	}
}

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
