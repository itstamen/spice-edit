# Finder Active Folder Reveal Design

## Document Information

- **Date:** 2026-08-24
- **Status:** Approved for implementation planning

## Problem

In project mode, pressing `Esc+e` ignores the directory most recently selected
in the left file tree. `showInFileManager` always prefers the active file tab,
so a user must open a concrete file before Finder opens the expected project
directory. Clicking a directory node updates `App.activeFolder`, but the reveal
action does not consult that existing context.

## Desired Behavior

- In project mode, `Esc+e` opens the current `activeFolder` in Finder.
- Clicking a directory node makes that directory the reveal target even when a
  file tab remains active on the right.
- Clicking the project-root row makes the project root the reveal target.
- Clicking a file node makes its parent directory the active folder and reveal
  target, matching existing `activeFolder` behavior.
- The selected tree context remains authoritative until another directory or
  file node updates it.
- In single-file mode, where there is no file tree, the active file remains the
  reveal target with file-selection semantics.
- If project context is unavailable, the action falls back to the active file,
  then to the project root.

## Approach

Reuse `App.activeFolder`; do not introduce a second tree-selection field.
Directory, root, and file-node clicks already update this field through
`setActiveFolder`, and file opening already sets it to the file's parent.

Extract target selection into a small deterministic helper returning the path
and `selectFile` flag:

1. When a file tree exists and `activeFolder` is non-empty, return
   `activeFolder` with `selectFile=false`.
2. Otherwise, when an active file-backed tab exists, return its path with
   `selectFile=true`.
3. Otherwise, return `rootDir` with `selectFile=false`.

`showInFileManager` delegates to this helper and keeps the existing asynchronous
validation, platform adapter, and status handling unchanged.

## Components

### Target Resolution

`internal/app/reveal.go` owns the new private target helper. It depends only on
existing App state and can be tested without launching Finder.

### Finder Integration

`internal/app/reveal_darwin.go` remains unchanged. The intermittent Finder
"Recents" tab race is not reproducible in the current session and is separate
from choosing the correct SpiceEdit directory context.

### Documentation

`README.md` documents that `Esc+e` follows the selected project directory in
project mode and uses the current file in single-file mode.

## Tests

Application tests cover:

- Project mode prefers `activeFolder` even with an active file tab.
- A project-root active folder reveals the root as a directory.
- A file-node context reveals the file's parent directory.
- Single-file mode without a tree preserves the active file path and
  `selectFile=true`.
- Empty project context falls back to the active file, then the project root.
- Existing dirty-buffer and asynchronous success/failure behavior remains
  unchanged.

## Error Handling

The existing `revealPathInFileManager` path validation remains authoritative.
Missing or inaccessible targets produce the current status error and do not
launch Finder. No silent fallback is added after a selected directory becomes
invalid, because that would open an unrelated location instead of surfacing the
stale context.

## Out Of Scope

- Changing Finder tab creation, reuse, or AppleScript timing.
- Fixing the currently non-reproducible "Recents" tab race.
- Changing directory-tree selection, expansion, highlighting, or file opening.
- Changing Windows or non-macOS platform adapters.
