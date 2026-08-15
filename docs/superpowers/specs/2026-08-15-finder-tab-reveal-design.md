# Finder Tab Reveal Design

## Status

- Date: 2026-08-15
- Status: Approved for implementation planning

## Goal

Make `Esc e` open the active file's containing folder in a new tab of an
existing macOS Finder window. When Finder has no open windows, open one Finder
window at that folder.

## Problem

The current Darwin adapter uses `open -R <file>`. Replacing it with
`open -a Finder <folder>` still opens a separate Finder window even when Finder
is configured to prefer tabs. The `open` command does not reliably apply
Finder's tab preference to externally requested folders.

## Decision

Use a Darwin-only `osascript` adapter. It receives the target folder as one
literal argument rather than interpolating paths into AppleScript.

1. For an active file, resolve the target to its parent directory. For a
   root fallback, use the supplied directory unchanged.
2. Activate Finder and inspect its open-window count.
3. With no Finder windows, create one window at the target folder.
4. With an existing Finder window, use System Events to send `Cmd+T` to the
   Finder process, then set the active Finder tab's target to the folder.

Finder's script dictionary does not expose a direct create-tab command; the
System Events key action is therefore required to guarantee the requested
tab behavior.

## Permissions and Errors

The existing Finder window path requires macOS Accessibility permission for
the process launching SpiceEdit, usually the terminal emulator. The adapter
must turn authorization failures into an actionable error naming Accessibility
permission. `revealPathInFileManager` continues to post that error to the main
event loop and keeps the editor usable.

## Platform Behaviour

- macOS: active files open their parent folder in a Finder tab; no Finder
  window opens one window instead.
- Windows: retain `SHOpenFolderAndSelectItems` file selection behavior.
- Other platforms: retain the unsupported-platform error.

## Testing

- Extract the AppleScript text and `osascript` argument construction into
  deterministic Darwin helpers.
- Test parent-directory selection for file targets, direct use of directory
  targets, literal path arguments containing spaces and shell metacharacters,
  and the Accessibility-error normalization helper.
- Keep shared reveal tests unchanged because the App still passes
  `selectFile=true` for an active tab.

## Out of Scope

- Changing macOS Finder settings.
- UI automation on Windows or unsupported platforms.
- Selecting the source file in the Finder tab after opening its parent folder.
