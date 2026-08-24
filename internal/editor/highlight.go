// =============================================================================
// File: internal/editor/highlight.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// Highlight tokenises src using a Chroma lexer chosen by filename (falling
// back to content-based detection, then to a plain-text lexer) and returns a
// per-line slice of styles parallel to the buffer's lines: styles[i][j] is
// the style for rune j on line i.
//
// Returning a per-rune style grid keeps the renderer simple — it just looks
// up the style for each cell it draws — at the cost of some memory.
// For files small enough to comfortably review, that's a fine trade.
func Highlight(filename, src string, t theme.Theme) [][]tcell.Style {
	return highlightSource(filename, src, t)
}

// highlightLeadLines is how many rows above and below the viewport we
// tokenise but discard, so the lexer can re-enter state it couldn't see
// otherwise, like a multi-line comment or quoted string that spans the
// viewport. Without this lead, scrolling into the middle of a long comment
// colours the body as plain code because the lexer starts fresh at the
// viewport. The lead runs both above and below: chroma needs both
// delimiters of a multi-line block in range to colour the body, so a
// comment that opens above and closes below the viewport needs the opener
// reached by the upper lead and the closer reached by the lower lead.
// Bounded so keystroke cost still follows terminal height, not file size:
// a block longer than the combined lead is the rare case that still
// mis-colours.
const highlightLeadLines = 256

// highlightVisibleWindow returns only the requested style rows and their
// absolute starting line. Tokenisation includes a bounded lead around the
// requested range so multi-line lexer state can be reconstructed.
func highlightVisibleWindow(filename string, lines []string, startLine, height int, t theme.Theme) (int, [][]tcell.Style) {
	if height <= 0 || len(lines) == 0 || startLine >= len(lines) {
		return 0, nil
	}
	if startLine < 0 {
		startLine = 0
	}
	endLine := startLine + height
	if endLine > len(lines) {
		endLine = len(lines)
	}
	// Tokenise a bounded lead above and below the viewport. The upper lead
	// lets the lexer re-enter comment / string state opened earlier; the
	// lower lead closes blocks that end past the viewport. We keep only the
	// visible rows; the lead rows are styled then dropped.
	leadStart := startLine - highlightLeadLines
	if leadStart < 0 {
		leadStart = 0
	}
	leadEnd := endLine + highlightLeadLines
	if leadEnd > len(lines) {
		leadEnd = len(lines)
	}
	src := strings.Join(lines[leadStart:leadEnd], "\n")
	leadStyles := highlightSource(filename, src, t)
	rows := make([][]tcell.Style, endLine-startLine)
	for i := startLine; i < endLine; i++ {
		idx := i - leadStart
		if idx >= len(leadStyles) {
			break
		}
		rows[i-startLine] = leadStyles[idx]
	}
	return startLine, rows
}

// HighlightVisible is the absolute-grid compatibility wrapper used by callers
// outside Tab rendering. The editor itself caches relative windows instead.
func HighlightVisible(filename string, lines []string, startLine, height int, t theme.Theme) [][]tcell.Style {
	styles := make([][]tcell.Style, len(lines))
	start, rows := highlightVisibleWindow(filename, lines, startLine, height, t)
	if start < 0 || start >= len(styles) {
		return styles
	}
	copy(styles[start:], rows)
	return styles
}

// highlightSource tokenises src and returns one style row per source line.
func highlightSource(filename, src string, t theme.Theme) [][]tcell.Style {
	base := tcell.StyleDefault.Background(t.BG).Foreground(t.Text)

	// Pre-allocate a styles grid sized to the source. We seed every cell
	// with the base style so untokenised runes still render readably.
	lines := strings.Split(src, "\n")
	styles := baseStyleGrid(lines, base)

	if isMarkdownFilename(filename) {
		applyMarkdownStructuralStyles(filename, lines, styles, t, base)
		return styles
	}

	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Analyse(src)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}

	applyLexerOverlay(lexer, src, 0, styles, t, base)
	return styles
}

