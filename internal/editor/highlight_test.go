// =============================================================================
// File: internal/editor/highlight_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the syntax-highlight grid generator. We don't pin specific
// chroma token assignments (those are an upstream concern), only the shape
// invariants the renderer relies on: one row per source line, each row long
// enough to cover its line's runes, and a graceful fallback for unknown or
// missing lexers.

package editor

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/theme"
)

// TestHighlight_GoSourceShapesGrid verifies that highlighting a snippet of
// Go produces a grid whose row count and per-row length match the source
// lines and rune counts — that's what the renderer indexes into.
func TestHighlight_GoSourceShapesGrid(t *testing.T) {
	src := "package main\n\nfunc main() {}\n"
	th := theme.Default()

	got := Highlight("main.go", src, th)
	lines := strings.Split(src, "\n")
	if len(got) != len(lines) {
		t.Fatalf("rows = %d, want %d", len(got), len(lines))
	}
	for i, ln := range lines {
		if len(got[i]) != len([]rune(ln)) {
			t.Errorf("row %d len = %d, want %d", i, len(got[i]), len([]rune(ln)))
		}
	}
}

// TestHighlight_GoKeywordIsColored confirms at least one rune in a Go source
// gets a non-default foreground — proves the lexer ran and produced styles.
func TestHighlight_GoKeywordIsColored(t *testing.T) {
	src := "package main\nfunc f() {}\n"
	th := theme.Default()

	got := Highlight("main.go", src, th)
	base := tcell.StyleDefault.Background(th.BG).Foreground(th.Text)

	differs := false
	for _, row := range got {
		for _, st := range row {
			if st != base {
				differs = true
				break
			}
		}
		if differs {
			break
		}
	}
	if !differs {
		t.Fatal("expected at least one non-base styled rune in highlighted Go source")
	}
}

// TestHighlight_UnknownExtension gracefully falls back to the plain-text
// (or analyzed) lexer instead of panicking. The grid must still match the
// source shape.
func TestHighlight_UnknownExtension(t *testing.T) {
	src := "anything goes here\nsecond line"
	th := theme.Default()

	got := Highlight("file.totallymadeup", src, th)
	lines := strings.Split(src, "\n")
	if len(got) != len(lines) {
		t.Fatalf("rows = %d, want %d", len(got), len(lines))
	}
	for i, ln := range lines {
		if len(got[i]) != len([]rune(ln)) {
			t.Errorf("row %d len = %d, want %d", i, len(got[i]), len([]rune(ln)))
		}
	}
}

// TestHighlight_EmptyInput returns a single empty row, mirroring NewBuffer's
// behaviour — strings.Split("", "\n") yields [""], so the grid has one row
// of zero runes.
func TestHighlight_EmptyInput(t *testing.T) {
	th := theme.Default()
	got := Highlight("file.go", "", th)
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if len(got[0]) != 0 {
		t.Fatalf("expected empty row, got len=%d", len(got[0]))
	}
}

// TestHighlight_MultibyteRunes makes sure rune-indexed columns are handled
// when the source contains multi-byte characters — each row must have one
// style entry per rune (not per byte).
func TestHighlight_MultibyteRunes(t *testing.T) {
	src := "// héllo\nx := 1\n"
	th := theme.Default()

	got := Highlight("main.go", src, th)
	lines := strings.Split(src, "\n")
	for i, ln := range lines {
		if len(got[i]) != len([]rune(ln)) {
			t.Errorf("row %d: style len = %d, rune len = %d", i, len(got[i]), len([]rune(ln)))
		}
	}
}

// TestHighlight_NoFilenameAnalyses lets Chroma analyse the content when no
// filename is provided. It should still return a properly shaped grid.
func TestHighlight_NoFilenameAnalyses(t *testing.T) {
	src := "package main\nfunc main() {}\n"
	th := theme.Default()

	got := Highlight("", src, th)
	lines := strings.Split(src, "\n")
	if len(got) != len(lines) {
		t.Fatalf("rows = %d, want %d", len(got), len(lines))
	}
}

