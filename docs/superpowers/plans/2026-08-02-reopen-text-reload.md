# Reopen Text Reload Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every repeated open of an existing text tab read the current disk contents, while confirming before discarding unsaved edits.

**Architecture:** Keep `openFileWithExternalPolicy` as the shared entry point and delegate existing editable text tabs to two focused helpers. One helper decides whether confirmation is required; the other performs `Tab.Reload`, Git-line refresh, and status reporting. Image and external paths remain unchanged.

**Tech Stack:** Go, tcell confirmation modal, existing `editor.Tab.Reload`, simulation-screen tests, Make targets.

## Global Constraints

- Reload every clean existing text tab unconditionally on repeated open.
- Do not gate reload on modification time.
- Confirm before replacing a dirty text buffer.
- Keep No as the default confirmation action.
- No, Esc, or outside-click cancellation must preserve buffer, Dirty, cursor, selection, scroll, and undo history.
- Successful confirmed reload must clear Dirty and reset undo/redo through `Tab.Reload`.
- Do not reload image tabs.
- Do not change external-file confirmation or default-application behavior.
- Preserve unrelated Kitty design files in the worktree.

---

### Task 1: Add repeated-open regression tests

**Files:**
- Create: `internal/app/reopen_test.go`

**Interfaces:**
- Consumes: `App.openFile`, `App.confirmYes`, `App.handleConfirmKey`, `Tab.Reload`, and `Tab.CanUndo`.
- Produces: failing tests for the helpers implemented in Task 2.

- [x] **Step 1: Test clean reload without mtime change**

Open a text file, overwrite its disk contents, restore its original mtime with `os.Chtimes`, then open it again:

```go
tab := a.activeTabPtr()
mtime := tab.Mtime
if err := os.WriteFile(target, []byte("new disk contents"), 0o644); err != nil {
    t.Fatal(err)
}
if err := os.Chtimes(target, mtime, mtime); err != nil {
    t.Fatal(err)
}
a.openFile(target)
```

Assert the buffer contains the new contents, one tab remains, Dirty is false, and status is `Reloaded notes.txt from disk`.

- [x] **Step 2: Test dirty cancel behavior**

Dirty the buffer, overwrite the disk file, and call `openFile` again. Assert the modal title is `Reload file from disk?`, No is selected, and the current buffer remains. Send Esc through `handleConfirmKey` and verify Dirty, buffer contents, cursor, selection, scroll, and `CanUndo()` are unchanged.

- [x] **Step 3: Test dirty confirmation behavior**

Repeat the dirty setup, set `confirmHover = 1`, and call `confirmYes()`. Assert disk contents replace the buffer, Dirty is false, undo and redo are empty, and status reports a successful reload.

- [x] **Step 4: Test reload failure protection**

Open a clean text file, remove it from disk, and open the same path again. Assert the previous buffer remains and status starts with `Could not reload notes.txt:`.

- [x] **Step 5: Run focused tests and confirm failure**

Run: `go test ./internal/app -run 'TestOpenFile_Reopen'`

Expected: tests fail because existing tabs are only activated and never reloaded or confirmed.

---

### Task 2: Implement existing-text-tab reload flow

**Files:**
- Modify: `internal/app/app.go`
- Test: `internal/app/reopen_test.go`

**Interfaces:**
- Produces: `(*App).reopenTextTab(tab *editor.Tab)`.
- Produces: `(*App).reloadTextTabFromDisk(tab *editor.Tab)`.
- Consumed by: `openFileWithExternalPolicy` existing-tab branch.

- [x] **Step 1: Add the reload helpers**

```go
func (a *App) reopenTextTab(tab *editor.Tab) {
    if tab.Dirty {
        name := tab.DisplayName()
        a.openConfirm(
            "Reload file from disk?",
            fmt.Sprintf("Reload %s and discard unsaved changes?", name),
            func(app *App) { app.reloadTextTabFromDisk(tab) },
        )
        return
    }
    a.reloadTextTabFromDisk(tab)
}

func (a *App) reloadTextTabFromDisk(tab *editor.Tab) {
    name := tab.DisplayName()
    if err := tab.Reload(); err != nil {
        a.flash(fmt.Sprintf("Could not reload %s: %v", name, err))
        return
    }
    tab.GitLines = loadGitLineChanges(a.rootDir, tab.Path)
    a.flash(fmt.Sprintf("Reloaded %s from disk", name))
}
```

- [x] **Step 2: Wire the existing-tab branch**

After activating the matching tab:

```go
if t.IsExternal() {
    a.handleExternalFileOpen(t.Path, policy)
    return
}
if !t.IsReadOnly() {
    a.reopenTextTab(t)
}
return
```

Do not call the helper for image tabs.

- [x] **Step 3: Run focused tests**

Run: `go test ./internal/app -run 'TestOpenFile_Reopen'`

Expected: PASS.

- [x] **Step 4: Run related open-flow tests**

Run: `go test ./internal/app -run 'Test(OpenFile_|SidebarClick_|FinderKey_EnterOpensFile|External)'`

Expected: PASS.

---

### Task 3: Verify and commit

**Files:**
- Modify: `docs/superpowers/specs/2026-08-02-reopen-text-reload-design.md`
- Modify: `docs/superpowers/plans/2026-08-02-reopen-text-reload.md`

**Interfaces:**
- Marks the approved design and completed plan.

- [x] **Step 1: Mark documentation complete**

Change the design status from `Proposed` to `Approved` and mark every implementation-plan checkbox complete.

- [x] **Step 2: Format and audit**

Run: `gofmt -w internal/app/app.go internal/app/reopen_test.go`

Run: `git diff --check`

Expected: no output.

- [x] **Step 3: Run repository tests**

Run: `make test`

Expected: PASS under the race detector.

- [x] **Step 4: Build all required targets**

Run: `make build`

Run: `make build-linux`

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/spice-edit-app-windows.test.exe ./internal/app`

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/spiceedit-windows-amd64.exe .`

Expected: all commands succeed.

- [x] **Step 5: Commit the fix**

Stage only the reload fix, its tests, plan, and approved design. Do not stage the pre-existing Kitty design files.

```bash
git commit -m "Reload text files when reopened"
```

- [x] **Step 6: Audit final status**

Run: `git status --short --branch`

Expected: only the pre-existing Kitty design files remain uncommitted.
