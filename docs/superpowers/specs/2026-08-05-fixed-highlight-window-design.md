# Clamped Scroll Syntax Highlight Design

## Status

- Date: 2026-08-05
- Status: Approved for implementation planning

## Problem

Large-file navigation can visibly flash between plain and syntax-coloured text.
`RenderSoftWrap` derived the logical highlight range before it clamped an
overshot `ScrollY` to the final viewport. The highlighter therefore cleared
its cache for the out-of-range position, while the renderer drew the clamped
position without syntax styles for one frame.

## Decision

Derive the non-soft-wrap highlight range only after calling `clampScroll`.
The highlighter and renderer then reference the same final logical viewport.
The existing whole-file and bounded-window cache policies remain unchanged.

## Behaviour

- Fast navigation that overshoots the document end clamps the scroll position
  before it invokes the highlighter.
- The first frame at the clamped final viewport has a populated relative style
  row for its visible content.
- Content edits, theme changes, and lexer-sensitive path changes invalidate
  the cache and rebuild it on the next render.
- Soft-wrap rendering continues to derive the highlighted logical range from
  its visible visual rows.

## Implementation

- Move non-soft-wrap `highlightStart` and `highlightHeight` assignment after
  `clampScroll(h)` in `internal/editor/tab.go`.
- Add an editor regression test with an out-of-range `ScrollY` and assert that
  the clamped first visible line has styles after one render.

## Validation

- Add a focused test for an overshot final viewport.
- Run `gofmt` and `make test`.

## Out of Scope

- Changes to terminal frame batching, cache size, or rendering APIs.
