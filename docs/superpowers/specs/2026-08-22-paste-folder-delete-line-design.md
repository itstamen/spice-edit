# Paste, Folder Creation, and Delete-Line Design

## Document information

- **Date:** 2026-08-22
- **Status:** Approved for implementation

## Summary

SpiceEdit will preserve line breaks when text is pasted through either the
terminal clipboard path or the editor's internal clipboard. The main menu will
gain a `New folder` action, and `Esc+d` will delete the current line or every
line touched by an active selection.

## Goals

- Insert multi-line pasted content as multi-line buffer text for both paste
  paths.
- Keep a paste as one structural undo operation and avoid rendering each pasted
  character as a separate edit.
- Create folders from the main menu in the current active directory without a
  shortcut.
- Provide predictable delete-line behavior for a cursor and for a selection.

## Paste handling

### Canonical newline representation

All pasted text passes through one internal normalization helper before it
reaches `editor.Tab.InsertString`:

- Convert CRLF (`\r\n`) to LF (`\n`).
- Convert remaining CR (`\r`) to LF (`\n`).
- Preserve all other text unchanged.

The editor buffer continues to use LF as its single line separator. This helper
is used for both internal clipboard content (`Esc+v` and the `Paste` menu row)
and terminal-delivered paste content.

### Bracketed terminal paste

`internal/app` will handle tcell `EventPaste` boundaries. On a start event it
enters a paste-capture state. During that state it translates paste text key
events into text, including Enter and Tab, without running ordinary keyboard,
leader-key, menu, or editing actions. On the matching end event it normalizes
the captured text and inserts it once into the active editable tab.

This makes a bracketed multi-line paste one buffer insertion and one structural
undo step. It also prevents pasted `Esc`-like content from being interpreted as
an editor command. Empty captures and read-only/no-tab targets are safe no-ops.

Terminals that do not emit bracketed-paste boundaries retain the normal input
path. Ordinary Enter remains an immediate newline edit as today.

## New folder menu action

The File actions section of the main `Esc Esc` menu will add `New folder`, with
no Esc-leader shortcut.

Its target directory follows the existing `New file` contract:

- Use `activeFolder`, falling back to `rootDir` when it is unset or no longer a
  directory.
- Display the resolved target folder in the prompt hint.
- Accept a relative path such as `assets/icons`; create intermediate folders as
  needed.
- Reject an empty name, invalid paths, and filesystem failures through the
  existing status/prompt error presentation.
- Refresh the tree and expand the target path so the created folder is visible.

No context-menu folder creation is added in this change.

## Delete current line

Add an editor-level `DeleteCurrentLines` operation and bind it to `Esc+d`. The
main menu will expose the same action as `Delete current line` with the `Esc d`
shortcut.

- Without a selection, delete the cursor's logical line.
- With a selection, delete every logical line touched by the inclusive
  selection range, regardless of selection direction.
- Deleting the final remaining line leaves one empty line in the buffer.
- Collapse cursor and anchor to the first deleted line at column zero, clamped
  to the remaining buffer.
- Mark the tab dirty, invalidate text-derived rendering/cache state, and record
  exactly one structural undo snapshot.
- Read-only tabs are unchanged.

## Tests

Add focused regression coverage for:

- Internal clipboard paste with LF, CRLF, and CR newlines.
- Bracketed terminal paste containing multiple lines, tabs, and text that could
  otherwise look like a leader sequence; verify one insertion and one Undo.
- Existing ordinary Enter behavior outside a paste capture.
- Folder creation in the active folder, nested-path creation, invalid target
  fallback, failure reporting, and tree refresh/expansion behavior.
- Delete one line, delete every line touched by forward and reverse selections,
  preserve one empty buffer line, reject edits for read-only tabs, and restore
  the exact prior buffer/cursor state with Undo.

## Out of scope

- Reading the macOS system clipboard directly from the TUI.
- New keyboard shortcuts for folder creation.
- Context-menu folder creation.
- Changes to clipboard copy/cut, selection toast behavior, or image previews.
