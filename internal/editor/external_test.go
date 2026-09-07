// =============================================================================
// File: internal/editor/external_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// TestNewTab_BinaryCreatesExternalTab ensures unsupported bytes never enter
// an editable buffer and every text mutation remains a no-op.
func TestNewTab_BinaryCreatesExternalTab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.bin")
	if err := os.WriteFile(path, []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}

	tab, err := NewTab(path)
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	if !tab.IsExternal() || !tab.IsReadOnly() {
		t.Fatal("expected read-only external tab")
	}
	if got := tab.Buffer.String(); got != "" {
		t.Fatalf("external buffer = %q, want empty", got)
	}

	tab.InsertString("changed")
	tab.InsertRune('x')
	tab.Backspace()
	tab.Delete()
	if got := tab.Buffer.String(); got != "" || tab.Dirty {
		t.Fatalf("external mutation changed tab: buffer=%q dirty=%v", got, tab.Dirty)
	}
	if err := tab.Save(); err == nil {
		t.Fatal("expected external Save error")
	}
	if changed, ok := tab.ToggleLineComment(); changed || ok {
		t.Fatalf("external comment = (%v, %v), want false, false", changed, ok)
	}
}

// TestNewTab_CorruptImageFallsBackToExternal keeps a damaged image from
// falling through to the normal text editor after decode fails.
func TestNewTab_CorruptImageFallsBackToExternal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.png")
	if err := os.WriteFile(path, []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	tab, err := NewTab(path)
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	if !tab.IsExternal() {
		t.Fatal("corrupt image should become external tab")
	}
}

// TestRenderExternal_DisplaysFileAndAction checks the external-file surface
// names the selected file and exposes the explicit desktop-app action.
func TestRenderExternal_DisplaysFileAndAction(t *testing.T) {
	scr := newSimScreen(t, 70, 7)
	defer scr.Fini()

	tab := newExternalTab("/tmp/manual.pdf")
	tab.RenderSoftWrap(scr, theme.Default(), 0, 0, 70, 7, true)
	scr.Show()

	var rows []string
	for y := 0; y < 7; y++ {
		var row strings.Builder
		for x := 0; x < 70; {
			text, _, width := scr.Get(x, y)
			row.WriteString(text)
			if width < 1 {
				width = 1
			}
			x += width
		}
		rows = append(rows, row.String())
	}
	got := strings.Join(rows, "\n")
	for _, want := range []string{"manual.pdf", externalOpenButton} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered screen does not contain %q:\n%s", want, got)
		}
	}
}

// TestExternalOpenHitTest pins the visible action bounds to the centered
// layout used by RenderExternal.
func TestExternalOpenHitTest(t *testing.T) {
	tab := newExternalTab("/tmp/manual.pdf")
	x, y, w, h := 10, 4, 40, 9
	buttonW := len(externalOpenButton)
	buttonX := x + (w-buttonW)/2
	buttonY := y + (h-3)/2 + 2

	for _, px := range []int{buttonX, buttonX + buttonW/2, buttonX + buttonW - 1} {
		if !tab.ExternalOpenHitTest(px, buttonY, x, y, w, h) {
			t.Errorf("button cell (%d,%d) did not hit", px, buttonY)
		}
	}
	for _, point := range [][2]int{
		{buttonX - 1, buttonY},
		{buttonX + buttonW, buttonY},
		{buttonX, buttonY - 2},
		{x - 1, buttonY},
	} {
		if tab.ExternalOpenHitTest(point[0], point[1], x, y, w, h) {
			t.Errorf("non-button cell (%d,%d) hit", point[0], point[1])
		}
	}
}
