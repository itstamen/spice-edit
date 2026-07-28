// =============================================================================
// File: internal/editor/indent.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// indent.go owns two related concerns:
//
//   1. Visual-column math. Tabs render as multi-cell tab stops, so a rune's
//      "screen position" is no longer equal to its index in the line. The
//      renderer, cursor placement, and mouse hit-test all consult these
//      helpers when converting between rune indices and visual cells.
//
//   2. Indent style detection. When a tab is opened, we sample its existing
//      indentation and pick the unit (tab character vs N spaces) the user
//      gets when they press Tab. This avoids the "spaces in a tab-indented
//      file" bug that has historically been the #1 complaint about new
//      editors.

package editor

import (
	"path/filepath"
	"strings"

	"github.com/cloudmanic/spice-edit/internal/termtext"
)

// TabStop is the visual cell width of a hard tab. Four matches what most
// modern editors and viewers default to (cat, less, GitHub) and matches the
// editor's typical insert width when the user presses Tab.
const TabStop = 4

// defaultSpaceIndent is the fallback indent unit when detection has nothing
// to go on (empty file, no leading whitespace anywhere). Four spaces is
// the common convention across the languages the editor most often opens.
const defaultSpaceIndent = "    "

// RuneVisualWidth returns the cell count occupied by r when it lands at the
// given visualCol (0-based, measured from the start of the line). Hard tabs
// expand to fill enough cells to reach the next TabStop boundary. Other runes
// use their terminal width; callers that need full emoji/combining-sequence
// support should use the grapheme-aware line helpers below.
func RuneVisualWidth(r rune, visualCol int) int {
	if r == '\t' {
		w := TabStop - (visualCol % TabStop)
		if w == 0 {
			w = TabStop
		}
		return w
	}
	width := termtext.StringWidth(string(r))
	if width < 0 {
		return 0
	}
	return width
}

// LineVisualCol returns the visual column (0-based) at the rune position
// runeCol within runes. Tabs in the prefix expand to tab stops. Used for
// cursor placement and selection / find highlighting.
//
// runeCol is clamped to [0, len(runes)] so callers can pass an end-of-line
// position without bounds-checking.
func LineVisualCol(runes []rune, runeCol int) int {
	if runeCol < 0 {
		runeCol = 0
	}
	if runeCol > len(runes) {
		runeCol = len(runes)
	}
	visualCol := 0
	for _, cluster := range lineClusters(runes) {
		if runeCol <= cluster.StartRune {
			return visualCol
		}
		if runeCol < cluster.EndRune {
			return visualCol
		}
		visualCol += clusterVisualWidth(cluster, visualCol)
	}
	return visualCol
}

// RuneColAtVisual returns the rune index whose start column is at or just
// before targetVisualCol, snapping clicks inside a tab's visual span back
// to the tab's start. Used by mouse hit-testing — clicking anywhere in a
// 4-cell tab places the cursor at the tab character itself, not somewhere
// "inside" it (which would be a no-op anyway).
//
// When targetVisualCol is past the line's end, the rune count is returned
// (cursor lands at the end-of-line virtual position).
func RuneColAtVisual(runes []rune, targetVisualCol int) int {
	if targetVisualCol <= 0 {
		return 0
	}
	visualCol := 0
	for _, cluster := range lineClusters(runes) {
		width := clusterVisualWidth(cluster, visualCol)
		if width > 0 && targetVisualCol < visualCol+width {
			return cluster.StartRune
		}
		visualCol += width
	}
	return len(runes)
}

// runeColAtOrAfterVisual returns a safe grapheme boundary at or after the
// requested visual column. When horizontal scrolling lands inside a wide
// grapheme, it advances to the cluster's end instead of displaying half a
// glyph at the left edge of the viewport.
func runeColAtOrAfterVisual(runes []rune, targetVisualCol int) int {
	if targetVisualCol <= 0 {
		return 0
	}
	visualCol := 0
	for _, cluster := range lineClusters(runes) {
		if targetVisualCol <= visualCol {
			return cluster.StartRune
		}
		width := clusterVisualWidth(cluster, visualCol)
		if width > 0 && targetVisualCol < visualCol+width {
			return cluster.EndRune
		}
		visualCol += width
	}
	return len(runes)
}

