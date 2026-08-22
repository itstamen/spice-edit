# System Clipboard Paste Design

## Document information

- **Date:** 2026-08-22
- **Status:** Approved for implementation

## Summary

SpiceEdit will use the current macOS system clipboard as the source for menu
Paste and `Esc+v`, rather than its potentially stale internal clipboard. A
bracketed terminal paste initiated by Command+V will also prefer the same
system clipboard content, with the captured terminal payload retained only as
a compatibility fallback when the local system clipboard cannot be read.

## Goals

- Paste the same text currently copied in another macOS application.
- Prevent menu Paste and `Esc+v` from inserting stale `clipBuf` content.
- Keep terminal Command+V usable over SSH and on systems where local clipboard
  access is unavailable.
- Preserve the existing newline normalization and one-step undo behavior.

## Clipboard source behavior

### System reader

`internal/clipboard` will provide a read operation for the host clipboard. On
macOS it uses `pbpaste` and returns its text verbatim. The App will hold an
injectable reader function so tests can provide exact content or errors without
calling a host subprocess.

### Menu Paste and Esc+v

The Paste menu action and `Esc+v` leader action will read the macOS system
clipboard at the moment the command is invoked.

- A successful read passes the result through the existing newline normalizer
  and one shared insertion path.
- A read failure flashes an actionable system-clipboard error and leaves the
  buffer unchanged.
- These explicit commands never fall back to `clipBuf`; stale editor-local
  content must not be inserted.
- Paste remains enabled for every writable text tab because clipboard
  availability is known only when the command runs.

### Command+V terminal paste

Bracketed paste remains enabled. At its end boundary, SpiceEdit first attempts
to read the local system clipboard:

- On success, insert the system clipboard text and ignore the captured terminal
  payload.
- On read failure, insert the captured terminal payload, preserving terminal
  and SSH compatibility.
- Both paths normalize CRLF and CR to LF and produce one structural undo step.

The existing capture behavior continues to prevent pasted escape sequences or
navigation keys from triggering editor commands. Text-routing behavior for
open modals remains unchanged.

## Internal clipboard compatibility

Copy and Cut continue to update `clipBuf` and publish selected text through
the existing system-copy path. `clipBuf` remains available for current copy
state and compatibility tests, but it is no longer the source for explicit
Paste commands.

## Tests

Add focused tests that verify:

- Menu Paste and `Esc+v` insert injected system clipboard content even when
  `clipBuf` contains different stale text.
- A system-reader failure leaves the buffer unchanged and does not use
  `clipBuf`.
- Bracketed terminal paste prefers injected system clipboard content, but uses
  its captured payload when the reader fails.
- All sources preserve multiline LF output from CRLF/CR input and undo as one
  insertion.
- Read-only and no-tab targets remain safe no-ops.
- The Paste menu row is enabled for writable text tabs independently of
  `clipBuf`.

## Out of scope

- Clipboard history or multiple clipboard formats.
- Clipboard reads from non-macOS host APIs beyond terminal-paste fallback.
- Changing copy/cut shortcuts, OSC 52 output, or modal text paste behavior.
