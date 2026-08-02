# Default Application Open Design

## Document information

- **Date:** 2026-08-02
- **Status:** Approved
- **Related PRD:** `docs/superpowers/specs/2026-08-01-soft-wrap-prd.md`, version 0.5
- **Selected approach:** Add a third, read-only tab mode for unsupported files.
- **Follow-up:** Left file-tree clicks now require confirmation as specified in `docs/superpowers/specs/2026-08-02-tree-external-open-confirmation-design.md`.

## 1. Goal

SpiceEdit must edit safe text files, preview supported images, and hand every other file to the operating system's default application. Unsupported files remain represented inside SpiceEdit by a read-only tab that displays `编辑器只支持文本及图片文件在线编辑.`.

Users can also open any file-backed text, image, or unsupported tab with the default application through `Esc o` or the main `≡` menu. External opening must keep the terminal UI responsive on macOS, native Windows, and Linux.

## 2. Non-goals

- Do not edit or render PDFs, office documents, archives, databases, audio, video, or executables inline.
- Do not add an application chooser or custom opener configuration.
- Do not save dirty text buffers before opening their on-disk file externally.
- Do not transfer remote files from an SSH host to the SSH client desktop.
- Do not add image codecs beyond those supported by the existing image-preview subsystem.

## 3. Architecture

The implementation has three units with separate responsibilities:

1. `internal/editor/filekind.go` classifies a path as text, image, or unsupported before `NewTab` reads the whole file.
2. `internal/editor/external.go` defines the unsupported tab mode and renders its placeholder.
3. `internal/app/defaultopen.go` owns asynchronous external opening, event delivery, status messages, and the menu/leader action. Platform-specific files implement the actual handoff.

The editor package never launches processes. The app package never parses file bytes or decides whether a file is safe to edit.

```text
file tree / finder / command-line path
                  |
                  v
             App.openFile
                  |
                  v
          editor.NewTab(path)
                  |
                  v
          bounded classification
           /        |        \
          v         v         v
       text tab  image tab  external tab
                              |
                              v
                  async default-app handoff
```

## 4. File classification

### 4.1 Result type

`internal/editor/filekind.go` defines an unexported enum:

```go
type fileKind uint8

const (
    fileKindText fileKind = iota
    fileKindImage
    fileKindExternal
)
```

`classifyFile(path string) (fileKind, error)` applies the rules below. `NewTab` remains the only public constructor and keeps its current signature.

### 4.2 Classification order

1. An empty path or a missing path is text. This preserves untitled tabs and the New File workflow.
2. Existing paths must be regular, readable files. Classification reads at most an 8 KiB sample and returns the read/stat error when it cannot inspect the file.
3. A recognised inline-image extension (`.png`, `.jpg`, `.jpeg`, or `.gif`, case-insensitive) is an image candidate.
4. A known external-format extension is unsupported even when its header happens to be valid UTF-8. The initial set covers PDF and office documents; archives; databases; audio/video; compiled objects, libraries, and executables; fonts; and unsupported bitmap formats such as WebP, BMP, TIFF, ICO, and HEIC.
5. A NUL byte or invalid UTF-8 marks every remaining file unsupported, including files with text-looking extensions.
6. A valid UTF-8 sample without NUL bytes is text. This includes unknown extensions and extensionless scripts or configuration files.
7. An image candidate that fails decoding becomes unsupported. It never falls through to text.

The 8 KiB limit prevents classification from doubling the memory cost of large files. `NewTab` reads the complete file only after classification returns text.

### 4.3 Error handling

Permission errors, directory paths, and other failed reads remain open errors. They do not become unsupported tabs because SpiceEdit cannot prove the path is a readable local file.

## 5. Unsupported tab mode

### 5.1 Tab state

`internal/editor/external.go` defines `externalMode = "external"`. `newExternalTab(path)` creates a `Tab` with:

- the absolute file path;
- an empty buffer that contains no bytes from the unsupported file;
- the file's modification time when available;
- `Mode` set to `externalMode`;
- normal undo initialization so shared tab lifecycle code remains safe.

`Tab` adds:

```go
func (t *Tab) IsExternal() bool
func (t *Tab) IsReadOnly() bool
```

`IsReadOnly` returns true for image and external tabs. Existing image-only editing guards change to `IsReadOnly` where the intent is "editable text only." Image rendering and terminal-graphics checks continue to use `IsImage`.

### 5.2 Rendering

`RenderSoftWrap` dispatches external tabs before text layout. `RenderExternal` fills the editor rectangle with the theme background, centers the exact message `编辑器只支持文本及图片文件在线编辑.`, and hides the cursor.

The status bar identifies the tab as an externally opened file and includes its basename. It does not display line, column, line count, dirty state, or image dimensions.

### 5.3 Allowed and blocked actions

External tabs support:

- tab switching and closing;
- Rename file and Delete file;
- Copy relative path and Copy absolute path;
- `Esc o` and `Open with default app`.

External tabs block:

