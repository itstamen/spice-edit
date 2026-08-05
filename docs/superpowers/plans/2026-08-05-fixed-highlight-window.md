# Clamped Scroll Highlight Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the plain-text frame that appears when rapid large-file navigation overshoots the final viewport before syntax highlighting is computed.

**Architecture:** `RenderSoftWrap` clamps logical scrolling before deriving the highlight range. The existing relative cache then builds styles for the same lines the renderer draws, including the final viewport after a fast jump to the end of a file.

**Tech Stack:** Go 1.24, Chroma v2, tcell v2, standard Go testing.

## Global Constraints

- Do not add dependencies, background work, or new rendering APIs.
- Keep bounded large-file highlighting and whole-file caching at or below `highlightWholeFileLines`.
- Preserve invalidation on edits, theme changes, and lexer-sensitive path changes.
- Add same-package regression tests and finish with `gofmt` and `make test`.

---

## File Structure

| Path | Responsibility |
|---|---|
| `internal/editor/tab.go` | Clamp logical scrolling before calculating the syntax-highlight range. |
| `internal/editor/tab_test.go` | Test that a clamped final viewport has styles in its first rendered frame. |

### Task 1: Highlight the Clamped Viewport

**Files:**
- Modify: `internal/editor/tab.go`
- Modify: `internal/editor/tab_test.go`

**Consumes:** `Tab.clampScroll(int)`, `Tab.ensureHighlightWindow(int, int, theme.Theme)`, and `Tab.highlightStylesForLine(int)`.

**Produces:** `RenderSoftWrap` computes styles for its post-clamp logical viewport.

- [x] **Step 1: Write the clamped-scroll regression test**

In `internal/editor/tab_test.go`, retain the existing cache tests and add this test after them:

```go
func TestTab_RenderHighlightsClampedScrollAtEnd(t *testing.T) {
	tab := highlightCacheTestTab(5000)
	scr := highlightCacheScreen(t)
	tab.ScrollY = tab.Buffer.LineCount() + 100
	tab.cursorMoved = false
	tab.Render(scr, theme.Default(), 0, 0, 80, 20)

	wantScrollY := tab.Buffer.LineCount() - 20 + 20/2
	if tab.ScrollY != wantScrollY {
		t.Fatalf("ScrollY=%d, want %d", tab.ScrollY, wantScrollY)
	}
	if styles := tab.highlightStylesForLine(tab.ScrollY); len(styles) == 0 {
		t.Fatal("clamped final viewport has no syntax styles")
	}
}
```

- [x] **Step 2: Verify the regression fails before the viewport fix**

Run:

```bash
go test ./internal/editor -run TestTab_RenderHighlightsClampedScrollAtEnd -count=1
```

Expected: FAIL because `ensureHighlightWindow` receives the pre-clamp out-of-range `ScrollY`, clears `Styles`, then the renderer draws the clamped final viewport without syntax styles.

- [x] **Step 3: Compute the highlight range after clamping**

In `RenderSoftWrap`, replace the pre-clamp initialisation:

```go
	highlightStart, highlightHeight := t.ScrollY, h
```

with post-clamp calculation in the non-soft-wrap branch:

```go
	t.clampScroll(h)
	highlightStart = t.ScrollY
	highlightHeight = h
```

- [x] **Step 4: Format and run focused regressions**

Run:

```bash
gofmt -w internal/editor/tab.go internal/editor/tab_test.go
go test ./internal/editor -run 'TestTab_Render(HighlightsClampedScrollAtEnd|ReusesHighlightInsidePrefetchWindow|CachesWholeSmallFileHighlight|RebuildsHighlightOutsidePrefetchWindow|RebuildsHighlightForThemeAndPath)' -count=1
```

Expected: PASS.

- [x] **Step 5: Validate the complete repository**

Run:

```bash
git diff --check
make test
```

Expected: no whitespace errors and `go test -race ./...` passes.

- [ ] **Step 6: Commit only this cache fix**

Run:

```bash
git add internal/editor/tab.go internal/editor/tab_test.go
git commit -m "Highlight clamped final viewport"
```

Expected: do not include the pre-existing image-preview documentation changes.

## Plan Review

- Spec coverage: Task 1 aligns the highlighted logical range with the clamped rendered viewport and retains the existing cache strategy and invalidation.
- Placeholder scan: no incomplete steps or deferred implementation details remain.
- Type consistency: the plan uses existing `Tab`, `Buffer`, `Render`, `clampScroll`, and `highlightStylesForLine` APIs.
