# Show in File Manager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `Esc e` and a menu action that selects the active file in Finder or Windows Explorer, falling back to opening the project root when no file-backed tab is active.

**Architecture:** A common asynchronous reveal coordinator resolves the target and posts completion through tcell. Build-tagged macOS and Windows adapters perform the platform operation; Linux and other platforms return a clear unsupported error. The existing default-application opener stays separate.

**Tech Stack:** Go, tcell, `os/exec`, `golang.org/x/sys/windows`, Windows Shell APIs, build tags, Go tests, Make targets.

## Global Constraints

- Bind `Esc e` and expose the same action in the main `≡` menu.
- Select the active file on macOS and Windows when the tab has a path.
- Open the project root when there is no active file path.
- Do not use the sidebar active folder as fallback.
- Do not add Linux file-manager support.
- Do not show a confirmation dialog.
- Run platform calls asynchronously and update status only on the tcell event loop.
- Preserve dirty buffers, active tabs, selections, and undo history.
- Preserve unrelated Kitty design-document changes in the worktree.

---

### Task 1: Add common reveal orchestration and tests

**Files:**
- Create: `internal/app/reveal.go`
- Create: `internal/app/reveal_test.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Produces: `type pathRevealer func(path string, selectFile bool) error`.
- Produces: `type revealDoneEvent struct` implementing `tcell.Event`.
- Produces: `(*App).showInFileManager()` and `(*App).revealPathInFileManager(path string, selectFile bool)`.
- Consumes: build-tagged `platformRevealPath(path string, selectFile bool) error` from Task 2.

- [x] **Step 1: Write failing target-resolution tests**

Inject `App.revealPath` and verify an active file passes its absolute path with `selectFile=true`, while no-tab and untitled states pass `rootDir` with `selectFile=false`:

```go
a.revealPath = func(path string, selectFile bool) error {
    calls <- revealCall{path: path, selectFile: selectFile}
    return nil
}
a.showInFileManager()
```

Use text, image, and external fixtures so every tab mode follows the same target rule.

- [x] **Step 2: Write failing asynchronous result tests**

Verify success and failure arrive as `revealDoneEvent`, status copy distinguishes selected files from an opened root, and missing paths fail before invoking the injected adapter.

- [x] **Step 3: Implement common orchestration**

Create `internal/app/reveal.go` with:

```go
type pathRevealer func(path string, selectFile bool) error

type revealDoneEvent struct {
    when       time.Time
    path       string
    selectFile bool
    err        error
}

func (e *revealDoneEvent) When() time.Time { return e.when }

func (a *App) showInFileManager() {
    path := a.rootDir
    selectFile := false
    if tab := a.activeTabPtr(); tab != nil && tab.Path != "" {
        path = tab.Path
        selectFile = true
    }
    a.revealPathInFileManager(path, selectFile)
}
```

Validate a non-empty absolute path with `os.Stat`, run the injected or platform adapter in a goroutine, and post `revealDoneEvent` to `a.screen`.

- [x] **Step 4: Route completion events**

Add `revealPath pathRevealer` to `App` and route `*revealDoneEvent` in `handleEvent`:

```go
case *revealDoneEvent:
    a.handleRevealDone(e)
```

- [x] **Step 5: Run focused tests**

Run: `go test ./internal/app -run 'Test(ShowInFileManager|Reveal)'`

Expected: PASS.

---

### Task 2: Implement platform reveal adapters

**Files:**
- Create: `internal/app/reveal_darwin.go`
- Create: `internal/app/reveal_darwin_test.go`
- Create: `internal/app/reveal_windows.go`
- Create: `internal/app/reveal_other.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces on every platform: `platformRevealPath(path string, selectFile bool) error`.
- Consumes on Windows: existing `platformOpenPath(path string) error` for project-root directories.

- [x] **Step 1: Implement and test macOS argument construction**

```go
func darwinRevealArgs(path string, selectFile bool) []string {
    if selectFile {
        return []string{"-R", path}
    }
    return []string{path}
}

func platformRevealPath(path string, selectFile bool) error {
    return exec.Command("open", darwinRevealArgs(path, selectFile)...).Run()
}
```

Test spaces, Unicode, and shell metacharacters remain one literal path argument.

- [x] **Step 2: Implement Windows Shell selection**

Use `runtime.LockOSThread`, `windows.CoInitializeEx`, `SHParseDisplayName`, and `SHOpenFolderAndSelectItems`. Microsoft documents that `cidl=0` with a fully qualified item PIDL opens the parent and selects that item, so no child-PIDL array is needed.

