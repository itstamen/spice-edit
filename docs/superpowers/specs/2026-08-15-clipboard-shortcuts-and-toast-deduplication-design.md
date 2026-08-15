# Clipboard Shortcuts And Toast Deduplication Design

## Status

- Date: 2026-08-15
- Status: Approved for implementation planning

## Goal

Eliminate duplicate copy notifications in Herdr while making clipboard actions and Select All available through the existing Esc leader and visibly documented in the action menu.

## Context

A successful text copy already writes OSC 52 to the terminal. In Herdr, that write produces Herdr's own `copied to clipboard` notification. SpiceEdit also draws its custom `Copied to clipboard` toast, so users see two overlays for one mouse selection or double-click word selection.

The editor already has menu actions for Copy selection, Cut selection, and Paste. `editor.Tab.SelectAll()` already selects the complete editable buffer. The Esc leader table intentionally omitted `c`, `x`, and `v`; that decision is now superseded by this requirement.

## Decision

Remove SpiceEdit's custom copy-toast feature entirely. Herdr's native OSC 52 feedback is the sole visual confirmation where Herdr is in use. SpiceEdit retains its existing status-bar feedback, especially `Copied (system clipboard unavailable)` when the OSC 52 write fails.

Add the following Esc leader shortcuts and show each in the corresponding menu row:

| Menu action | Shortcut | Behavior |
| --- | --- | --- |
| Copy selection | `Esc c` | Copy the active editable text selection through the existing internal clipboard and OSC 52 path. |
| Cut selection | `Esc x` | Copy the active editable text selection through the existing path, then delete it. |
| Paste | `Esc v` | Insert SpiceEdit's internal clipboard at the active editable text cursor. |
| Select all | `Esc a` | Select the full active editable text buffer only. It does not copy or change the clipboard. |

## Architecture

### Remove Local Toast State

Delete only the App-local toast mechanism:

- `copyToastUntil`;
- copy-toast constants;
- `copyToastExpiredEvent`;
- scheduling and expiry handling;
- `copyToastRect` and `drawCopyToast`;
- draw-loop routing for the toast;
- toast-specific tests.

Do not change `clipboard.CopyToSystem`, the OSC 52 sequence, or Herdr. Do not add terminal-environment detection. Do not suppress Herdr's native notification.

`copySelection` continues to update `clipBuf`, attempt the system clipboard write, flash `Copied` on success, and flash the exact unavailable message on failure. Mouse selection release continues to auto-copy once, but no longer owns visual overlay state or timers.

### Menu And Leader Actions

Use existing `menuCopy`, `menuCut`, and `menuPaste` methods. Add the matching `shortcut` strings to their `menuItemDef` entries and add `c`, `x`, and `v` bindings to `leaderBindings()`.

Add `menuSelectAll` in the same menu-action section. It closes the menu, obtains the active tab, returns for no tab or read-only tab, and calls `tab.SelectAll()`. Add a `Select all` menu row with `Esc a`, enabled only for an editable text tab. Add `a` to `leaderBindings()`.

Use a narrow existing-style enable predicate for editable text, rather than making Select All visible but permanently disabled for image/external tabs. The action must remain safe when invoked through the leader despite a state transition.

## User Experience

- In Herdr, successful mouse drag and double-click selection produce one native Herdr copy notification, not a second SpiceEdit overlay.
- A successful menu or `Esc c` copy retains the current status-bar `Copied` feedback.
- The action menu displays the four shortcuts in the existing right-aligned shortcut column.
- `Esc a` selects all text in an editable tab. It neither writes OSC 52 nor changes `clipBuf`.
- On image, external-file, empty-editor, or otherwise read-only states, `Esc a` is a no-op and the menu row is disabled.
- Existing menu navigation, double-Esc menu opening, and unbound leader-key fallthrough remain unchanged.

## Scope

Modify only:

- `internal/app/app.go`;
- `internal/app/leader.go`;
- same-package App/leader tests;
- `README.md` if it still claims SpiceEdit shows a confirmation toast.

This does not rework clipboard transport, read system clipboards, add Ctrl shortcuts, modify Herdr, or change automatic mouse-selection copying itself.

## Testing

- Assert no App-local toast state, event, renderer, or timer remains.
- Simulate mouse drag and double-click selection with a successful clipboard writer; assert exactly one system-copy attempt and no secondary local overlay state.
- Preserve existing system-write failure coverage and exact unavailable status message.
- Assert menu labels expose `Esc c`, `Esc x`, `Esc v`, and `Esc a`.
- Assert each leader binding invokes its corresponding menu action.
- Assert `Esc a` selects the full editable buffer without writing or changing the internal clipboard.
- Assert `Esc a` is safe and inert with no tab and read-only image/external tabs.
- Run focused App/leader tests, `make test`, and `make build`.

