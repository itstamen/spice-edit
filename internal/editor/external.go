// =============================================================================
// File: internal/editor/external.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"os"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/termtext"
	"github.com/cloudmanic/spice-edit/internal/theme"
)

const (
	externalMode       = "external"
	externalOpenButton = "[ Open with default app ]"
)

// externalViewLayout records the centered rows shared by rendering and mouse
// hit testing so the visible button and clickable button cannot drift apart.
type externalViewLayout struct {
	name    string
	nameX   int
	nameY   int
	nameW   int
	buttonX int
	buttonY int
	buttonW int
}

// newExternalTab creates a read-only path placeholder without copying any of
// the unsupported file's bytes into the tab buffer.
func newExternalTab(path string) *Tab {
	var mtime time.Time
	if info, err := os.Stat(path); err == nil {
		mtime = info.ModTime()
	}
	t := &Tab{
		Path:   path,
		Buffer: NewBuffer(""),
		Mtime:  mtime,
		Mode:   externalMode,
	}
	t.initUndo()
	return t
}

// IsExternal reports whether the tab delegates viewing to a desktop app.
func (t *Tab) IsExternal() bool {
	return t != nil && t.Mode == externalMode
}

// IsReadOnly reports whether the tab has no editable text surface.
func (t *Tab) IsReadOnly() bool {
	return t != nil && (t.IsImage() || t.IsExternal())
}

// externalLayout centers the external filename and action inside one editor
// rectangle, clipping each row when the terminal is unusually narrow.
func (t *Tab) externalLayout(x, y, w, h int) (externalViewLayout, bool) {
	if t == nil || !t.IsExternal() || w <= 0 || h <= 0 {
		return externalViewLayout{}, false
	}

	layout := externalViewLayout{name: t.DisplayName()}
	layout.nameW = termtext.StringWidth(layout.name)
	if layout.nameW > w {
		layout.nameW = w
	}
	layout.nameX = x + (w-layout.nameW)/2

	if h == 1 {
		layout.nameY = y
		return layout, true
	}
	gap := 0
	blockH := 2
	if h >= 3 {
		gap = 1
		blockH = 3
	}
	layout.nameY = y + (h-blockH)/2
	layout.buttonY = layout.nameY + 1 + gap
	layout.buttonW = termtext.StringWidth(externalOpenButton)
	if layout.buttonW > w {
		layout.buttonW = w
	}
	layout.buttonX = x + (w-layout.buttonW)/2
	return layout, true
}

// ExternalOpenHitTest reports whether a screen coordinate lands on the
// visible default-application action in this external tab.
func (t *Tab) ExternalOpenHitTest(px, py, x, y, w, h int) bool {
	layout, ok := t.externalLayout(x, y, w, h)
	return ok && layout.buttonW > 0 && py == layout.buttonY &&
		px >= layout.buttonX && px < layout.buttonX+layout.buttonW
}

// RenderExternal fills the editor surface and centers the selected filename
// above the explicit default-application action.
func (t *Tab) RenderExternal(scr tcell.Screen, th theme.Theme, x, y, w, h int) {
	style := tcell.StyleDefault.Background(th.BG).Foreground(th.Muted)
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			scr.SetContent(cx, cy, ' ', nil, style)
		}
	}
	layout, ok := t.externalLayout(x, y, w, h)
	if ok {
		nameStyle := tcell.StyleDefault.Background(th.BG).Foreground(th.Text).Bold(true)
		termtext.DrawString(scr, layout.nameX, layout.nameY, layout.nameW, layout.name, nameStyle)
		if layout.buttonW > 0 {
			buttonStyle := tcell.StyleDefault.Background(th.BG).Foreground(th.Accent).Bold(true)
			termtext.DrawString(scr, layout.buttonX, layout.buttonY, layout.buttonW, externalOpenButton, buttonStyle)
		}
	}
	scr.HideCursor()
}
