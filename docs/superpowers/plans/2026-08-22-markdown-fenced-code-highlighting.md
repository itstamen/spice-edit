# Markdown Fenced Code Highlighting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Give Markdown fenced code blocks a distinct code foreground and language-aware syntax colors.

**Architecture:** Keep work in the existing editor highlighter. Apply the document Markdown lexer first, then parse fenced ranges and overlay each fence body with a Chroma lexer or the distinct code base style.

**Tech Stack:** Go, Chroma v2.24.0, tcell, Go test.

## Global Constraints

- Preserve existing Markdown heading, fence punctuation, and language-label styles.
- Support backtick and tilde fences with matching marker character and opening-marker length.
- Resolve Chroma aliases; missing and unknown labels use the existing softer syntax-text color.
- An unclosed fence styles through end of buffer; non-Markdown files remain unchanged.
- Keep existing public Highlight and HighlightVisible APIs and bounded highlight-window behavior.

---

### Task 1: Parse and Highlight Fenced Bodies

**Files:**
- Modify: internal/editor/highlight.go
- Modify: internal/editor/highlight_test.go

**Interfaces:**
- Produce a private fence descriptor that records marker rune, marker length, and language label.
- Produce a private lexer-overlay helper that applies a lexer to a style-grid line range using a supplied base style.
- Reuse styleForToken, styleRow, styleRuneRange, and lexers.Get.

- [ ] **Step 1: Write failing editor tests**

Add tests that use Highlight on README.md and inspect foreground colors. Cover Go keyword and comment colors inside a go fence; JSON string and number colors inside a json fence; unlabelled and unknown body text using SynPunct rather than Text; a four-backtick opening not closed by three backticks; tilde fences; unclosed fences; current heading/fence-label colors; and a notes.txt control case.

- [ ] **Step 2: Verify focused tests fail**

Run: go test ./internal/editor -run 'TestHighlight_Markdown(FencedCode|StructuralStyles|.*NonMarkdown)' -count=1

Expected: FAIL because body text still inherits document-level Markdown styling and fence length is not tracked.

- [ ] **Step 3: Extract shared lexer application**

Move the token scan currently in highlightSource into a private helper taking lexer, source, line offset, style grid, theme, and base style. It must coalesce the lexer, map tokens through styleForToken, and write only in-bounds rune cells. Call it from highlightSource with the normal Text base so ordinary source highlighting is unchanged.

- [ ] **Step 4: Apply fenced-body overlays**

Update the Markdown structural pass to track active marker character and length. It must style fence punctuation Muted and opening language labels bold Accent, then identify each exclusive body range. Seed every body row with base.Foreground(theme.SynPunct). For a nonempty recognized label, join only body lines, obtain lexers.Get(label), and apply the shared lexer helper with the code base. Do not call highlightSource from the nested path. On end of input, flush any unclosed body range.

- [ ] **Step 5: Verify editor coverage**

Run:

    go test ./internal/editor -run 'TestHighlight_Markdown(FencedCode|StructuralStyles|.*NonMarkdown)' -count=1
    go test ./internal/editor -count=1

Expected: PASS.

- [ ] **Step 6: Format, validate, and commit**

Run:

    gofmt -w internal/editor/highlight.go internal/editor/highlight_test.go
    make test
    make build
    git diff --check

Commit:

    git add internal/editor/highlight.go internal/editor/highlight_test.go
    git commit -m "Highlight Markdown fenced code blocks"

### Task 2: Fenced-Highlighting Review

**Files:**
- Modify: no source changes expected.

- [ ] **Step 1: Inspect final task diff**

Confirm only the highlighter and its tests changed, the nested path does not recurse, and code-body base styles differ from prose.

- [ ] **Step 2: Run final focused verification**

Run:

    go test ./internal/editor -run 'TestHighlight_(Markdown|Visible)' -count=1
    git diff --check
    git status --short

Expected: tests and whitespace checks PASS; unrelated current-worktree files remain unstaged.
