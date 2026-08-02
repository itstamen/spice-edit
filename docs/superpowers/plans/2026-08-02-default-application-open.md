# Default Application Open Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Classify files before editing, represent unsupported files with a read-only placeholder tab, and open file-backed tabs through the operating system's default application with `Esc o` and the main menu.

**Architecture:** `internal/editor` owns bounded file classification and text/image/external tab modes. `internal/app` owns the asynchronous default-application handoff, platform adapters, status events, and UI wiring. Existing image-specific rendering remains separate while all editable-text checks use `Tab.IsReadOnly()`.

**Tech Stack:** Go, tcell simulation screens, `os/exec`, Windows `ShellExecuteW`, build tags, existing Make targets.

## Global Constraints

- Display the exact placeholder text `编辑器只支持文本及图片文件在线编辑.`.
- Keep supported text editable and supported PNG/JPEG/GIF images previewable inline.
- Use `Esc o`; add no `Ctrl+` shortcut and expose the action in the main `≡` menu.
- Open dirty text tabs from their on-disk path without saving or altering dirty/undo state.
- Run external openers asynchronously and report completion on the tcell event loop.
- Treat "local" as the host running SpiceEdit; SSH client transfer is out of scope.
- Preserve all pre-existing uncommitted changes. Do not create implementation commits unless the user requests one because target files already contain unrelated worktree edits.

---

### Task 1: Add bounded file classification

**Files:**
- Create: `internal/editor/filekind.go`
- Create: `internal/editor/filekind_test.go`
- Modify: `internal/editor/tab.go`

**Interfaces:**
- Produces: `classifyFile(path string) (fileKind, error)`.
- Produces: `fileKindText`, `fileKindImage`, and `fileKindExternal`.
- Consumed by: `editor.NewTab` in Task 2.

- [ ] **Step 1: Write failing classifier tests**

Cover missing files, empty files, UTF-8 unknown extensions, NUL/invalid UTF-8 disguised as `.txt`, known external formats such as `.pdf`, image candidates, and directory/read errors.

```go
func TestClassifyFile_BinaryOverridesTextExtension(t *testing.T) {
    path := filepath.Join(t.TempDir(), "payload.txt")
    if err := os.WriteFile(path, []byte{'a', 0, 'b'}, 0o644); err != nil {
        t.Fatal(err)
    }
    got, err := classifyFile(path)
    if err != nil {
        t.Fatal(err)
    }
    if got != fileKindExternal {
        t.Fatalf("kind = %v, want external", got)
    }
}
```

- [ ] **Step 2: Run the focused tests and confirm failure**

Run: `go test ./internal/editor -run 'TestClassifyFile'`

Expected: build failure because `classifyFile` and `fileKindExternal` do not exist.

- [ ] **Step 3: Implement the classifier**

Create an 8 KiB bounded reader, recognised inline-image extension check, known-external extension set, and UTF-8/NUL checks.

```go
const fileSniffLimit = 8 * 1024

func classifyFile(path string) (fileKind, error) {
    if path == "" {
        return fileKindText, nil
    }
    f, err := os.Open(path)
    if os.IsNotExist(err) {
        return fileKindText, nil
    }
    if err != nil {
        return fileKindText, err
    }
    defer f.Close()

    info, err := f.Stat()
    if err != nil {
        return fileKindText, err
    }
    if !info.Mode().IsRegular() {
        return fileKindText, fmt.Errorf("%s is not a regular file", path)
    }

    sample := make([]byte, fileSniffLimit)
    n, err := f.Read(sample)
    if err != nil && err != io.EOF {
        return fileKindText, err
    }
    sample = sample[:n]
    if isImageExt(path) {
        return fileKindImage, nil
    }
    if isExternalExt(path) || bytes.IndexByte(sample, 0) >= 0 || !utf8.Valid(sample) {
        return fileKindExternal, nil
    }
    return fileKindText, nil
}
```

- [ ] **Step 4: Run classifier tests**

Run: `go test ./internal/editor -run 'TestClassifyFile'`

Expected: PASS.

---

### Task 2: Add the external tab mode and placeholder renderer

