# TODO

## Grapheme-aware buffer positions

**What:** Make cursor movement, selection, deletion, and find navigation operate on grapheme boundaries instead of rune boundaries.

**Why:** The CJK display-width work renders and lays out graphemes correctly while preserving rune-indexed buffer positions. A caret can therefore occupy several logical positions inside one visible combining sequence.

**Pros:** Editing behavior matches the visible characters for combining marks, emoji sequences, and decomposed Unicode.

**Cons:** This changes `editor.Position`, buffer edits, undo snapshots, find matches, selection rules, mouse hit testing, and their tests.

**Context:** Start with `internal/editor/buffer.go` and the `Position` API. Preserve file contents and Unicode normalization; only change navigation and edit boundaries. The CJK terminal-width design at `docs/superpowers/specs/2026-07-28-cjk-terminal-width-design.md` supplies correct display metrics but intentionally does not make graphemes atomic edits.

**Depends on / blocked by:** No external dependency. Treat this as a separately designed buffer-model migration after the CJK display-width fix lands.

## Soft wrap visual-row navigation

**What:** Design and implement full visual-row navigation for soft wrap.

**Why:** Soft wrap V1 keeps navigation logical-line based: Up/Down, Page up/down, wheel scrolling, continuation-row mouse clicks, and find scrolling do not yet follow the exact wrapped visual rows. Users will eventually expect cursor movement and hit testing to match what they see on screen.

**Pros:** Makes soft wrap feel like a complete editor feature instead of a read-only display aid. It also gives mouse placement, find reveal, and page scrolling one shared visual-row model.

**Cons:** This touches high-risk editor behavior: `MoveCursor`, `EnsureVisible`, `HitTest`, find reveal, page scrolling, wheel scrolling, selection extension, and CJK/combining boundary tests. A partial implementation would be worse than deferring it.

**Context:** Start from `docs/superpowers/specs/2026-08-01-soft-wrap-prd.md`, where V1 explicitly defers full visual-row navigation. Build on the V1 wrap-layout helper so logical buffer positions and wrapped visual segments share one mapping instead of duplicating width calculations across render, input, and find paths.

**Depends on / blocked by:** Soft wrap V1 must land first with a tested wrap-layout helper and cell-level rendering tests for CJK, combining sequences, and tab boundaries.
