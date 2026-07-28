# CJK Terminal Cell Width Design

## Problem

Issue #39 reports that CJK text loses every second character in the editor and file tree. CJK runes occupy two terminal cells, but SpiceEdit currently advances one screen cell per rune. It then writes the next rune into the trailing cell of the wide rune. Tcell defines that write as undefined.

## Scope

This change makes all runtime text use terminal-cell widths where layout or drawing depends on width. It covers editor content, file-tree names, tab labels, status text, finder and find input, form and prompt input, and dynamic modal text.

The buffer, cursor, selection, find matches, and undo history remain rune-indexed. This change does not redesign editing around grapheme clusters.

## Shared terminal-text package

Add `internal/termtext` as the single source of truth for display widths and bounded text drawing.

- `StringWidth` returns the terminal-cell width of a string with `github.com/rivo/uniseg`.
- `RuneWidth` returns a rune's width for editor visual-column calculations. It preserves a minimum width of one for zero-width runes so existing rune-indexed cursor behavior stays stable.
- `DrawString` walks grapheme clusters, skips a cluster that would exceed the supplied cell width, and draws each accepted cluster with `tcell.Screen.Put`. It returns the number of cells drawn.

`uniseg` is already a transitive dependency of tcell. SpiceEdit will add it as a direct dependency because application code imports it through `termtext`.

## Editor behavior

`RuneVisualWidth` will keep its tab-stop rule and use `termtext.RuneWidth` for every other rune. This makes `LineVisualCol` and `RuneColAtVisual` CJK-aware without changing their rune-indexed API.

`Tab.Render` will paint a tab as explicit spaces across its tab-stop span. For ordinary runes it will paint only the rune's first cell, and only when the complete rune fits in the viewport. The prepainted line background supplies the trailing cell of a wide rune. This prevents overwriting a CJK rune's second cell.

`EnsureVisible` will compare visual columns, then choose a rune boundary at or after the required visual column. This keeps the cursor visible after CJK text and avoids a viewport starting in the second cell of a rune. Mouse hit testing, selection, find highlighting, and overflow indicators will use the corrected visual-column helpers.

## Application and file-tree behavior

The file tree will use `termtext.DrawString` for row segments and `termtext.StringWidth` when positioning icon and name segments. It will truncate before a partial wide rune instead of letting tcell replace it with a blank.

The application will route dynamic strings through the same drawing helper and use `StringWidth` for right alignment, centering, layout, and truncation. Text fields will calculate each rune's screen offset from the prefix width rather than from its rune index.

## Tests

Tests will prove the following behavior.

- `termtext` reports `中文测试` as eight cells and draws wide runes without writing into their trailing cells.
- Editor visual-column conversion, rendering, cursor placement, hit testing, and horizontal visibility honor CJK widths.
- A file-tree row renders the complete CJK filename `中文测试.txt` when space permits.
- App-level dynamic text placement uses cell widths for CJK tab labels and input values.

## Non-goals

This change does not change keyboard shortcuts, editor command behavior, file encoding, or Unicode normalization. It does not promise full grapheme-cluster cursor semantics; that would require a separate buffer-model change.
