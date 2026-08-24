# Markdown Prose and Fenced Code Contrast Design

## Document information

- **Date:** 2026-08-24
- **Status:** Approved for implementation

## Problem

Markdown files currently pass through Chroma's document-level Markdown lexer
before SpiceEdit applies its structural heading and fenced-code styles. This
causes ordinary prose, including Chinese paragraphs, to inherit string-like
syntax colors instead of the editor's primary text color.

Fenced blocks without a language label use `SynPunct` as their base color.
That color is too close to the primary text color, so an unlabelled prompt
block can look identical to surrounding prose. A labelled fence such as
`bash`, `json`, or `go` does receive language-aware token colors, which is why
the behavior differs between otherwise similar Markdown files.

## Rendering behavior

### Markdown prose

- Ordinary Markdown prose uses the theme's primary `Text` color.
- Existing heading colors remain unchanged.
- Fence punctuation remains `Muted` and a language label remains bold
  `Accent`.
- Markdown is not tokenized with the document-level Chroma Markdown lexer, so
  prose cannot be recolored as a string, keyword, or other programming token.

### Labelled fenced code

- A recognized language label selects the matching Chroma lexer, including
  aliases such as `bash`, `sh`, `go`, `json`, and `python`.
- The fenced body uses SpiceEdit's existing token-to-theme mapping for
  keywords, strings, numbers, comments, functions, types, variables,
  operators, punctuation, and constants.
- Identifiers and unclassified tokens use the fenced-code base color rather
  than the surrounding prose color.

### Unlabelled and unknown fenced code

- No language inference is attempted.
- The entire fenced body uses a new neutral `CodeText` theme color that is
  clearly darker than `Text` but remains readable against the editor
  background.
- Unknown language labels use the same neutral fallback.

## Architecture

`internal/editor/highlight.go` remains the owner of syntax highlighting.
`highlightSource` will treat Markdown as a structural document: it seeds the
style grid with the primary text style and calls the Markdown structural pass
without first applying the document-level Markdown lexer. Non-Markdown files
continue through the existing filename/content lexer path unchanged.

`internal/theme/theme.go` will add `CodeText` to the curated theme. The fenced
code structural pass will use it as the base for both fallback blocks and the
default tokens emitted by a recognized nested lexer.

## Tests

Regression tests will verify that:

- ordinary Markdown prose uses `Text`, including prose Chroma previously
  classified as a string;
- headings and fence punctuation retain their existing colors;
- labelled Go, JSON, and Bash fences use language token colors;
- identifiers inside labelled fences use `CodeText` rather than prose `Text`;
- unlabelled and unknown fences use `CodeText` uniformly;
- non-Markdown syntax highlighting remains unchanged;
- visible-window highlighting preserves the same behavior.

## Out of scope

- Automatic language detection for unlabelled fences.
- Importing or configuring a full VS Code theme.
- Markdown preview rendering or Markdown syntax concealment.
- Changing syntax colors for non-Markdown files.
