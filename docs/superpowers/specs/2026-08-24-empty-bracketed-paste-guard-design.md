# Empty Bracketed Paste Guard Design

## Document information

- **Date:** 2026-08-24
- **Status:** Approved for implementation

## Problem

On macOS with a Chinese input method active, pressing Control can produce an
empty or non-text bracketed-paste sequence. SpiceEdit currently treats every
paste end marker as a request to read pbpaste, so the current system clipboard
is inserted despite no actual paste text arriving.

## Design

- Paste capture tracks whether it received at least one accepted text event:
  rune, Enter/CR, LF, or Tab.
- beginPaste resets both the text buffer and that marker.
- finishPaste returns immediately when the marker is false. It does not read
  the system clipboard, route text, alter selection, or change a modal input.
- When text was captured, existing behavior is preserved: prefer the macOS
  system clipboard and fall back to captured terminal text if the read fails.

## Tests

- Empty paste start/end does not call the system reader and leaves buffer,
  cursor, selection, and modal state unchanged.
- A paste containing only ignored control/navigation events behaves the same.
- A paste containing text still reads the system clipboard and inserts it.

## Out of scope

- Changes to clipboard source precedence, IME configuration, or ordinary
  non-bracketed key handling.
