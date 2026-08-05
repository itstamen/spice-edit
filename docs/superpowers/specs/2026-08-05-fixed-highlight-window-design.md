# Fixed Syntax Highlight Window Design

## Status

- Date: 2026-08-05
- Status: Approved for implementation planning

## Problem

Large-file navigation can visibly flash between plain and syntax-coloured text.
The existing rolling cache extends one line at a time after the viewport reaches
its prefetched edge. Each extension tokenises a new source window, so fast
PageDown input repeatedly performs expensive highlighting and flushes many
intermediate frames.

## Decision

Use a fixed relative syntax-highlight window for large files. The cache covers
the visible logical rows plus a bounded prefetch region above and below. A
render reuses the cache whenever its visible rows fit entirely inside it. When
the viewport leaves that range, the editor tokenises and installs one complete
replacement window around the new viewport.

Small files remain fully highlighted as a single cache to avoid any cache
boundary during normal navigation.

## Behaviour

- Fast navigation inside the cached range does not invoke the highlighter.
- Crossing a large-file cache boundary invokes the highlighter once for a
  complete, already-styled window; the renderer does not grow or trim the
  cache one line at a time.
- Content edits, theme changes, and lexer-sensitive path changes invalidate
  the cache and rebuild it on the next render.
- Soft-wrap rendering continues to derive the highlighted logical range from
  its visible visual rows.

## Implementation

- Remove incremental cache-extension helpers from `internal/editor/tab.go`.
- Keep the existing whole-file threshold and bounded prefetch calculation.
- Rebuild the cache only after invalidation or when the visible range no longer
  fits inside the current cached window.
- Update editor tests to assert that a boundary crossing replaces the complete
  cache once, while subsequent navigation within the new window reuses it.

## Validation

- Add focused tests for large-file boundary crossing and repeated navigation
  after that crossing.
- Run `gofmt` and `make test`.

## Out of Scope

- Background or asynchronous highlighting.
- Lexer state checkpoints or whole-file syntax caches for arbitrarily large
  documents.
- Changes to terminal frame batching or rendering APIs.