// applyLexerOverlay tokenises src with lexer and overlays the resulting token
// styles onto styles, beginning at lineOffset.
func applyLexerOverlay(lexer chroma.Lexer, src string, lineOffset int, styles [][]tcell.Style, t theme.Theme, base tcell.Style) {
	// Coalesce merges adjacent same-type tokens; cheaper to scan in render.
	lexer = chroma.Coalesce(lexer)
	iter, err := lexer.Tokenise(nil, src)
	if err != nil {
		return
	}

	line, col := lineOffset, 0
	for tok := iter(); tok != chroma.EOF; tok = iter() {
		st := styleForToken(tok.Type, t, base)
		for _, r := range tok.Value {
			if r == '\n' {
				line++
				col = 0
				continue
			}
			if line >= 0 && line < len(styles) && col >= 0 && col < len(styles[line]) {
				styles[line][col] = st
			}
			col++
		}
	}
}

// baseStyleGrid returns a correctly shaped grid pre-filled with base.
func baseStyleGrid(lines []string, base tcell.Style) [][]tcell.Style {
	styles := make([][]tcell.Style, len(lines))
	for i, ln := range lines {
		runes := []rune(ln)
		row := make([]tcell.Style, len(runes))
		for j := range row {
			row[j] = base
		}
		styles[i] = row
	}
	return styles
}

// styleForToken maps a Chroma token type to a tcell.Style using the active
// theme. Literal strings and numbers use their subcategories; the remaining
// mappings use categories to cover language-specific token subtypes.
func styleForToken(tt chroma.TokenType, t theme.Theme, base tcell.Style) tcell.Style {
	switch {
	case tt.InSubCategory(chroma.LiteralString):
		return base.Foreground(t.SynString)
	case tt.InSubCategory(chroma.LiteralNumber):
		return base.Foreground(t.SynNumber)
	case tt.InCategory(chroma.Keyword):
		return base.Foreground(t.SynKeyword)
	case tt.InCategory(chroma.Comment):
		return base.Foreground(t.SynComment).Italic(true)
	case tt.InCategory(chroma.Operator):
		return base.Foreground(t.SynOperator)
	case tt.InCategory(chroma.Punctuation):
		return base.Foreground(t.SynPunct)
	case tt.InCategory(chroma.Literal):
		return base.Foreground(t.SynConstant)
	case tt.InCategory(chroma.Name):
		switch tt {
		case chroma.NameFunction, chroma.NameFunctionMagic:
			return base.Foreground(t.SynFunction)
		case chroma.NameClass, chroma.NameNamespace:
			return base.Foreground(t.SynType)
		case chroma.NameBuiltin, chroma.NameBuiltinPseudo:
			return base.Foreground(t.SynBuiltin)
		case chroma.NameConstant:
			return base.Foreground(t.SynConstant)
		case chroma.NameVariable, chroma.NameVariableInstance,
			chroma.NameVariableClass, chroma.NameVariableGlobal,
			chroma.NameVariableAnonymous:
			return base.Foreground(t.SynVariable)
		case chroma.NameTag:
			return base.Foreground(t.SynType)
		case chroma.NameAttribute:
			return base.Foreground(t.SynVariable)
		}
		return base
	}
	return base
}

// applyMarkdownStructuralStyles overlays Markdown document cues and fenced-code
// syntax colors on top of Chroma's token grid.
func applyMarkdownStructuralStyles(filename string, lines []string, styles [][]tcell.Style, t theme.Theme, base tcell.Style) {
	if !isMarkdownFilename(filename) {
		return
	}
	var activeFence *markdownFenceDescriptor
	bodyStart := 0
	flushFenceBody := func(end int) {
		if activeFence == nil || bodyStart >= end {
			return
		}

		codeBase := base.Foreground(t.CodeText)
		for i := bodyStart; i < end && i < len(styles); i++ {
			styleRow(styles[i], codeBase)
		}
		if activeFence.language == "" {
			return
		}
		lexer := lexers.Get(activeFence.language)
		if lexer == nil {
			return
		}
		applyLexerOverlay(lexer, strings.Join(lines[bodyStart:end], "\n"), bodyStart, styles, t, codeBase)
	}

	for lineIdx, line := range lines {
		if lineIdx >= len(styles) {
			break
		}
		fenceStart, fence, markerEnd, labelStart, labelEnd, isFence := markdownFence(line)
		if activeFence != nil {
			if isFence && fence.marker == activeFence.marker && fence.length >= activeFence.length {
				flushFenceBody(lineIdx)
				styleRuneRange(styles[lineIdx], fenceStart, markerEnd, base.Foreground(t.Muted))
				activeFence = nil
			}
			continue
		}
		if level, ok := markdownHeadingLevel(line); ok {
			styleRow(styles[lineIdx], base.Foreground(markdownHeadingColor(level, t)).Bold(true))
			continue
		}
		if isFence {
			fenceStyle := base.Foreground(t.Muted)
			labelStyle := base.Foreground(t.Accent).Bold(true)
			styleRuneRange(styles[lineIdx], fenceStart, markerEnd, fenceStyle)
			if labelStart < labelEnd {
				styleRuneRange(styles[lineIdx], labelStart, labelEnd, labelStyle)
			}
			fenceCopy := fence
			activeFence = &fenceCopy
			bodyStart = lineIdx + 1
		}
	}
	flushFenceBody(len(lines))
}