**Files:**
- Create: `internal/editor/external.go`
- Create: `internal/editor/external_test.go`
- Modify: `internal/editor/tab.go`
- Modify: `internal/editor/comment.go`

**Interfaces:**
- Consumes: `classifyFile` and `fileKind` from Task 1.
- Produces: `newExternalTab(path string) *Tab`.
- Produces: `(*Tab).IsExternal() bool`, `(*Tab).IsReadOnly() bool`, and `(*Tab).RenderExternal(...)`.

- [ ] **Step 1: Write failing external-tab tests**

```go
func TestNewTab_BinaryCreatesExternalTab(t *testing.T) {
    path := filepath.Join(t.TempDir(), "data.bin")
    if err := os.WriteFile(path, []byte{0, 1, 2}, 0o644); err != nil {
        t.Fatal(err)
    }
    tab, err := NewTab(path)
    if err != nil {
        t.Fatal(err)
    }
    if !tab.IsExternal() || !tab.IsReadOnly() {
        t.Fatal("expected read-only external tab")
    }
    if got := tab.Buffer.String(); got != "" {
        t.Fatalf("external buffer = %q", got)
    }
}
```

Add simulation-screen assertions for the exact Chinese message and mutation tests proving Insert/Backspace/Delete/Save/ToggleLineComment are no-ops or errors.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/editor -run 'Test(NewTab_Binary|External)'`

Expected: build failures for the external-tab APIs.

- [ ] **Step 3: Implement the external mode**

```go
const externalMode = "external"
const externalMessage = "编辑器只支持文本及图片文件在线编辑."

func (t *Tab) IsExternal() bool { return t != nil && t.Mode == externalMode }
func (t *Tab) IsReadOnly() bool { return t != nil && (t.IsImage() || t.IsExternal()) }
```

Use `termtext.StringWidth` and `termtext.DrawString` to center the message. Update `NewTab` to classify first, decode image candidates, convert image decode failures to external tabs, and read the full file only for text tabs.

- [ ] **Step 4: Generalize editable-text guards**

Replace `IsImage` with `IsReadOnly` in Save, mutations, line comments, app-independent text operations, and `RenderSoftWrap` dispatch. Keep `IsImage` in graphics/image metadata paths.

- [ ] **Step 5: Run editor tests**

Run: `go test ./internal/editor`

Expected: PASS.

---

### Task 3: Add asynchronous default-application opening

**Files:**
- Create: `internal/app/defaultopen.go`
- Create: `internal/app/defaultopen_unix.go`
- Create: `internal/app/defaultopen_windows.go`
- Create: `internal/app/defaultopen_other.go`
- Create: `internal/app/defaultopen_test.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Produces: `type pathOpener func(string) error`.
- Produces: `platformOpenPath(path string) error` in every build-tag variant.
- Produces: `(*App).openPathWithDefaultApp(path string)` and `defaultOpenDoneEvent`.

- [ ] **Step 1: Write failing orchestration tests**

Inject an opener that records the literal path, trigger the action, poll the simulation screen for `defaultOpenDoneEvent`, and verify success/error status handling.

```go
a.openPath = func(path string) error {
    opened <- path
    return nil
}
a.openPathWithDefaultApp(target)
```

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/app -run 'TestDefaultOpen'`

Expected: build failures for `openPath`, `defaultOpenDoneEvent`, and the action method.

- [ ] **Step 3: Implement common asynchronous orchestration**

```go
type pathOpener func(string) error

type defaultOpenDoneEvent struct {
    when time.Time
    path string
    err  error
}

func (e *defaultOpenDoneEvent) When() time.Time { return e.when }
```

Run the opener in a goroutine, post the event to `a.screen`, and handle it inside `handleEvent`.

- [ ] **Step 4: Implement platform adapters**

Use `open` on macOS, `xdg-open` with `gio open` fallback on Linux, `ShellExecuteW` on Windows, and an unsupported-platform error elsewhere. Use build tags so each platform exports `platformOpenPath` once.

- [ ] **Step 5: Run app opener tests**

Run: `go test ./internal/app -run 'TestDefaultOpen'`

Expected: PASS.

---

### Task 4: Wire open flow, menu, leader, and read-only UI behavior

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/leader.go`
- Modify: `internal/app/find.go`
- Modify: `internal/app/app_test.go`
- Modify: `internal/app/leader_test.go`

