// =============================================================================
// File: internal/app/sidebarpath.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-09-03
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/clipboard"
	"github.com/cloudmanic/spice-edit/internal/spiceconfig"
	"github.com/cloudmanic/spice-edit/internal/termtext"
)

// sidebarPathType controls whether the selected tree path is project-relative
// or absolute before any optional prefix is stripped.
type sidebarPathType uint8

const (
	sidebarPathRelative sidebarPathType = iota
	sidebarPathAbsolute
	sidebarPathExpandedHeight  = 8
	sidebarPathCollapsedHeight = 1

	// sidebarPathPanelHeight remains an alias for tests and callers that refer
	// to the original expanded-only panel height.
	sidebarPathPanelHeight = sidebarPathExpandedHeight
)

// normalizeSidebarPathValue makes copied and rendered path values stable
// across platforms without affecting filesystem paths used by editor actions.
func normalizeSidebarPathValue(value string, separator rune) string {
	if separator == '/' {
		return value
	}
	return strings.ReplaceAll(value, string(separator), "/")
}

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
	value = normalizeSidebarPathValue(value, filepath.Separator)
	prefix := string(a.sidebarPathPrefix)
	if prefix == "" {
		return value, true
	}
	if a.sidebarPathType == sidebarPathRelative && prefix == "./" {
		return value, true
	}
	if !strings.HasPrefix(value, prefix) {
		return value, false
	}
	return strings.TrimPrefix(value, prefix), true
}

// currentSidebarFolderPrefix returns the selected node's folder rendered in
// the active path mode. Files contribute their parent; directories contribute
// themselves. A non-empty prefix always ends in a normalized slash.
func (a *App) currentSidebarFolderPrefix() (string, bool) {
	if a.tree == nil || a.tree.SelectedPath == "" {
		return "", false
	}
	target := a.tree.SelectedPath
	info, err := os.Stat(target)
	if err != nil {
		return "", false
	}
	folder := target
	if !info.IsDir() {
		folder = filepath.Dir(target)
	}

	value := a.relativePathFor(folder)
	if a.sidebarPathType == sidebarPathAbsolute {
		value = absolutePathFor(folder)
	}
	value = normalizeSidebarPathValue(value, filepath.Separator)
	if value == "." {
		return "./", true
	}
	return strings.TrimSuffix(value, "/") + "/", true
}

// toggleCurrentSidebarPrefix switches between no prefix and the selected
// node's folder, then republishes the newly displayed path to the clipboard.
func (a *App) toggleCurrentSidebarPrefix() {
	if len(a.sidebarPathPrefix) > 0 {
		a.sidebarPathPrefix = nil
	} else {
		prefix, ok := a.currentSidebarFolderPrefix()
		if !ok {
			a.flash("No path selected")
			return
		}
		a.sidebarPathPrefix = []rune(prefix)
	}
	a.sidebarPathPrefixCursor = len(a.sidebarPathPrefix)
	a.sidebarPathPrefixScroll = 0
	a.sidebarPathPrefixFocused = false
	a.copyCurrentSidebarPath(false)
}

// copyCurrentSidebarPath publishes the tree selection's displayed path after
// an explicit sidebar event. It intentionally leaves the editor clipboard and
// status success message alone so path browsing does not disturb text editing.
func (a *App) copyCurrentSidebarPath(force bool) {
	value, _ := a.currentSidebarPath()
	clipboardValue := value
	if a.sidebarPathQuoted {
		clipboardValue = strconv.Quote(value)
	}
	if a.tree == nil || a.tree.SelectedPath == "" ||
		(!force && a.sidebarPathHasCopied && clipboardValue == a.sidebarPathLastCopied) {
		return
	}
	copyToSystem := a.copyToSystem
	if copyToSystem == nil {
		copyToSystem = clipboard.CopyToSystem
	}
	if err := copyToSystem(clipboardValue); err != nil {
		a.flash(fmt.Sprintf("Copy failed: %v", err))
		return
	}
	a.sidebarPathLastCopied = clipboardValue
	a.sidebarPathHasCopied = true
}

// sidebarPathInputRect returns the rendered prefix field's clickable cells.
func (a *App) sidebarPathInputRect() (x, y, w int) {
	x, y, panelW, panelH := a.sidebarPathRect()
	if panelW <= 0 || panelH < sidebarPathExpandedHeight {
		return 0, 0, 0
	}
	relative, quoted := sidebarPathOptionLabels(a.sidebarPathType == sidebarPathRelative, a.sidebarPathQuoted)
	optionWidth := termtext.StringWidth(relative) + 2 + termtext.StringWidth(quoted)
	if optionWidth <= panelW-1 {
		return x + 1, y + 4, panelW - 2
	}
	prefixLabel := "Strip prefix"
	inputX := x + termtext.StringWidth(prefixLabel) + 1
	return inputX, y + 4, panelW - termtext.StringWidth(prefixLabel) - 1
}