```go
func platformRevealPath(path string, selectFile bool) error {
    if !selectFile {
        return platformOpenPath(path)
    }
    runtime.LockOSThread()
    defer runtime.UnlockOSThread()

    uninitialize, err := initializeCOM()
    if err != nil {
        return err
    }
    if uninitialize {
        defer windows.CoUninitialize()
    }

    pathPtr, err := windows.UTF16PtrFromString(path)
    if err != nil {
        return err
    }
    var pidl unsafe.Pointer
    hr, _, _ := shParseDisplayName.Call(
        uintptr(unsafe.Pointer(pathPtr)), 0,
        uintptr(unsafe.Pointer(&pidl)), 0, 0,
    )
    runtime.KeepAlive(pathPtr)
    if hresultFailed(hr) {
        return hresultError("SHParseDisplayName", hr)
    }
    defer windows.CoTaskMemFree(pidl)

    hr, _, _ = shOpenFolderAndSelectItems.Call(uintptr(pidl), 0, 0, 0)
    if hresultFailed(hr) {
        return hresultError("SHOpenFolderAndSelectItems", hr)
    }
    return nil
}
```

Treat `S_OK` and `S_FALSE` as successful COM initialization requiring `CoUninitialize`; treat `RPC_E_CHANGED_MODE` as already initialized without uninitializing; return every other HRESULT.

- [x] **Step 3: Implement unsupported platforms**

Use the build tag `!darwin && !windows` and return:

```go
return fmt.Errorf("show in file manager is not supported on %s", runtime.GOOS)
```

- [x] **Step 4: Synchronize modules**

Run: `go mod tidy`

Expected: `golang.org/x/sys` becomes a direct dependency because Windows code imports `x/sys/windows`.

- [x] **Step 5: Verify platform compilation**

Run: `go test ./internal/app -run 'TestDarwinRevealArgs'`

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/spice-edit-app-windows.test.exe ./internal/app`

Expected: both commands succeed.

---

### Task 3: Wire menu, leader, and documentation

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/leader.go`
- Modify: `internal/app/leader_test.go`
- Modify: `internal/app/reveal_test.go`
- Modify: `README.md`
- Modify: `website/content/docs/getting-started.md`
- Modify: `website/content/docs/hotkeys.md`
- Modify: `website/content/docs/mouse-controls.md`
- Modify: `website/content/docs/running-over-ssh.md`
- Modify: `website/content/docs/troubleshooting.md`

**Interfaces:**
- Produces: `(*App).menuShowInFileManager()`.
- Adds: `leaderBindings` entry `{'e', (*App).menuShowInFileManager}`.
- Adds menu label: `Show in file manager` with shortcut `Esc e`.

- [x] **Step 1: Add failing menu and leader tests**

Verify the menu row label and shortcut, then inject `revealPath` and dispatch the `e` leader action. The call must use the current file path or project root according to Task 1.

- [x] **Step 2: Wire the action**

Add the File actions menu row beside **Open with default app**, add `e` to `leaderBindings`, and implement:

```go
func (a *App) menuShowInFileManager() {
    a.closeMenu()
    a.showInFileManager()
}
```

- [x] **Step 3: Update both feature descriptions**

Document that unsupported files clicked in the tree require confirmation. Add `Esc e`, Finder/Explorer selection, project-root fallback, no Linux support, and SSH host-local behavior.

- [x] **Step 4: Run focused app tests**

Run: `go test ./internal/app -run 'Test(SidebarClick_|ShowInFileManager|Reveal|Leader_E|MenuLayout_Show)'`

Expected: PASS.

---

### Task 4: Full verification and commit

**Files:**
- No source changes expected unless verification finds a defect.

**Interfaces:**
- Validates the tree confirmation and file-manager reveal features together.

- [x] **Step 1: Format and audit**

Run: `gofmt -w internal/app/app.go internal/app/externalfile_test.go internal/app/leader.go internal/app/leader_test.go internal/app/reveal.go internal/app/reveal_test.go internal/app/reveal_darwin.go internal/app/reveal_darwin_test.go internal/app/reveal_windows.go internal/app/reveal_other.go`

Run: `git diff --check`

Expected: no output.

- [x] **Step 2: Run repository tests**

Run: `make test`

Expected: PASS under the race detector.

- [x] **Step 3: Build all required targets**

Run: `make build`

Run: `make build-linux`

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/spice-edit-app-windows.test.exe ./internal/app`

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/spiceedit-windows-amd64.exe .`

Expected: all commands succeed.

- [x] **Step 4: Commit implementation**

Stage only the two feature implementations, plans, and user documentation. Do not stage the pre-existing Kitty design drafts.

```bash
git commit -m "Confirm external opens and reveal files in managers"
```

- [x] **Step 5: Audit final status**

Run: `git status --short --branch`

Expected: only the pre-existing Kitty design-document changes remain uncommitted.
