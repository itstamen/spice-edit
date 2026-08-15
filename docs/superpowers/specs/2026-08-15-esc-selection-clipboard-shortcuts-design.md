# Esc Selection And Clipboard Shortcuts Design

## Status

- Date: 2026-08-15
- Status: Approved for implementation planning

## Goal

Add SSH-safe Esc-leader shortcuts for copying, cutting, pasting, and selecting
all text in the active SpiceEdit tab.

## Decision

Add these bindings to the existing Esc-leader table:

| Shortcut | Action |
|---|---|
| `Esc c` | Copy selection |
| `Esc x` | Cut selection |
| `Esc v` | Paste |
| `Esc a` | Select all |

`Esc c`, `Esc x`, and `Esc v` call the existing menu handlers and therefore
retain their established semantics. Copy and cut place selected text in the
editor's internal clipboard and publish it through OSC 52. Paste inserts only
the editor's internal clipboard content. System clipboard paste remains the
terminal's `Cmd+V` or right-click paste input because terminal applications do
not reliably expose secure clipboard reads, especially through SSH and tmux.

## Select All

Add an App-level `menuSelectAll` handler. It closes any open action menu,
looks up the active tab, and calls the existing `Tab.SelectAll()` primitive.
The handler provides concise status feedback and safely handles missing,
read-only, image, and placeholder tabs without mutating content.

Add **Select all** to the main action menu so the action remains available in
environments where keyboard chords are intercepted.

## Scope

The change is limited to the App leader table, menu entries and handlers,
same-package tests, and README hotkey documentation. It does not add host
clipboard-reading support, change OSC 52 behavior, or introduce Ctrl-based
shortcuts.

## Testing

- Assert each new Esc-leader binding resolves to the intended App method.
- Assert `Esc a` selects the entire active text buffer and does not edit it.
- Assert menu layout includes Select all under the existing text-edit actions.
- Preserve existing copy/cut/paste tests and validate no-active-tab and
  unsupported-tab safeguards.
- Run focused App tests, `make test`, and `make build`.
