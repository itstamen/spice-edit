# Markdown Prose and Fenced Code Contrast Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render Markdown prose in the primary text color while giving labelled fenced code language-aware syntax colors and unlabelled fenced code a clearly distinct neutral color.

**Architecture:** Add a dedicated `Theme.CodeText` color for the default contents of fenced code. Treat Markdown as a structural document in `highlightSource`: start with the primary text grid, skip the document-level Markdown lexer, then apply heading, fence, and nested language-lexer overlays only where required.

**Tech Stack:** Go, tcell, Chroma v2, Go tests.

**Spec:** `docs/superpowers/specs/2026-08-24-markdown-prose-code-contrast-design.md`

## Global Constraints

- Ordinary Markdown prose uses `Theme.Text`.
- Existing heading, fence punctuation, and fence language-label styles remain unchanged.
- Recognized fence labels use the matching Chroma lexer and existing syntax-token colors.
- Unlabelled and unknown fences use `Theme.CodeText`; do not infer a language.
- Non-Markdown syntax highlighting remains unchanged.
- Keep the cached visible-window highlighting path intact.
- Cache sparse Markdown fence checkpoints per `Tab.textRevision`; each
  visible-window state lookup scans at most 256 structural lines.

---

### Task 1: Add a Dedicated Fenced-Code Base Color

**Files:**
- Modify: `internal/theme/theme.go`
- Test: `internal/theme/theme_test.go`

**Interfaces:**
- Produces: `theme.Theme.CodeText tcell.Color`
- Consumes: no new interface

- [ ] **Step 1: Write the failing theme tests**

Add `{"CodeText", th.CodeText}` to the explicit color list in
`TestDefault_AllColorsSet`. Add these contrast invariants to
`TestDefault_ContrastInvariants`:

```go
{"CodeText vs Text", th.CodeText, th.Text},
{"CodeText vs BG", th.CodeText, th.BG},
```

- [ ] **Step 2: Run the focused test and verify failure**

Run:

```bash
go test ./internal/theme -count=1
```

Expected: compilation fails because `Theme.CodeText` does not exist.

- [ ] **Step 3: Add the theme field and default color**

Add the field beside the syntax-highlighting colors:

```go
CodeText tcell.Color // Neutral base text inside fenced code blocks.
```

Assign a readable, clearly subdued Tokyo Night-compatible value in `Default`:

```go
CodeText: tcell.NewRGBColor(0x8f, 0x98, 0xb8),
```

- [ ] **Step 4: Format and verify the theme package**

Run:

```bash
gofmt -w internal/theme/theme.go internal/theme/theme_test.go
go test ./internal/theme -count=1
git diff --check
```

Expected: all commands pass.

- [ ] **Step 5: Commit the theme change**

```bash
git add internal/theme/theme.go internal/theme/theme_test.go
git commit -m "Add fenced code text color"
```

---

### Task 2: Separate Markdown Prose from Fenced-Code Highlighting

**Files:**
- Modify: `internal/editor/highlight.go`
- Test: `internal/editor/highlight_test.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: `theme.Theme.CodeText tcell.Color` from Task 1
- Produces: Markdown highlighting where prose uses `Text` and fenced bodies use `CodeText` plus nested lexer overlays

- [ ] **Step 1: Write failing Markdown regression tests**

Change the source in `TestHighlight_MarkdownStructuralStyles` and add the
prose assertions:

```go
prose := "普通正文：保持主文本颜色"
src := "# Title\n## Section\n```go\nfmt.Println(1)\n```\n" + prose + "\n"
got := Highlight("README.md", src, th)