// pointInRect reports whether the screen point lies in the half-open rect.
func pointInRect(px, py, x, y, w, h int) bool {
	return w > 0 && h > 0 && px >= x && px < x+w && py >= y && py < y+h
}

// sidebarPathContains reports whether a point belongs to the path panel.
func (a *App) sidebarPathContains(x, y int) bool {
	px, py, pw, ph := a.sidebarPathRect()
	return pointInRect(x, y, px, py, pw, ph)
}

// handleSidebarPathMouse consumes left-clicks in the path panel. It maps the
// prefix field through terminal cells so wide runes retain a valid cursor.
func (a *App) handleSidebarPathMouse(x, y int) bool {
	panelX, panelY, panelW, panelH := a.sidebarPathRect()
	if !pointInRect(x, y, panelX, panelY, panelW, panelH) {
		return false
	}
	if y == panelY {
		a.sidebarPathCollapsed = !a.sidebarPathCollapsed
		a.sidebarPathPrefixFocused = false
		a.sidebarPathPrefixScroll = 0
		if err := spiceconfig.SetSidebarPathCollapsed(a.spiceConfigPath, a.sidebarPathCollapsed); err != nil {
			a.flash("config: " + err.Error())
		}
		return true
	}

	relative, quoted := sidebarPathOptionLabels(a.sidebarPathType == sidebarPathRelative, a.sidebarPathQuoted)
	optionWidth := termtext.StringWidth(relative) + 2 + termtext.StringWidth(quoted)
	changed := false
	if optionWidth <= panelW-1 && y == panelY+2 {
		relativeX := panelX + 1
		quotedX := relativeX + termtext.StringWidth(relative) + 2
		switch {
		case x >= relativeX && x < relativeX+termtext.StringWidth(relative):
			a.sidebarPathType = toggleSidebarPathType(a.sidebarPathType)
			changed = true
		case x >= quotedX && x < quotedX+termtext.StringWidth(quoted):
			a.sidebarPathQuoted = !a.sidebarPathQuoted
			changed = true
		}
	} else if optionWidth > panelW-1 {
		switch y {
		case panelY + 2:
			a.sidebarPathType = toggleSidebarPathType(a.sidebarPathType)
			changed = true
		case panelY + 3:
			a.sidebarPathQuoted = !a.sidebarPathQuoted
			changed = true
		}
	}
	if changed {
		a.sidebarPathPrefixFocused = false
		a.copyCurrentSidebarPath(false)
		return true
	}

	inputX, inputY, inputW := a.sidebarPathInputRect()
	if pointInRect(x, y, inputX, inputY, inputW, 1) {
		a.sidebarPathPrefixFocused = true
		a.sidebarPathPrefixCursor = textInputCursorAt(a.sidebarPathPrefix, a.sidebarPathPrefixScroll, x-inputX)
		return true
	}

	a.sidebarPathPrefixFocused = false
	return true
}