**Interfaces:**
- Consumes: `Tab.IsExternal`, `Tab.IsReadOnly`, and `App.openPathWithDefaultApp`.
- Produces: `(*App).menuOpenWithDefaultApp()`; reuses the existing `(*App).hasFileTab()` path predicate.

- [ ] **Step 1: Write failing flow and UI tests**

Cover automatic launch for new/reopened external tabs, no launch for text/images, no duplicate external tab, menu enablement, `Esc o`, no-path status, dirty-state preservation, external status-bar rendering, and blocked key/mouse/find/comment paths.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/app -run 'Test(OpenFile_External|MenuOpenWithDefault|HandleKey_LeaderOpenWithDefault|ExternalTab)'`

Expected: failures because the flow is not wired.

- [ ] **Step 3: Update `openFile`**

When an existing or new tab is external, invoke the opener after activating it. Load git gutter data only for editable text tabs. Keep supported text/image behavior unchanged.

- [ ] **Step 4: Add menu and leader wiring**

```go
{'o', (*App).menuOpenWithDefaultApp},
```

Add `Open with default app` with `Esc o` to File actions. The action validates a non-empty path and calls the shared opener.

- [ ] **Step 5: Apply read-only predicates across App**

Use `IsReadOnly` for keyboard input, editor mouse selection, find, comments, soft-wrap-only routing, git gutters, save predicates, and disk reconciliation. Keep Rename/Delete/Copy path actions enabled for external tabs.

- [ ] **Step 6: Render external-tab status**

Show `EXTERNAL · <basename>` instead of line/column information.

- [ ] **Step 7: Run app and editor tests**

Run: `go test ./internal/editor ./internal/app`

Expected: PASS.

---

### Task 5: Update user documentation

**Files:**
- Modify: `README.md`
- Modify: the relevant files under `website/content/docs/`

**Interfaces:**
- Documents: `Esc o`, menu access, automatic unsupported-file handoff, inline formats, Linux opener requirement, and SSH host-local behavior.

- [ ] **Step 1: Locate the hotkey and file-opening sections**

Run: `rg -n 'Esc z|hotkey|image|SSH|file explorer' README.md website/content/docs`

- [ ] **Step 2: Update documentation with exact behavior**

State that text files edit inline, PNG/JPEG/GIF preview inline, unsupported files open in the host OS default application, and `Esc o` opens any file-backed active tab externally.

- [ ] **Step 3: Check documentation formatting**

Run: `git diff --check`

Expected: no whitespace errors.

---

### Task 6: Full regression and cross-platform build validation

**Files:**
- No source changes expected unless verification exposes a defect.

**Interfaces:**
- Validates all deliverables from Tasks 1-5.

- [ ] **Step 1: Format changed Go files**

Run: `gofmt -w internal/editor/filekind.go internal/editor/filekind_test.go internal/editor/external.go internal/editor/external_test.go internal/editor/tab.go internal/editor/comment.go internal/app/defaultopen.go internal/app/defaultopen_unix.go internal/app/defaultopen_windows.go internal/app/defaultopen_other.go internal/app/defaultopen_test.go internal/app/app.go internal/app/leader.go internal/app/find.go internal/app/app_test.go internal/app/leader_test.go`

- [ ] **Step 2: Run focused packages**

Run: `go test ./internal/editor ./internal/app`

Expected: PASS.

- [ ] **Step 3: Run repository tests**

Run: `make test`

Expected: PASS under the race detector.

- [ ] **Step 4: Build the native binary**

Run: `make build`

Expected: `bin/spiceedit` builds successfully.

- [ ] **Step 5: Build Linux**

Run: `make build-linux`

Expected: Linux amd64 binary builds successfully.

- [ ] **Step 6: Cross-compile Windows into a temporary directory**

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/spiceedit-windows-amd64.exe .`

Expected: Windows amd64 binary builds successfully without writing generated artifacts into the repository.

- [ ] **Step 7: Final diff audit**

Run: `git diff --check` and `git status --short`.

Expected: no whitespace errors; unrelated pre-existing worktree changes remain present and untouched.
