# Markdown Fenced Code Highlighting Design

## Document information

- **Date:** 2026-08-22
- **Status:** Approved for implementation

## Summary

Markdown fenced code blocks will render differently from surrounding prose.
When the opening fence includes a language label, SpiceEdit will apply the
matching Chroma lexer to the fence body. Fences without a recognized language
will still use a distinct, muted code-text foreground so ordinary code content
does not blend into Markdown body text.

## Goals

- Keep Markdown prose and code-block content visually distinct.
- Apply existing syntax-theme colors to fenced blocks such as `go`, `json`,
  and `bash`.
- Preserve the current heading, fence punctuation, and language-label styles.
- Make unknown, unlabelled, and unclosed fences safe and readable.

## Rendering behavior

### Fence recognition

The Markdown structural pass will recognize both backtick and tilde fences.
It records the opening marker character and its full marker length. A closing
fence must use the same marker character and contain at least the opening
marker length, so a short marker cannot terminate a longer fence.

### Fenced body styling

For every line strictly between an opening and matching closing fence:

- Resolve the opening language label through Chroma's lexer registry,
  including lexer aliases.
- When a lexer is found, tokenize only the fenced body and map tokens through
  the existing `styleForToken` theme mapping.
- Use a code-specific base style with the existing softer syntax-text
  foreground. Identifiers and other unclassified text therefore remain
  visibly different from surrounding Markdown prose.
- When the label is empty or unknown, apply that same code base style to the
  entire body without attempting syntax tokenization.

The nested lexer path must not invoke Markdown structural styling again. A
fence labeled `markdown` is tokenized as code content only, preventing
recursive fence processing.

### Existing Markdown cues

- Opening and closing fence punctuation remains `Muted`.
- The opening language label remains bold `Accent`.
- Heading styling and all non-fenced Markdown text retain current behavior.
- An unclosed fence styles all subsequent lines as code content through the
  end of the buffer.
- Non-Markdown filenames do not receive any fence-specific style changes.

## Architecture

`internal/editor/highlight.go` remains the single owner of syntax and
Markdown styling. Its existing token-to-theme mapper will be reused by a
small helper that applies a lexer to a supplied line range and a supplied base
style. Markdown fence parsing will collect each body range and apply this
helper after the document-level Markdown lexer has produced its ordinary
style grid.

No theme configuration or new runtime dependency is required. The existing
softer syntax-text color is reused as the code-body base color.

## Tests

Add regression tests in `internal/editor/highlight_test.go` that assert:

- A `go` fence gives body identifiers a code-base foreground and maps Go
  keywords, strings, and comments through existing syntax colors.
- A `json` fence maps keys, strings, and numbers through the existing theme.
- Empty and unknown language labels color body text with the code base rather
  than Markdown prose text.
- Backtick and tilde fences honor marker length; short markers do not close a
  longer fence.
- An unclosed fence reaches end-of-buffer with code styling.
- Existing heading/fence-label styling and non-Markdown-file behavior remain
  unchanged.

## Out of scope

- Markdown preview rendering or hidden syntax characters.
- Inline-code styling changes.
- New user-configurable themes or per-language color settings.
- Markdown parsing beyond headings and fenced code blocks.