// isMarkdownFilename reports whether filename should receive Markdown-specific
// structural styling.
func isMarkdownFilename(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".md" || ext == ".markdown" {
		return true
	}
	base := strings.ToLower(filepath.Base(filename))
	return base == "readme.md" || base == "license.md"
}

// markdownHeadingLevel detects ATX headings from # through ######.
func markdownHeadingLevel(line string) (int, bool) {
	runes := []rune(line)
	i := 0
	for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
		i++
	}
	level := 0
	for i+level < len(runes) && runes[i+level] == '#' && level < 6 {
		level++
	}
	if level == 0 {
		return 0, false
	}
	next := i + level
	if next < len(runes) && runes[next] == '#' {
		return 0, false
	}
	if next < len(runes) && runes[next] != ' ' && runes[next] != '\t' {
		return 0, false
	}
	return level, true
}

// markdownHeadingColor maps Markdown heading depth to theme colors.
func markdownHeadingColor(level int, t theme.Theme) tcell.Color {
	switch level {
	case 1:
		return t.Accent
	case 2:
		return t.AccentSoft
	case 3:
		return t.SynFunction
	case 4:
		return t.SynType
	case 5:
		return t.SynString
	default:
		return t.SynComment
	}
}

// markdownFenceDescriptor identifies an opening Markdown code fence.
type markdownFenceDescriptor struct {
	marker   rune
	length   int
	language string
}

// markdownFence detects a fenced-code delimiter and returns its descriptor
// plus rune ranges for the fence punctuation and optional language label.
func markdownFence(line string) (fenceStart int, fence markdownFenceDescriptor, markerEnd, labelStart, labelEnd int, ok bool) {
	runes := []rune(line)
	i := 0
	for i < len(runes) && i < 4 && runes[i] == ' ' {
		i++
	}
	if i > 3 || i >= len(runes) {
		return 0, markdownFenceDescriptor{}, 0, 0, 0, false
	}
	ch := runes[i]
	if ch != '`' && ch != '~' {
		return 0, markdownFenceDescriptor{}, 0, 0, 0, false
	}
	j := i
	for j < len(runes) && runes[j] == ch {
		j++
	}
	if j-i < 3 {
		return 0, markdownFenceDescriptor{}, 0, 0, 0, false
	}
	k := j
	for k < len(runes) && (runes[k] == ' ' || runes[k] == '\t') {
		k++
	}
	labelEnd = k
	for labelEnd < len(runes) && runes[labelEnd] != ' ' && runes[labelEnd] != '\t' {
		labelEnd++
	}
	return i, markdownFenceDescriptor{
		marker:   ch,
		length:   j - i,
		language: string(runes[k:labelEnd]),
	}, j, k, labelEnd, true
}

// styleRow applies st to every rune style in row.
func styleRow(row []tcell.Style, st tcell.Style) {
	for i := range row {
		row[i] = st
	}
}

// styleRuneRange applies st to the half-open rune range [start, end).
func styleRuneRange(row []tcell.Style, start, end int, st tcell.Style) {
	if start < 0 {
		start = 0
	}
	if end > len(row) {
		end = len(row)
	}
	for i := start; i < end; i++ {
		row[i] = st
	}
}
