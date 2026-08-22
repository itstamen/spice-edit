# Paste, Folder Creation, and Delete-Line Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve newlines for terminal and internal clipboard pastes, add main-menu folder creation, and add `Esc d` deletion of all lines touched by a selection.

**Architecture:** Keep line deletion in `internal/editor`, folder creation in `internal/app/fileops.go`, and terminal event capture in `internal/app/app.go`. Normalize all clipboard newlines before one `Tab.InsertString` call; buffer bracketed terminal paste until tcell supplies its end boundary.

**Tech Stack:** Go, tcell v2.13.9, standard library filesystem APIs, Go test, Make.

## Global Constraints

- Use LF (`\n`) as the only buffer newline representation; normalize pasted CRLF and CR before insertion.
- Do not add Ctrl-key shortcuts; all actions remain reachable from the main menu.
- `New folder` appears only in the main menu and has no Esc-leader shortcut.
- `Esc d` deletes every logical line touched by an active selection, including reverse selections.
- Each completed paste and delete-lines command must produce one structural undo snapshot.
- Preserve at least one empty buffer line and leave unrelated worktree changes untouched.

---

### Task 1: Editor Delete-Lines Primitive

**Files:**
- Modify: `internal/editor/tab.go:480-620`
- Modify: `internal/editor/tab_test.go`
- Modify: `internal/editor/undo_test.go`

**Interfaces:**
- Produces: `func (t *Tab) DeleteCurrentLines()`.
- Consumes: `Buffer.Clamp`, `Buffer.LineCount`, `pushUndo(undoGroupStructural)`, and `markTextChanged`.
- Consumed by: Task 4's App action.

- [ ] **Step 1: Write failing editor tests**

Add a table test that calls `DeleteCurrentLines` and asserts buffer, cursor, anchor, `Dirty`, and `cursorMoved` for: one middle line; forward selection touching lines 1-2; reverse selection touching lines 1-2; last line; and the only line. Use these core cases:

```go
{"cursor", "one\ntwo\nthree", Position{1, 1}, Position{1, 1}, "one\nthree", Position{1, 0}},
{"forward", "one\ntwo\nthree\nfour", Position{1, 2}, Position{2, 1}, "one\nfour", Position{1, 0}},
{"reverse", "one\ntwo\nthree\nfour", Position{2, 1}, Position{1, 2}, "one\nfour", Position{1, 0}},
{"only", "one", Position{}, Position{}, "", Position{}},
```

Add a read-only no-op test. In `undo_test.go`, add `TestDeleteCurrentLines_UndoRestoresState`, which snapshots multi-line text and a reverse selection, deletes, calls `Undo`, and requires exact buffer/cursor/anchor restoration.

- [ ] **Step 2: Verify focused tests fail**

Run: `go test ./internal/editor -run 'TestTab_DeleteCurrentLines|TestDeleteCurrentLines' -count=1`

Expected: FAIL because `DeleteCurrentLines` does not exist.

- [ ] **Step 3: Implement the editor primitive**

Add a doc-commented `DeleteCurrentLines` beside `DeleteSelection`. Derive `start`/`end` from the cursor or clamped anchor/cursor line numbers. Push exactly one structural undo entry, replace an all-buffer deletion with `[]string{""}`, otherwise remove the inclusive slice with:

```go
t.Buffer.Lines = append(t.Buffer.Lines[:start], t.Buffer.Lines[end+1:]...)
pos := t.Buffer.Clamp(Position{Line: start, Col: 0})
t.Cursor, t.Anchor = pos, pos
t.Dirty = true
t.markTextChanged()
t.cursorMoved = true
```

Never call `DeleteRange`: it would join the lines adjacent to the deleted region.

- [ ] **Step 4: Verify focused tests pass**

Run: `go test ./internal/editor -run 'TestTab_DeleteCurrentLines|TestDeleteCurrentLines' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit Task 1**

```bash
git add internal/editor/tab.go internal/editor/tab_test.go internal/editor/undo_test.go
git commit -m "Add delete current lines editor action"
```

### Task 2: Main-Menu Folder Creation

**Files:**
- Modify: `internal/app/fileops.go:42-130,196-245`
- Modify: `internal/app/app.go:220-234`
- Modify: `internal/app/fileops_test.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Produces: `func createFolder(string) error`, `func (a *App) doCreateFolder(parent, name string)`, `func (a *App) menuNewFolder()`.
- Consumes: `activeFolder`, `rootDir`, `relativeFolderLabel`, `openPrompt`, `refreshTree`, `refreshGitStatus`, `invalidateFinder`, `sidebarRect`, `Tree.Reveal`.
- Does not produce a context-menu action or an Esc-leader binding.

