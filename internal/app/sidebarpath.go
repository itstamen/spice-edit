// =============================================================================
// File: internal/app/sidebarpath.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-09-03
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/termtext"
)

// sidebarPathType controls whether the selected tree path is project-relative
// or absolute before any optional prefix is stripped.
type sidebarPathType uint8

const (
	sidebarPathRelative sidebarPathType = iota
	sidebarPathAbsolute
	sidebarPathPanelHeight = 8
)

// currentSidebarPath returns the selected path rendered for the panel and
// whether its configured strip prefix matches that rendered value.
func (a *App) currentSidebarPath() (string, bool) {
	if a.tree == nil || a.tree.SelectedPath == "" {
		return "", true
	}
	value := a.relativePathFor(a.tree.SelectedPath)
	if a.sidebarPathType == sidebarPathAbsolute {
		value = absolutePathFor(a.tree.SelectedPath)
	}
	prefix := string(a.sidebarPathPrefix)
	if prefix == "" {
		return value, true
	}
	if !strings.HasPrefix(value, prefix) {
		return value, false
	}
	return strings.TrimPrefix(value, prefix), true
}

// drawSidebarPath paints the compact, unframed path information panel below
// the file tree. It is deliberately read-only: clipboard writes belong to the
// explicit copy interaction, never a render pass.
func (a *App) drawSidebarPath() {
	x, y, w, h := a.sidebarPathRect()
	if w <= 0 || h == 0 {
		return
	}

	bgStyle := tcell.StyleDefault.Background(a.theme.SidebarBG).Foreground(a.theme.Text)
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			a.screen.SetContent(cx, cy, ' ', nil, bgStyle)
		}
	}

	titleStyle := bgStyle.Foreground(a.theme.Muted).Bold(true)
	labelStyle := bgStyle.Foreground(a.theme.Muted)
	valueStyle := bgStyle.Foreground(a.theme.Text)
	selectedStyle := bgStyle.Foreground(a.theme.Accent)

	drawAtWidth(a.screen, x, y, w, " ─ Path", titleStyle)
	drawAtWidth(a.screen, x+1, y+1, w-1, "Path type", labelStyle)
	relative, absolute := "( ) Relative", "( ) Absolute"
	if a.sidebarPathType == sidebarPathAbsolute {
		absolute = "(x) Absolute"
	} else {
		relative = "(x) Relative"
	}
	drawAtWidth(a.screen, x+1, y+2, w-1, relative, selectedStyle)
	drawAtWidth(a.screen, x+1+termtext.StringWidth(relative)+2, y+2,
		w-1-termtext.StringWidth(relative)-2, absolute, valueStyle)
	drawAtWidth(a.screen, x+1, y+3, w-1, "Strip prefix", labelStyle)

	value, prefixValid := a.currentSidebarPath()
	inputStyle := valueStyle
	if !prefixValid {
		inputStyle = inputStyle.Foreground(a.theme.Error)
	}
	inputX, inputW := x+1, w-2
	if inputW > 0 {
		for cx := inputX; cx < inputX+inputW; cx++ {
			a.screen.SetContent(cx, y+4, ' ', nil, inputStyle)
		}
		drawTextInput(a.screen, inputX, y+4, inputW, a.sidebarPathPrefix,
			a.sidebarPathPrefixCursor, &a.sidebarPathPrefixScroll, inputStyle,
			a.sidebarPathPrefixFocused)
	}

	drawAtWidth(a.screen, x+1, y+5, w-1, "Current path", labelStyle)
	drawSidebarPathTail(a.screen, x+1, y+6, w-2, value, valueStyle)
}

// drawSidebarPathTail writes the last two terminal rows of value. It walks
// grapheme clusters so wide and combining characters cannot spill outside the
// panel or split across a row boundary.
func drawSidebarPathTail(scr tcell.Screen, x, y, width int, value string, style tcell.Style) {
	if width <= 0 || value == "" {
		return
	}
	clusters := termtext.Clusters(value)
	start, used := len(clusters), 0
	for start > 0 {
		clusterWidth := clusters[start-1].Width
		if clusterWidth > 0 && used+clusterWidth > 2*width {
			break
		}
		used += clusterWidth
		start--
	}

	row, col := 0, 0
	for _, cluster := range clusters[start:] {
		if cluster.Width > 0 && col+cluster.Width > width {
			row++
			col = 0
		}
		if row >= 2 {
			return
		}
		clusterUsed, drawn := termtext.DrawCluster(scr, x+col, y+row, width-col, cluster, style)
		if drawn {
			col += clusterUsed
		}
	}
}