// TestHighlight_DiverseTokens exercises the styleForToken switch by feeding
// it source containing keywords, strings, numbers, comments, and names, then
// confirms the grid is well-formed. We don't assert on specific colors —
// only that the function ran across the diverse token types without panic
// and produced a parallel grid.
func TestHighlight_DiverseTokens(t *testing.T) {
	src := `// comment line
package main

import "fmt"

const Answer = 42

type Foo struct{}

func (f *Foo) Bar() string {
	return "hello"
}
`
	th := theme.Default()
	got := Highlight("main.go", src, th)
	lines := strings.Split(src, "\n")
	if len(got) != len(lines) {
		t.Fatalf("rows = %d, want %d", len(got), len(lines))
	}
	for i, ln := range lines {
		if len(got[i]) != len([]rune(ln)) {
			t.Errorf("row %d len = %d, want %d", i, len(got[i]), len([]rune(ln)))
		}
	}
}

// TestHighlightVisible_LimitsTokenisingToViewport pins the fast path:
// off-screen rows stay empty in the output while visible rows are tokenised.
// (A bounded lead above the viewport is also tokenised but discarded, so the
// output grid still only carries the visible rows.)
func TestHighlightVisible_LimitsTokenisingToViewport(t *testing.T) {
	th := theme.Default()
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "package main"
	}

	got := HighlightVisible("main.go", lines, 10, 2, th)
	if len(got) != len(lines) {
		t.Fatalf("rows = %d, want %d", len(got), len(lines))
	}
	if got[0] != nil {
		t.Fatalf("off-screen row was highlighted, got %v", got[0])
	}
	base := tcell.StyleDefault.Background(th.BG).Foreground(th.Text)
	if got[10][0] == base {
		t.Fatal("visible row was not highlighted")
	}
}

// TestHighlightVisible_LeadRecoversCommentState proves the bounded lead
// fixes the regression where scrolling into the middle of a multi-line
// comment coloured the body as plain code. The comment opens above the
// viewport; without the lead the lexer would start fresh at the visible
// rows and never know it was inside a comment.
func TestHighlightVisible_LeadRecoversCommentState(t *testing.T) {
	th := theme.Default()
	// A multi-line comment opens on line 1 and closes on line 6. The
	// viewport starts on line 4, inside the comment, with the opening /*
	// above the visible rows.
	lines := []string{
		"package main",   // 0
		"/*",             // 1  <- comment opens
		" * body one",    // 2
		" * body two",    // 3
		" * body three",  // 4  <- viewport start
		" * body four",   // 5
		" */",            // 6
		"func main() {}", // 7
	}
	got := HighlightVisible("main.go", lines, 4, 2, th)
	comment := tcell.StyleDefault.Background(th.BG).Foreground(th.SynComment).Italic(true)
	for _, i := range []int{4, 5} {
		row := got[i]
		if row == nil {
			t.Fatalf("line %d was not highlighted", i)
		}
		// At least one rune on each visible comment body line must carry
		// the comment style, proving the lead let the lexer re-enter the
		// block instead of tokenising the body as plain text.
		hasComment := false
		for _, st := range row {
			if st == comment {
				hasComment = true
				break
			}
		}
		if !hasComment {
			t.Errorf("line %d has no comment-styled rune; lead did not recover comment state", i)
		}
	}
}

func TestHighlightVisibleWindowReturnsOnlyRequestedRows(t *testing.T) {
	lines := make([]string, 100000)
	for i := range lines {
		lines[i] = "{\"value\": 1}"
	}
	start, rows := highlightVisibleWindow("large.json", lines, 50000, 40, theme.Default())
	if start != 50000 || len(rows) != 40 {
		t.Fatalf("start=%d rows=%d, want 50000/40", start, len(rows))
	}
}

func TestHighlightVisibleWindowClampsEdges(t *testing.T) {
	lines := []string{"one", "two", "three"}
	start, rows := highlightVisibleWindow("x.txt", lines, -4, 20, theme.Default())
	if start != 0 || len(rows) != 3 {
		t.Fatalf("start=%d rows=%d, want 0/3", start, len(rows))
	}
}