- [ ] **Step 1: Write failing filesystem and menu tests**

In `fileops_test.go`, create a nested `assets/icons` directory with `createFolder`, assert it is a directory, then require a second call to return an existing-target error. In `app_test.go`, call `menuNewFolder`, assert title `New folder` and an active-folder hint, submit `assets/icons`, then assert the directory exists. Add a missing active-folder case that asserts the prompt uses `rootDir`, and a menu table assertion that `New folder` has no shortcut.

- [ ] **Step 2: Verify focused tests fail**

Run: `go test ./internal/app -run 'TestCreateFolder|TestMenuNewFolder|TestBuiltinMenu.*NewFolder' -count=1`

Expected: FAIL because the helpers and menu action do not exist.

- [ ] **Step 3: Implement folder creation and menu wiring**

Add `createFolder` next to `createEmptyFile`; reject an existing path with `os.Lstat`, then use `os.MkdirAll(path, 0o755)`. Add `childPath(parent, name)` to reject empty/absolute paths, `.` and clean paths equal to or escaping via `..` before joining to the parent.

`doCreateFolder` must trim input, validate it, create the path, then refresh tree/git/finder, call `a.tree.Reveal(target, listH)` using `max(0, sidebarH-2)`, and flash `Created folder ` followed by the submitted relative path. Errors must prefix the concrete validation or filesystem error with `Create folder failed: `.

Mirror `menuNewFile` for active-folder fallback and prompt invocation:

```go
a.openPrompt("New folder", "in "+a.relativeFolderLabel(folder), "", func(app *App, value string) {
    app.doCreateFolder(folder, value)
})
```

Insert `{label: "New folder", action: (*App).menuNewFolder, enabled: alwaysTrue}` immediately after `New file` in the File actions group.

- [ ] **Step 4: Verify focused tests pass**

Run: `go test ./internal/app -run 'TestCreateFolder|TestMenuNewFolder|TestBuiltinMenu.*NewFolder' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit Task 2**

```bash
git add internal/app/fileops.go internal/app/fileops_test.go internal/app/app.go internal/app/app_test.go
git commit -m "Add main menu folder creation"
```

### Task 3: Unified Internal and Bracketed Terminal Paste

**Files:**
- Modify: `internal/app/app.go:340-420,1119-1145,1387-1545,2565-2578`
- Modify: `internal/app/app_test.go`
- Modify: `internal/app/eventburst_test.go`

**Interfaces:**
- Produces: `func normalizePastedText(string) string`, `func (a *App) beginPaste()`, `func (a *App) appendPastedKey(*tcell.EventKey)`, `func (a *App) finishPaste()`.
- Consumes: tcell `EventPaste.Start/End`, `Tab.InsertString`, and `strings.NewReplacer`.

- [ ] **Step 1: Write failing paste tests**

Add:

```go
func TestNormalizePastedText(t *testing.T) {
    if got := normalizePastedText("a\r\nb\rc\nd"); got != "a\nb\nc\nd" {
        t.Fatalf("normalized = %q", got)
    }
}
```

Set `a.clipBuf = "a\\r\\nb\\rc"`, call `pasteClipboard`, and require three buffer lines. Feed `handleEvent` a `NewEventPaste(true)`, rune keys, `KeyEnter`, `KeyTab`, an Esc-like sequence, and `NewEventPaste(false)`; require no mutation before the end, exact final text after it, no leader action, and one `Undo` restoring the initial state. Add a direct ordinary `KeyEnter` test proving immediate newline insertion outside capture.

- [ ] **Step 2: Verify focused tests fail**

Run: `go test ./internal/app -run 'TestNormalizePastedText|TestPasteClipboard.*Newline|TestHandleEvent_BracketedPaste|TestHandleKey_EnterOutsidePaste' -count=1`

Expected: FAIL because `EventPaste` is ignored and internal clipboard text is unnormalized.

- [ ] **Step 3: Implement capture and normalization**

Add `pasting bool` and `pasteBuf strings.Builder` to `App`. In `handleEvent`, route `*tcell.EventPaste`: start calls `beginPaste`; end calls `finishPaste`. At the start of `handleKey`, before modal/menu/leader handling, divert events to `appendPastedKey` while capture is active.

Translate `KeyRune`, `KeyEnter`, `KeyTab`, `KeyCtrlM`, and `KeyCtrlJ` into text; ignore non-text navigation/function keys. `finishPaste` clears capture state first and calls one helper that normalizes via:

```go
strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
```

Then insert once through `Tab.InsertString` only when the active tab exists and is writable. Route `pasteClipboard` through that same helper. Do not change ordinary non-bracketed key behavior.

- [ ] **Step 4: Verify focused tests pass**

Run: `go test ./internal/app -run 'TestNormalizePastedText|TestPasteClipboard.*Newline|TestHandleEvent_BracketedPaste|TestHandleKey_EnterOutsidePaste' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit Task 3**

