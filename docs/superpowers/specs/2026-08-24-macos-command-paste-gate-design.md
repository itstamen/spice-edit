# macOS Command Paste Gate Design

## Document Information

- **Date:** 2026-08-24
- **Status:** Approved for implementation

## Problem

On macOS, a Chinese input method can emit a bracketed-paste sequence while the
user presses Control to switch input methods. Terminal bracketed-paste markers
do not identify their origin, so SpiceEdit must not treat every such sequence
as an explicit paste request. In particular, it must never read `pbpaste` in
response to a terminal paste event, because that inserts unrelated clipboard
content.

## Goals

- Insert text only after one of three explicit requests: physical `Command+V`,
  `Esc+v`, or the Paste menu action.
- Keep `Esc+v` and the Paste menu action backed by the current macOS system
  clipboard.
- Keep `Command+V` usable in a terminal while rejecting bracketed-paste events
  that are not preceded by that physical shortcut.
- Do not suppress or modify keyboard input destined for other applications.
- Preserve existing newline normalization, modal routing, and one-step undo
  behavior for accepted terminal payloads.

## Design

### Explicit clipboard commands

The Paste menu action and `Esc+v` remain the only app commands that call the
injectable system-clipboard reader. They insert the returned text through the
existing shared paste insertion path. A reader failure flashes the existing
unavailable message and changes no text.

### Native Command+V signal

On macOS, a small CoreGraphics listener observes keyboard key-down events in
listen-only mode. When it sees physical V with the Command modifier, it stores
the monotonic observation time in an atomic value. It neither consumes the
event nor writes clipboard content. Apple requires event-listening permission
for this facility; when unavailable, SpiceEdit leaves `Command+V` disabled and
continues to support the two explicit in-app paste commands.

The listener is deliberately narrow: it records only Command+V and exposes a
testable `RecentCommandPaste(now time.Time) bool` query. The acceptance window
is short and defined in one named constant so that the terminal's immediate
bracketed payload is accepted while delayed or unrelated input is rejected.

### Terminal paste gate

Bracketed paste stays enabled to ensure terminals preserve pasted text as one
payload instead of dispatching its characters as commands. At the start marker,
SpiceEdit snapshots whether the native Command+V signal is recent. At the end
marker:

- If the snapshot is false, discard the captured payload without routing it,
  reading the system clipboard, changing selection, or changing modal state.
- If the snapshot is true, route only the captured terminal payload. Never
  call the system-clipboard reader from this path.
- Empty accepted payloads are still no-ops.

This is intentionally a source gate, not an IME heuristic. The input method's
Control sequence lacks the native Command+V signal, so its terminal paste
payload cannot modify the editor.

### Platform boundaries and lifecycle

The macOS implementation may use cgo with ApplicationServices/CoreGraphics.
It runs its callback source on a dedicated run loop and provides a close method
that stops the source before application shutdown. Non-macOS builds provide a
same-shaped no-op implementation returning false, so no platform-specific API
leaks into the app event loop.

App construction owns the listener. The app stores its query function behind a
small injectable seam for tests. Shutdown closes the listener after the event
loop ends. A permission failure is surfaced once through the status line rather
than repeatedly while the user types.

## Tests

- A bracketed terminal payload with no recent Command+V signal is discarded;
  the buffer, cursor, selection, modal value, and system-reader call count are
  unchanged.
- A recent Command+V signal accepts a captured multiline terminal payload,
  normalizes CRLF/CR to LF, and makes no system-reader call.
- An empty accepted payload is a no-op.
- Menu Paste and `Esc+v` still read injected system clipboard text and do not
  depend on the native listener.
- The native-listener abstraction reports false on unsupported platforms and
  can be deterministically driven by its unit tests without requesting macOS
  permission.
- Existing non-paste keyboard handling and modal paste routing stay covered by
  focused `internal/app` tests.

## Out of Scope

- Clipboard history, multi-format clipboard support, or changing copy/cut.
- Capturing or suppressing global shortcuts outside SpiceEdit.
- Making `Command+V` work on macOS when event-listening permission is denied.