for col := range []rune(prose) {
	if fgAt(5, col) != th.Text {
		t.Fatalf("prose fg at col %d = %v, want Text", col, fgAt(5, col))
	}
}
```

Update `TestHighlight_MarkdownFencedCode` so the
`UnlabelledAndUnknownUseCodeBase` case expects `th.CodeText` rather than
`th.SynPunct`. Add a Bash case:

```go
t.Run("Bash", func(t *testing.T) {
	line := `echo "hello"`
	src := "```bash\n" + line + "\n```\n"
	got := Highlight("README.md", src, th)

	assertForeground(t, got, 1, strings.Index(line, `"hello"`), th.SynString)
})
```

Add this visible-window regression:

```go
func TestHighlightVisible_MarkdownProseAndFencedCode(t *testing.T) {
	th := theme.Default()
	prose := "普通正文"
	jsonLine := `{"name":"spice","count":42}`
	lines := []string{prose, "```json", jsonLine, "```"}
	got := HighlightVisible("README.md", lines, 0, len(lines), th)

	foregroundAt := func(line, col int) tcell.Color {
		fg, _, _ := got[line][col].Decompose()
		return fg
	}
	for col := range []rune(prose) {
		if fg := foregroundAt(0, col); fg != th.Text {
			t.Fatalf("prose fg at col %d = %v, want Text", col, fg)
		}
	}
	if fg := foregroundAt(2, strings.Index(jsonLine, `"spice"`)); fg != th.SynString {
		t.Fatalf("JSON string fg = %v, want SynString", fg)
	}
	if fg := foregroundAt(2, strings.Index(jsonLine, "42")); fg != th.SynNumber {
		t.Fatalf("JSON number fg = %v, want SynNumber", fg)
	}
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run:

```bash
go test ./internal/editor -run 'TestHighlight_(Markdown|Visible)' -count=1
```

Expected: at least the prose and unlabelled-code assertions fail because the
current implementation applies the Markdown lexer globally and uses
`SynPunct` as the fenced base.

- [ ] **Step 3: Change Markdown highlighting ownership**

In `highlightSource`, build the base grid before choosing a lexer. For a
Markdown filename, call `applyMarkdownStructuralStyles` and return without
calling the document-level Markdown Chroma lexer:

```go
base := tcell.StyleDefault.Background(t.BG).Foreground(t.Text)
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
```

In `applyMarkdownStructuralStyles`, change the fenced base to:

```go
codeBase := base.Foreground(t.CodeText)
```

Keep the existing recognized-language lookup and `applyLexerOverlay` call so
token categories continue using `styleForToken`.

- [ ] **Step 4: Update the README behavior description**

Replace the Markdown sentences in the syntax-highlighting feature bullet with:

```markdown
  rendering that keeps large-file navigation responsive. Markdown prose uses
  the primary text color; fenced code blocks use their declared language (for
  example, `go`, `json`, or `bash`) for syntax colors, while unlabelled or
  unknown fences use a distinct neutral code color without language guessing.
```

- [ ] **Step 5: Format and run focused tests**

Run:

```bash
gofmt -w internal/editor/highlight.go internal/editor/highlight_test.go
go test ./internal/editor -run 'TestHighlight_(Markdown|Visible)' -count=1
git diff --check
```

Expected: all commands pass.

- [ ] **Step 6: Run repository validation and build**

Run:

```bash
make test
CGO_ENABLED=0 go test ./internal/editor -count=1
make build
```

Expected: all tests pass and `bin/spiceedit` is produced.

- [ ] **Step 7: Commit the Markdown rendering fix**

```bash
git add README.md internal/editor/highlight.go internal/editor/highlight_test.go
git commit -m "Separate Markdown prose and fenced code colors"
```

---

### Final Review Amendment: Bound Deep Markdown Fence Recovery

The final review found that a correct deep-window recovery cannot rescan the
entire prefix for every window. Implement sparse fence-state checkpoints in
`internal/editor/highlight.go`, cache them per `Tab.textRevision` in
`internal/editor/tab.go`, invalidate them from `markTextChanged`, and reuse
the same index for highlight rebuilds and extensions. Add a cache lifecycle
test in `internal/editor/tab_test.go` and a 100,000-line Markdown benchmark in
`internal/editor/performance_test.go` that queries top, middle, and end
windows with a prebuilt index.
