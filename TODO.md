# TODO

## Grapheme-aware buffer positions

**What:** Make cursor movement, selection, deletion, and find navigation operate on grapheme boundaries instead of rune boundaries.

**Why:** The CJK display-width work renders and lays out graphemes correctly while preserving rune-indexed buffer positions. A caret can therefore occupy several logical positions inside one visible combining sequence.

**Pros:** Editing behavior matches the visible characters for combining marks, emoji sequences, and decomposed Unicode.

**Cons:** This changes `editor.Position`, buffer edits, undo snapshots, find matches, selection rules, mouse hit testing, and their tests.

**Context:** Start with `internal/editor/buffer.go` and the `Position` API. Preserve file contents and Unicode normalization; only change navigation and edit boundaries. The CJK terminal-width design at `docs/superpowers/specs/2026-07-28-cjk-terminal-width-design.md` supplies correct display metrics but intentionally does not make graphemes atomic edits.

**Depends on / blocked by:** No external dependency. Treat this as a separately designed buffer-model migration after the CJK display-width fix lands.
