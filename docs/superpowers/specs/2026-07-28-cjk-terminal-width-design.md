# CJK Terminal Cell Width Design

## Problem

Issue #39 reports that CJK text loses every second character in the editor and file tree. CJK runes occupy two terminal cells, but SpiceEdit advances one screen cell per rune. It then writes the next rune into the trailing cell of the wide rune. Tcell defines that write as undefined.

## Scope

This change makes all runtime text use terminal-cell widths where layout or drawing depends on width. It covers editor content, file-tree names, tab labels, status text, finder and find input, form and prompt input, dynamic modal text, and Finder result rows.

The buffer, cursor, selection, find matches, and undo history remain rune-indexed. The renderer and layout become grapheme-aware: several rune positions inside one grapheme may share one screen cell. This change does not make cursor movement or deletion grapheme-indexed.

## Data flow

```text
UTF-8 text
    |
    v
termtext grapheme iterator
    | cluster text + rune range + terminal-cell width
    +---------------------+---------------------+
    |                     |                     |
    v                     v                     v
editor                file tree              app UI
visual columns        row segments           text fields, labels,
render/hit-test       icon offsets           tabs, status, Finder
    |                     |                     |
    +---------------------+---------------------+
                          |
                          v
                    tcell.Screen.Put
```

## Shared terminal-text package

Add `internal/termtext` as the single source of truth for display widths, grapheme iteration, truncation, and bounded drawing.

- `StringWidth` returns a string's terminal-cell width with `github.com/rivo/uniseg`.
- A grapheme iterator reports each cluster's text, starting rune index, rune count, and cell width.
- `DrawString` draws complete graphemes within a cell limit with `tcell.Screen.Put` and returns the cells used.
- A cluster-level drawing helper lets callers choose a style per grapheme. Finder uses it to preserve directory dimming and match highlighting without changing its rune-indexed match API.
- A truncation helper keeps only complete graphemes and appends an ellipsis when it fits.

`uniseg` is already a transitive dependency of tcell. SpiceEdit will add it as a direct dependency because application code imports it through `termtext`.

## Editor behavior

Tabs retain their existing four-cell tab-stop rule. Non-tab editor text uses the grapheme iterator's actual cell widths. `LineVisualCol` and `RuneColAtVisual` remain rune-indexed APIs, but use grapheme boundaries so every rune in one grapheme maps to the same visual cell range.

`Tab.Render` paints each ordinary grapheme once, only when the complete grapheme fits in the viewport. The prepainted line background supplies a wide grapheme's trailing cells. When a grapheme contains multiple runes, its syntax, selection, and find style is derived from its rune range; a selected or matched rune applies that visual state to the visible grapheme.

`EnsureVisible` compares visual columns and chooses a grapheme boundary at or after the required column. This prevents horizontal scrolling from starting in the second cell of a wide character or inside a combining sequence. Mouse hit testing, selection, find highlighting, and overflow indicators use the same visual-column rules.

## Application and file-tree behavior

The file tree uses `termtext.DrawString` for row segments and `termtext.StringWidth` for icon and filename offsets. It truncates before a partial grapheme instead of letting tcell replace a wide rune with a blank.

The application removes misleading rune-count layout helpers. It uses `termtext.StringWidth` for right alignment, centering, tab geometry, status text, and truncation.

Prompt, form, find, and Finder inputs share one App-level single-line input renderer. The helper owns visual scrolling, complete-grapheme drawing, and caret placement; each UI supplies its rune slice, cursor, scroll state, and field rectangle. This prevents four input surfaces from implementing divergent CJK behavior.

Finder keeps `MatchedIndexes` as rune indexes. Its row renderer iterates graphemes, then applies the match style when any rune in that grapheme is matched. This preserves current fuzzy-match contracts and visual highlighting.

## Tests

Use same-package Go tests and `tcell.NewSimulationScreen("UTF-8")` for rendering checks.

```text
termtext
  ├─ grapheme ranges and widths
  ├─ bounded drawing and truncation
  └─ no partial wide grapheme

editor
  ├─ visual columns, cursor, scroll, and hit testing
  └─ render, selection, and find styles for CJK and combining text

file tree
  └─ CJK filenames, icons, offsets, and row-edge truncation

app
  ├─ tabs, status, menus, and modals
  ├─ prompt, form, find, and Finder input cursor/scroll behavior
  └─ Finder CJK paths with per-grapheme match highlighting
```

The tests must cover `中文测试`, a combining sequence such as `e\u0301`, and a decomposed CJK-adjacent sequence such as `か\u3099`.

## Failure modes

- A mismatch between `termtext` and tcell widths can overwrite a trailing cell. Exact SimulationScreen cell assertions prevent this silent regression.
- A bounded draw can split the final wide grapheme. Boundary tests require drawing to stop before the partial grapheme.
- Horizontal scrolling can start inside a grapheme. Editor and App-input tests require scroll positions to align to grapheme boundaries.
- Finder can highlight the wrong character after a wide or combining grapheme. CJK path tests require a cluster to highlight when any member rune matches.

## Non-goals

This change does not alter keyboard shortcuts, editor command behavior, file encoding, or Unicode normalization. It does not make grapheme clusters indivisible editing units; that separate buffer-model change is recorded in `TODO.md`.
