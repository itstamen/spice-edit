// =============================================================================
// File: internal/termtext/termtext.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-28
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package termtext provides terminal-cell-aware text primitives shared by
// SpiceEdit's editor and application UI. Buffer state remains rune-indexed;
// this package converts those indexes to grapheme and terminal-cell geometry
// only at render and hit-test boundaries.
package termtext

import (
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

// Cluster is one extended grapheme cluster and its range within a UTF-8
// string measured in runes. EndRune is exclusive. Width is the cluster's
// terminal-cell width, as reported by uniseg.
type Cluster struct {
	Text               string
	StartRune, EndRune int
	Width              int
}

// Clusters splits text into extended grapheme clusters. Keeping rune ranges
// lets callers retain their existing rune-indexed cursor, selection, and
// search-match state while rendering one terminal glyph at a time.
func Clusters(text string) []Cluster {
	clusters := make([]Cluster, 0)
	state := -1
	start := 0
	for len(text) > 0 {
		part, rest, width, nextState := uniseg.FirstGraphemeClusterInString(text, state)
		if part == "" {
			break
		}
		end := start + utf8.RuneCountInString(part)
		clusters = append(clusters, Cluster{
			Text:      part,
			StartRune: start,
			EndRune:   end,
			Width:     width,
		})
		text = rest
		state = nextState
		start = end
	}
	return clusters
}

// StringWidth returns the number of terminal cells text occupies. It never
// counts bytes or runes as cells, so CJK, emoji, and combining sequences keep
// their display width.
func StringWidth(text string) int {
	width := 0
	for _, cluster := range Clusters(text) {
		width += cluster.Width
	}
	return width
}

// VisualCol returns the visual cell at runeIndex. An index inside a combining
// sequence resolves to its containing cluster's starting cell. Indexes below
// zero and beyond the string are clamped to the string's visual bounds.
func VisualCol(text string, runeIndex int) int {
	if runeIndex <= 0 {
		return 0
	}
	visual := 0
	for _, cluster := range Clusters(text) {
		if runeIndex <= cluster.StartRune {
			return visual
		}
		if runeIndex < cluster.EndRune {
			return visual
		}
		visual += cluster.Width
	}
	return visual
}

// RuneIndexAtVisual returns the rune index at the start of the grapheme that
// occupies visualCol. A click in either cell of a wide grapheme therefore
// maps to one safe rune position. Values beyond the line return its rune end.
func RuneIndexAtVisual(text string, visualCol int) int {
	if visualCol <= 0 {
		return 0
	}
	visual := 0
	end := 0
	for _, cluster := range Clusters(text) {
		end = cluster.EndRune
		if cluster.Width > 0 && visualCol < visual+cluster.Width {
			return cluster.StartRune
		}
		visual += cluster.Width
	}
	return end
}

// RuneIndexAtOrAfterVisual returns a grapheme boundary that begins at or
// after visualCol. When visualCol lands inside a wide grapheme, it returns
// the following boundary so a clipped viewport never starts halfway through
// that glyph.
func RuneIndexAtOrAfterVisual(text string, visualCol int) int {
	if visualCol <= 0 {
		return 0
	}
	visual := 0
	end := 0
	for _, cluster := range Clusters(text) {
		if visualCol <= visual {
			return cluster.StartRune
		}
		end = cluster.EndRune
		if cluster.Width > 0 && visualCol < visual+cluster.Width {
			return cluster.EndRune
		}
		visual += cluster.Width
	}
	return end
}

// DrawCluster renders c at x,y only when the entire cluster fits in
// remaining cells. It returns the number of cells tcell used and whether the
// cluster was drawn. Drawing through Screen.Put preserves combining marks and
// wide-cell ownership in tcell's backing buffer.
func DrawCluster(scr tcell.Screen, x, y, remaining int, c Cluster, style tcell.Style) (used int, drawn bool) {
	if remaining < c.Width {
		return 0, false
	}
	_, used = scr.Put(x, y, c.Text, style)
	return used, true
}

// DrawString renders whole grapheme clusters until the available cell width
// is full. A wide glyph that would straddle the right edge is omitted instead
// of overwriting the next UI surface or being partially drawn.
func DrawString(scr tcell.Screen, x, y, remaining int, text string, style tcell.Style) int {
	if remaining <= 0 {
		return 0
	}
	used := 0
	for _, cluster := range Clusters(text) {
		clusterUsed, drawn := DrawCluster(scr, x+used, y, remaining-used, cluster, style)
		if !drawn {
			break
		}
		used += clusterUsed
	}
	return used
}

// Truncate returns a whole-grapheme prefix of text that fits maxCells. When
// truncation is necessary, the final available cell is an ellipsis. A
// two-cell grapheme is never sliced to make it fit.
func Truncate(text string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	if StringWidth(text) <= maxCells {
		return text
	}
	if maxCells == 1 {
		return "…"
	}
	limit := maxCells - 1
	used := 0
	end := 0
	for _, cluster := range Clusters(text) {
		if used+cluster.Width > limit {
			break
		}
		used += cluster.Width
		end = cluster.EndRune
	}
	return string([]rune(text)[:end]) + "…"
}