```bash
git add internal/app/app.go internal/app/app_test.go internal/app/eventburst_test.go
git commit -m "Preserve newlines in pasted text"
```

### Task 4: Bind Delete-Lines to Menu and Esc Leader

**Files:**
- Modify: `internal/app/app.go:215-240,2910-2945`
- Modify: `internal/app/leader.go:28-58`
- Modify: `internal/app/leader_test.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Consumes: `func (t *editor.Tab) DeleteCurrentLines()` from Task 1.
- Produces: `func (a *App) menuDeleteCurrentLine()` and `{'d', (*App).menuDeleteCurrentLine}`.

- [ ] **Step 1: Write failing command-routing tests**

Open a three-line editable tab, make a multi-line selection, send `Esc` then `d`, and assert the touched lines disappear. Add a menu test that finds `Delete current line`, requires shortcut `Esc d`, invokes its action, and verifies cursor-line deletion. Add a read-only path test that makes both routes no-ops.

- [ ] **Step 2: Verify focused tests fail**

Run: `go test ./internal/app -run 'TestLeaderDeleteCurrentLine|TestMenuDeleteCurrentLine|TestBuiltinMenu.*DeleteCurrentLine' -count=1`

Expected: FAIL because the App action and binding do not exist.

- [ ] **Step 3: Implement menu and leader action**

Add the Editing menu row:

```go
{label: "Delete current line", shortcut: "Esc d", action: (*App).menuDeleteCurrentLine, enabled: (*App).hasEditableTextTab},
```

Add:

```go
func (a *App) menuDeleteCurrentLine() {
    a.closeMenu()
    if tab := a.activeTabPtr(); tab != nil {
        tab.DeleteCurrentLines()
    }
}
```

Add `{'d', (*App).menuDeleteCurrentLine}` to `leaderBindings` by the other editing commands.

- [ ] **Step 4: Verify focused tests pass**

Run: `go test ./internal/app -run 'TestLeaderDeleteCurrentLine|TestMenuDeleteCurrentLine|TestBuiltinMenu.*DeleteCurrentLine' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit Task 4**

```bash
git add internal/app/app.go internal/app/app_test.go internal/app/leader.go internal/app/leader_test.go
git commit -m "Bind delete current line to Esc d"
```

### Task 5: Integration Verification

**Files:**
- Modify: no source changes expected.

- [ ] **Step 1: Format touched Go files**

Run:

```bash
gofmt -w internal/editor/tab.go internal/editor/tab_test.go internal/editor/undo_test.go internal/app/app.go internal/app/app_test.go internal/app/eventburst_test.go internal/app/fileops.go internal/app/fileops_test.go internal/app/leader.go internal/app/leader_test.go
```

- [ ] **Step 2: Run focused package tests**

Run: `go test ./internal/editor ./internal/app -count=1`

Expected: PASS.

- [ ] **Step 3: Run repository validation**

Run:

```bash
make test
make build
git diff --check
git status --short
```

Expected: tests and build PASS, whitespace validation clean, unrelated worktree files unstaged.

- [ ] **Step 4: Commit only formatting changes when necessary**

Run `git status --short`; stage only Go files changed solely by `gofmt` and commit `Format paste and file actions`. Do not commit when formatting makes no changes.
