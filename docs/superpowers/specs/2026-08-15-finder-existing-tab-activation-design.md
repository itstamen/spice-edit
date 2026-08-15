# Finder Existing Tab Activation Design

## Status

- Date: 2026-08-15
- Status: Approved for implementation planning

## Goal

Make `Esc e` activate an existing macOS Finder tab when it already displays
the active file's containing folder. Search every open Finder window, preferring
the frontmost window. Only create a new tab when no existing tab displays the
target folder.

## Problem

The current Darwin reveal adapter always creates a new Finder tab whenever any
Finder window exists. Repeated reveals of the same folder therefore create
duplicate tabs and make it harder to return to the already open location.

Finder's AppleScript dictionary exposes the target of the active tab in a
Finder window, but does not expose a collection of tabs. A correct
full-path match must consequently use Accessibility UI automation to activate
each tab before reading its target through Finder scripting. Matching tab titles
is not sufficient because distinct folders can share a name and Finder can
truncate or localize titles.

## Decision

Continue to use the Darwin-only `osascript` adapter and pass the target folder
as a literal argument. The script will perform the following steps:

1. Resolve an active-file request to its parent folder; keep a root-fallback
   directory unchanged.
2. Activate Finder. If Finder has no open windows, create one at the target
   folder and stop.
3. Traverse Finder windows in front-to-back order. Within each window,
   traverse its visible Finder tabs in order through System Events.
4. For each tab, activate it, ask Finder for that window's active target, and
   compare the resolved target with the requested directory.
5. On the first match, activate the containing Finder window and leave the
   matching tab selected. Do not create a new tab.
6. When no tab matches, make the front Finder window active, send `Cmd+T`
   through System Events, and set the new tab's Finder target to the requested
   folder.

When the target folder is open in more than one tab, the first match in the
frontmost Finder window wins. If the frontmost window has no match, the first
match in the next frontmost window wins.

## Permissions And Errors

Traversing and selecting Finder tabs uses System Events, so the terminal
application that launches SpiceEdit requires Accessibility permission. Existing
authorization normalization must continue to show an actionable message that
names Accessibility permission. Finder or System Events errors must return to
the shared asynchronous reveal completion path without blocking the editor UI.

## Scope

The change is limited to the Darwin reveal adapter, its deterministic tests,
the Finder reveal documentation, and macOS manual validation.

It does not change Finder settings, Windows Explorer behavior, Linux support,
or the shared reveal contract. It does not add a native macOS helper binary.

## Testing

- Unit-test any Go helpers that prepare a literal target argument and recognize
  accessibility errors.
- Add deterministic coverage for the AppleScript branch contract: no-window
  creation, existing-tab traversal before new-tab creation, and reuse of the
  first matching tab.
- Run `gofmt`, `make test`, and `make build`.
- Manually verify on macOS with Accessibility permission enabled:
  - an existing target tab in a background Finder window becomes active and
    frontmost without creating a tab;
  - duplicate target tabs prefer the frontmost Finder window;
  - no existing target tab creates exactly one new tab;
  - no Finder windows creates one window;
  - a denied permission leaves the editor usable and displays the guidance.