// TestHighlight_MarkdownStructuralStyles overlays Glow-inspired document cues
// without changing the shape of the style grid.
func TestHighlight_MarkdownStructuralStyles(t *testing.T) {
	th := theme.Default()
	src := "# Title\n## Section\n```go\nfmt.Println(1)\n```\nplain\n"
	got := Highlight("README.md", src, th)

	fgAt := func(line, col int) tcell.Color {
		fg, _, _ := got[line][col].Decompose()
		return fg
	}
	if fgAt(0, 0) != th.Accent {
		t.Fatalf("h1 fg = %v, want Accent", fgAt(0, 0))
	}
	if fgAt(1, 0) != th.AccentSoft {
		t.Fatalf("h2 fg = %v, want AccentSoft", fgAt(1, 0))
	}
	if fgAt(2, 0) != th.Muted {
		t.Fatalf("fence fg = %v, want Muted", fgAt(2, 0))
	}
	if fgAt(2, 3) != th.Accent {
		t.Fatalf("fence language fg = %v, want Accent", fgAt(2, 3))
	}
	if fgAt(4, 0) != th.Muted {
		t.Fatalf("closing fence fg = %v, want Muted", fgAt(4, 0))
	}
	if len(got[0]) != len([]rune("# Title")) {
		t.Fatalf("heading row length changed: %d", len(got[0]))
	}
}

// TestHighlight_MarkdownFencedCode overlays language-aware colors inside
// Markdown fences while preserving the document-level structural styles.
func TestHighlight_MarkdownFencedCode(t *testing.T) {
	th := theme.Default()

	foregroundAt := func(styles [][]tcell.Style, line, col int) tcell.Color {
		fg, _, _ := styles[line][col].Decompose()
		return fg
	}
	assertForeground := func(t *testing.T, styles [][]tcell.Style, line, col int, want tcell.Color) {
		t.Helper()
		if got := foregroundAt(styles, line, col); got != want {
			t.Fatalf("foreground at %d:%d = %v, want %v", line, col, got, want)
		}
	}

	t.Run("Go", func(t *testing.T) {
		src := "```go\nfunc main() {}\n\t// comment\n```\n"
		got := Highlight("README.md", src, th)

		assertForeground(t, got, 1, 0, th.SynKeyword)
		assertForeground(t, got, 2, 1, th.SynComment)
	})

	t.Run("JSON", func(t *testing.T) {
		line := `{"name":"spice","count":42}`
		src := "```json\n" + line + "\n```\n"
		got := Highlight("README.md", src, th)

		assertForeground(t, got, 1, strings.Index(line, `"spice"`), th.SynString)
		assertForeground(t, got, 1, strings.Index(line, "42"), th.SynNumber)
	})

	t.Run("UnlabelledAndUnknownUseCodeBase", func(t *testing.T) {
		src := "```\nplain text\n```\n```not-a-language\nunknown text\n```\n"
		got := Highlight("README.md", src, th)

		assertForeground(t, got, 1, 0, th.SynPunct)
		assertForeground(t, got, 4, 0, th.SynPunct)
	})

	t.Run("FourBackticksRequireFourToClose", func(t *testing.T) {
		src := "````\nbody one\n```\nbody two\n````\n"
		got := Highlight("README.md", src, th)

		assertForeground(t, got, 1, 0, th.SynPunct)
		assertForeground(t, got, 2, 0, th.SynPunct)
		assertForeground(t, got, 3, 0, th.SynPunct)
		assertForeground(t, got, 4, 0, th.Muted)
	})

	t.Run("TildeFence", func(t *testing.T) {
		src := "~~~json\n{\"value\": \"one\", \"count\": 1}\n~~~\n"
		got := Highlight("README.md", src, th)

		assertForeground(t, got, 1, 10, th.SynString)
		assertForeground(t, got, 1, 26, th.SynNumber)
		assertForeground(t, got, 2, 0, th.Muted)
	})

	t.Run("UnclosedFenceExtendsToEnd", func(t *testing.T) {
		src := "```go\nfunc main() {}\n"
		got := Highlight("README.md", src, th)

		assertForeground(t, got, 1, 0, th.SynKeyword)
	})
}

// TestHighlight_MarkdownNonMarkdown keeps Markdown
// punctuation from being recolored in unrelated file types.
func TestHighlight_MarkdownNonMarkdown(t *testing.T) {
	th := theme.Default()
	got := Highlight("notes.txt", "# Title\n```go\n", th)
	base := tcell.StyleDefault.Background(th.BG).Foreground(th.Text)
	if got[0][0] != base {
		t.Fatalf("text file heading-like line was styled: %v", got[0][0])
	}
}
