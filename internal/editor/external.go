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
	externalMode    = "external"
	externalMessage = "编辑器只支持文本及图片文件在线编辑."
)

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

// RenderExternal fills the editor surface and centers the unsupported-file
// message using terminal-cell widths so Chinese text remains aligned.
func (t *Tab) RenderExternal(scr tcell.Screen, th theme.Theme, x, y, w, h int) {
	style := tcell.StyleDefault.Background(th.BG).Foreground(th.Muted)
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			scr.SetContent(cx, cy, ' ', nil, style)
		}
	}
	if w > 0 && h > 0 {
		msgWidth := termtext.StringWidth(externalMessage)
		startX := x + (w-msgWidth)/2
		if startX < x {
			startX = x
		}
		termtext.DrawString(scr, startX, y+h/2, w-(startX-x), externalMessage, style)
	}
	scr.HideCursor()
}