// normalizedRuneCol returns runeCol clamped to the line and advanced out of
// a grapheme interior. Buffer editing intentionally remains rune-indexed, but
// a rendered viewport must never begin halfway through a grapheme.
func normalizedRuneCol(runes []rune, runeCol int) int {
	if runeCol <= 0 {
		return 0
	}
	if runeCol >= len(runes) {
		return len(runes)
	}
	for _, cluster := range lineClusters(runes) {
		if runeCol > cluster.StartRune && runeCol < cluster.EndRune {
			return cluster.EndRune
		}
	}
	return runeCol
}

// lineClusters converts the buffer's rune slice into terminal graphemes. Its
// ranges remain indexed in the original rune slice so cursor and selection
// state do not need a broader storage migration.
func lineClusters(runes []rune) []termtext.Cluster {
	return termtext.Clusters(string(runes))
}

// clusterVisualWidth applies the editor's tab-stop rule to a grapheme and
// otherwise returns the terminal width computed by termtext.
func clusterVisualWidth(cluster termtext.Cluster, visualCol int) int {
	if cluster.Text == "\t" {
		return RuneVisualWidth('\t', visualCol)
	}
	return cluster.Width
}

// DetectIndent picks the indent unit a freshly-opened buffer should use
// when the user presses Tab. The algorithm in priority order:
//
//  1. Walk every line; classify lines that start with whitespace as either
//     tab-indented or space-indented. Count both.
//  2. If tab-indented lines outnumber space-indented ones, return "\t".
//  3. If space-indented wins, return that many spaces (using the smallest
//     non-zero leading-space count as the indent width — that matches what
//     "infer" tools in other editors do).
//  4. With no signal, fall back to the path's extension: ".go" / Makefiles /
//     ".tsv" default to tabs (those file types either require or strongly
//     prefer tabs); everything else defaults to four spaces.
//
// The result is what the *Tab* key inserts; existing characters in the file
// are not rewritten.
func DetectIndent(lines []string, path string) string {
	tabLines := 0
	spaceLines := 0
	smallestSpaceWidth := 0 // 0 = "no space-indented line seen yet"

	for _, line := range lines {
		if line == "" {
			continue
		}
		switch line[0] {
		case '\t':
			tabLines++
		case ' ':
			spaceLines++
			n := leadingSpaces(line)
			if n > 0 && (smallestSpaceWidth == 0 || n < smallestSpaceWidth) {
				smallestSpaceWidth = n
			}
		}
	}

	if tabLines > spaceLines && tabLines > 0 {
		return "\t"
	}
	if spaceLines > 0 && smallestSpaceWidth > 0 {
		return strings.Repeat(" ", smallestSpaceWidth)
	}

	// No signal — pick a sensible default by file extension.
	return defaultIndentForPath(path)
}

// leadingSpaces counts the number of leading ' ' bytes in s, stopping at
// the first non-space byte.
func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

// defaultIndentForPath returns "\t" for languages whose conventions strongly
// prefer or require tabs (Go's gofmt rewrites to tabs; Makefiles literally
// require tabs at the start of recipe lines; TSV is tab-separated by name).
// Everything else defaults to four spaces. This is consulted only when the
// file has no existing indentation to learn from — a real Python source
// with 4-space indents will be detected as "    " regardless of the path.
func defaultIndentForPath(path string) string {
	base := strings.ToLower(filepath.Base(path))
	if base == "makefile" || base == "gnumakefile" {
		return "\t"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".tsv":
		return "\t"
	}
	return defaultSpaceIndent
}