// handleSidebarPathKey edits the focused prefix field and consumes every key
// so sidebar input can never leak into the active editor buffer.
func (a *App) handleSidebarPathKey(ev *tcell.EventKey) {
	if !a.sidebarPathPrefixFocused {
		return
	}
	if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyTab {
		a.sidebarPathPrefixFocused = false
		if ev.Key() == tcell.KeyEsc {
			a.lastEscape = time.Time{}
		}
		return
	}

	cursor := clampTextInputIndex(a.sidebarPathPrefixCursor, len(a.sidebarPathPrefix))
	changed := false
	switch ev.Key() {
	case tcell.KeyRune:
		if ev.Rune() < 0x20 {
			break
		}
		a.sidebarPathPrefix = append(a.sidebarPathPrefix, 0)
		copy(a.sidebarPathPrefix[cursor+1:], a.sidebarPathPrefix[cursor:])
		a.sidebarPathPrefix[cursor] = ev.Rune()
		cursor++
		changed = true
	case tcell.KeyLeft:
		if cursor > 0 {
			cursor--
		}
	case tcell.KeyRight:
		if cursor < len(a.sidebarPathPrefix) {
			cursor++
		}
	case tcell.KeyHome:
		cursor = 0
	case tcell.KeyEnd:
		cursor = len(a.sidebarPathPrefix)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if cursor > 0 {
			a.sidebarPathPrefix = append(a.sidebarPathPrefix[:cursor-1], a.sidebarPathPrefix[cursor:]...)
			cursor--
			changed = true
		}
	case tcell.KeyDelete:
		if cursor < len(a.sidebarPathPrefix) {
			a.sidebarPathPrefix = append(a.sidebarPathPrefix[:cursor], a.sidebarPathPrefix[cursor+1:]...)
			changed = true
		}
	}
	a.sidebarPathPrefixCursor = cursor
	if changed {
		a.copyCurrentSidebarPath(false)
	}
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

	toggle := "▲"
	if a.sidebarPathCollapsed {
		toggle = "▼"
	}
	drawAtWidth(a.screen, x, y, w, " ─ PATH INFO "+toggle, titleStyle)
	if a.sidebarPathCollapsed {
		return
	}
	drawAtWidth(a.screen, x+1, y+1, w-1, "Path setting", labelStyle)
	relative, quoted := sidebarPathOptionLabels(a.sidebarPathType == sidebarPathRelative, a.sidebarPathQuoted)
	relativeStyle, quotedStyle := valueStyle, valueStyle
	if a.sidebarPathType == sidebarPathRelative {
		relativeStyle = selectedStyle
	}
	if a.sidebarPathQuoted {
		quotedStyle = selectedStyle
	}

	value, prefixValid := a.currentSidebarPath()
	inputStyle := valueStyle
	if !prefixValid {
		inputStyle = inputStyle.Foreground(a.theme.Error)
	}
	drawInput := func(inputX, inputY, inputW int) {
		if inputW <= 0 {
			return
		}
		for cx := inputX; cx < inputX+inputW; cx++ {
			a.screen.SetContent(cx, inputY, ' ', nil, inputStyle)
		}
		drawTextInput(a.screen, inputX, inputY, inputW, a.sidebarPathPrefix,
			a.sidebarPathPrefixCursor, &a.sidebarPathPrefixScroll, inputStyle,
			a.sidebarPathPrefixFocused)
	}

	optionWidth := termtext.StringWidth(relative) + 2 + termtext.StringWidth(quoted)
	if optionWidth <= w-1 {
		drawAtWidth(a.screen, x+1, y+2, w-1, relative, relativeStyle)
		drawAtWidth(a.screen, x+1+termtext.StringWidth(relative)+2, y+2,
			w-1-termtext.StringWidth(relative)-2, quoted, quotedStyle)
		drawAtWidth(a.screen, x+1, y+3, w-1, "Strip prefix", labelStyle)
		drawInput(x+1, y+4, w-2)
	} else {
		// The narrowest supported sidebar has only 16 inner cells. Keep both
		// path choices whole by stacking them, then share the prefix label row
		// with its input so the panel remains exactly eight rows tall.
		drawAtWidth(a.screen, x+1, y+2, w-1, relative, relativeStyle)
		drawAtWidth(a.screen, x+1, y+3, w-1, quoted, quotedStyle)
		prefixLabel := "Strip prefix"
		drawAtWidth(a.screen, x, y+4, w, prefixLabel, labelStyle)
		drawInput(x+termtext.StringWidth(prefixLabel)+1, y+4, w-termtext.StringWidth(prefixLabel)-1)
	}

	drawAtWidth(a.screen, x+1, y+5, w-1, "Current path", labelStyle)
	drawSidebarPathTail(a.screen, x+1, y+6, w-2, value, valueStyle)
}

// sidebarPathOptionLabels returns the exact checkbox copy used by both
// rendering and mouse hit-testing so their terminal-cell geometry cannot drift.
func sidebarPathOptionLabels(relative, quoted bool) (string, string) {
	relativeMark := "☐"
	if relative {
		relativeMark = "☑"
	}
	quotedMark := "☐"
	if quoted {
		quotedMark = "☑"
	}
	return relativeMark + " Relative", quotedMark + " quoted"
}

// toggleSidebarPathType maps the single Relative checkbox to the two path
// modes: checked means project-relative and unchecked means absolute.
func toggleSidebarPathType(current sidebarPathType) sidebarPathType {
	if current == sidebarPathRelative {
		return sidebarPathAbsolute
	}
	return sidebarPathRelative
}

// drawSidebarPathTail writes the last two terminal rows of value. It walks
// grapheme clusters so wide and combining characters cannot spill outside the
// panel or split across a row boundary.
func drawSidebarPathTail(scr tcell.Screen, x, y, width int, value string, style tcell.Style) {
	if width <= 0 || value == "" {
		return
	}
	clusters := termtext.Clusters(value)
	start := len(clusters)
	// Fill the final row backward first, then the preceding row. This mirrors
	// the forward greedy line packing while accounting for unused cells at the
	// end of a row (for example, double-width glyphs in an odd-width panel).
	for row := 0; row < 2; row++ {
		used := 0
		for start > 0 {
			clusterWidth := clusters[start-1].Width
			if clusterWidth > width || (clusterWidth > 0 && used+clusterWidth > width) {
				break
			}
			used += clusterWidth
			start--
		}
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
