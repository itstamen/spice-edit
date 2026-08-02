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

// TestRenderExternal_DisplaysExactMessage checks the user-facing placeholder
// with a simulation screen so punctuation and Chinese text stay exact.
func TestRenderExternal_DisplaysExactMessage(t *testing.T) {
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
	if got := strings.Join(rows, "\n"); !strings.Contains(got, externalMessage) {
		t.Fatalf("rendered screen does not contain %q:\n%s", externalMessage, got)
	}
}