- keyboard and mouse editing;
- Save and Save & close;
- Undo, Redo, and Revert;
- Find in file;
- selection, clipboard mutation, formatting, line comments, and soft-wrap behavior;
- git line-gutter loading and text-buffer disk reconciliation.

If a periodic refresh finds the path missing, the tab may show a status warning, but it must not become dirty or savable.

## 6. Open flow

`App.openFile` remains the shared entry point for file-tree clicks, finder results, and command-line paths.

For a new path:

1. Resolve the absolute path and update the active folder/tree reveal state.
2. Call `editor.NewTab`.
3. Append and activate the returned tab.
4. Load git gutter data only for editable text tabs.
5. If the tab is external, call `openActivePathWithDefaultApp` asynchronously; otherwise flash the existing opened-file message.

For an already-open path:

1. Activate the existing tab.
2. Refresh git gutter data only for editable text tabs.
3. If the existing tab is external, invoke the default application again.
4. Never create a duplicate tab.

A supported text or image file never launches an external application merely because the user selected it in the tree.

## 7. Default-application adapter

### 7.1 Common app interface

`internal/app/defaultopen.go` defines:

```go
type pathOpener func(string) error

type defaultOpenDoneEvent struct {
    when time.Time
    path string
    err  error
}
```

`App` has an optional `openPath pathOpener` field. Production code uses `platformOpenPath` when the field is nil. Tests inject a recorder or controlled error.

`openPathWithDefaultApp(path)` validates the path, copies the selected opener and screen references, and runs the opener in a goroutine. The goroutine posts `defaultOpenDoneEvent`; the main event loop alone updates status state.

Success flashes `Opened <name> with default app`. Failure flashes `Could not open <name>: <reason>`. A failed launch leaves the active tab and editor state unchanged.

### 7.2 Platform implementations

- **macOS:** `exec.Command("open", path).Run()`.
- **Linux:** use `exec.LookPath("xdg-open")`; if absent, try `gio open`; return a clear error when neither command exists.
- **Windows:** call the native `ShellExecuteW` file association API in a `_windows.go` file. This avoids `cmd.exe` parsing and preserves spaces, Unicode, percent signs, quotes, and shell metacharacters in paths.
- **Other platforms:** a build-tagged fallback returns an unsupported-platform error.

Platform files expose the same `platformOpenPath(string) error` function. No platform-specific code enters `app.go`.

### 7.3 Dirty text tabs

`Esc o` opens the file currently stored on disk. It does not save, export, or clear the dirty buffer. The action leaves undo history and dirty state untouched.

## 8. Menu and leader integration

The File actions group adds:

```text
Open with default app    Esc o
```

The row is enabled when the active tab has a non-empty path. `leaderBindings` maps `o` to `menuOpenWithDefaultApp`. Both entry points call the same method.

When no file-backed tab exists, the menu row is disabled. A direct `Esc o` invocation flashes `No file to open with default app`.

## 9. Testing

### 9.1 Editor tests

- known and unknown valid UTF-8 files open as text;
- NUL and invalid UTF-8 files open as external tabs even with `.txt` extensions;
- empty and missing paths remain text;
- supported images remain image tabs;
- corrupt supported-image files become external tabs;
- external tabs reject Save and text mutation;
- external rendering displays the exact Chinese message and hides the cursor.

### 9.2 App tests

- file-tree/finder/command-line paths reuse the same classification path;
- opening an external file creates one tab and invokes the injected opener;
- reopening the same external file re-invokes the opener without duplicating the tab;
- supported text and image opens do not invoke the opener;
- `Esc o` and the menu call the same opener for text, image, and external tabs;
- no-path and opener-error cases produce the required status messages;
- completion events update status on the main event loop;
- read-only predicates disable save, find, formatting, comments, and text input.

### 9.3 Platform verification

- unit-test the injectable common orchestration on the host platform;
- test macOS/Linux command construction and Linux fallback selection without launching applications;
- cross-compile the Windows implementation to catch build-tag and syscall errors;
- run `make test`, `make build`, `make build-linux`, and a Windows `go build` before completion.

## 10. Risks and mitigations

- **Misclassification:** a known external-format set catches ASCII-heavy binary formats such as PDF, while content sampling overrides misleading text extensions. Fixtures cover NUL, invalid UTF-8, empty files, known external extensions, and corrupt images.
- **UI blocking:** opener calls run in a goroutine and return through a tcell event.
- **Path injection:** Unix commands receive the path as one argument; Windows uses `ShellExecuteW` instead of a command shell.
- **Headless hosts:** launcher errors appear in the status bar, and the placeholder remains usable.
- **SSH expectations:** documentation states that the application opens on the SpiceEdit host, not the SSH client.
- **Regression from broad read-only changes:** `IsReadOnly` replaces `IsImage` only where the code means editable text; graphics and image metadata paths retain `IsImage`.

## 11. Documentation updates

After implementation, update README and website hotkey/file-opening documentation with:

- `Esc o` and the menu label;
- automatic external opening for unsupported files;
- the supported inline image formats;
- host-local behavior in SSH sessions;
- Linux's dependency on `xdg-open` or `gio` for desktop handoff.
