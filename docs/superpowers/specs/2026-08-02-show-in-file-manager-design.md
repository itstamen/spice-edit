# Show in File Manager Design

## Document information

- **Date:** 2026-08-02
- **Status:** Proposed
- **Related design:** `docs/superpowers/specs/2026-08-02-default-application-open-design.md`
- **Selected approach:** Use native macOS and Windows file-manager reveal mechanisms behind an asynchronous app adapter.

## 1. Goal

Add `Esc e` and a main-menu action that shows the active file in the host operating system's file manager. Finder and Windows Explorer must open the containing directory and select the active file. When no file-backed tab is active, the action opens the project root directory.

## 2. Scope

The action supports:

- macOS Finder;
- Windows Explorer;
- editable text tabs;
- inline image tabs;
- read-only external tabs;
- no-tab and untitled-tab states through the project-root fallback.

Linux and other operating systems do not launch a file manager. They keep the shortcut and menu action visible and report that the platform is unsupported.

## 3. User interaction

The File actions menu adds:

```text
Show in file manager    Esc e
```

`Esc e` and the menu execute the same action. The action does not show a confirmation dialog because both entry points are explicit commands.

Target selection follows one rule:

1. If the active tab has a non-empty path, show and select that file.
2. Otherwise, open `App.rootDir` as a directory without selecting a child.

The fallback covers no open tabs and unsaved `untitled` tabs. It does not use the sidebar's active folder.

## 4. Architecture

The implementation adds a dedicated reveal subsystem under `internal/app`:

- `reveal.go` owns target resolution, asynchronous execution, event delivery, status messages, and the injectable test seam.
- `reveal_darwin.go` invokes Finder.
- `reveal_windows.go` invokes native Windows Shell APIs.
- `reveal_other.go` returns an unsupported-platform error for Linux and every other platform.

The adapter signature is:

```go
type pathRevealer func(path string, selectFile bool) error
```

`selectFile` is true for a file-backed active tab and false for the project-root fallback. `App` stores an optional `revealPath pathRevealer`; production uses `platformRevealPath`, while tests inject a recorder.

The existing default-application opener remains separate. Opening a file and revealing it in a file manager have different target semantics, status copy, and Windows APIs.

## 5. Data flow

```text
Esc e / main menu
        |
        v
resolve reveal target
   /              \
active file      no file path
path, select     rootDir, open
   \              /
        v
async platformRevealPath
        |
        v
revealDoneEvent on tcell queue
        |
        v
status-bar success or failure
```

The action never saves buffers, changes the active tab, changes the sidebar's active folder, or mutates undo history.

## 6. Platform behavior

### 6.1 macOS

- File target: run `open -R <absolute-file-path>` so Finder reveals and selects the file.
- Project-root fallback: run `open <absolute-directory-path>` so Finder opens that directory.
- Pass every path as one literal argument without shell parsing.

### 6.2 Windows

- File target: convert the absolute path to a Shell item identifier and call `SHOpenFolderAndSelectItems` so Explorer opens the containing folder and selects the file.
- Initialize COM for the goroutine before using Shell APIs and release the item identifier after the call.
- Project-root fallback: reuse the existing `ShellExecuteW` default-open path for the directory.
- Use UTF-16 APIs throughout so spaces, Unicode, percent signs, quotes, and shell metacharacters remain literal path data.

### 6.3 Linux and other systems

`platformRevealPath` returns `show in file manager is not supported on <goos>`. The app reports the error in the status bar and does not fall back to `xdg-open` or `gio`.

## 7. Asynchronous result handling

`reveal.go` defines:

```go
type revealDoneEvent struct {
    when       time.Time
    path       string
    selectFile bool
    err        error
}
```

The app validates and resolves the target to an absolute path, captures the selected revealer and screen, then runs the platform call in a goroutine. The goroutine posts `revealDoneEvent`; only the main event loop updates status state.

Success messages are:

- file target: `Revealed <name> in file manager`;
- project root: `Opened <name> in file manager`.

Failures use `Could not show <name> in file manager: <reason>`.

## 8. Menu and leader integration

`leaderBindings` maps `e` to `menuShowInFileManager`. The menu row remains visible and enabled because a project-root target always exists. On unsupported platforms, invoking either entry point reports the platform error.

The action sits beside **Open with default app** in the File actions group. No `Ctrl+` shortcut is added.

## 9. Error handling

- If the selected path no longer exists, report the stat error and do not call the platform adapter.
- If the project root is empty or unavailable, report `No path to show in file manager`.
- Platform and Shell API failures return through `revealDoneEvent`.
- A failed reveal leaves tabs, buffers, dirty state, and selection unchanged.

## 10. Testing

App tests cover:

- active text, image, and external tabs pass their absolute path with `selectFile=true`;
- no-tab and untitled-tab states pass the project root with `selectFile=false`;
- the operation runs asynchronously and posts success or failure through the tcell event queue;
- `Esc e` and the menu call the same action;
- reveal does not save or clear a dirty text buffer;
- missing targets fail before the platform adapter runs;
- macOS command construction uses `open -R` for files and `open` for directories;
- unsupported platforms return the required error;
- Windows code cross-compiles with its COM and Shell API calls.

Run `make test`, `make build`, `make build-linux`, a Windows app-test cross-compilation, and a Windows binary cross-compilation.

## 11. Documentation

Update README and website hotkey documentation with:

- `Esc e` and **Show in file manager**;
- Finder and Explorer file-selection behavior;
- project-root fallback when no file-backed tab is active;
- explicit lack of Linux support;
- host-local behavior during SSH sessions.

## 12. Non-goals

- Do not add Linux `xdg-open` or `gio` support.
- Do not reveal the sidebar's active folder.
- Do not add a confirmation dialog.
- Do not let users configure a custom file manager.
- Do not open the active file itself.
- Do not change the existing `Esc o` behavior.
