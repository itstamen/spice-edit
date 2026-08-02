# Tree External Open Confirmation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Require confirmation before a left file-tree click launches an unsupported file in the host operating system's default application.

**Architecture:** Add a small source-aware policy around the shared `App.openFile` lifecycle. The file tree selects a confirmation policy; existing callers retain immediate external opening. Reuse the current `openConfirm` modal and asynchronous opener instead of adding new modal or platform code.

**Tech Stack:** Go, tcell simulation screen, existing app confirmation modal, existing editor file classification, Go tests and Make targets.

## Global Constraints

- Confirm only left file-tree clicks on external files.
- Keep `Esc o`, the main menu, finder, command-line, and shared `openFile` behavior immediate.
- Create or activate the read-only external tab before showing confirmation.
- Keep the external tab active when the user cancels.
- Show confirmation on every tree click, including repeated clicks on an existing tab.
- Keep No as the default modal action.
- Do not change file classification, supported inline formats, or platform opener implementations.
- Preserve unrelated Kitty design-document changes in the worktree.

---

### Task 1: Lock down tree confirmation behavior

**Files:**
- Modify: `internal/app/externalfile_test.go`

**Interfaces:**
- Consumes: `App.sidebarClick`, `App.confirmYes`, `App.confirmCancel`, `App.openPath`, and `Tab.IsExternal`.
- Produces: regression coverage for the source-aware open policy implemented in Task 2.

- [x] **Step 1: Add a tree-row click helper**

Add a test helper that draws the app, scans the rendered tree rows with `HitTest`, and calls `sidebarClick` on the row whose path matches the fixture:

```go
func clickTreePath(t *testing.T, a *App, path string) {
    t.Helper()
    a.draw()
    sx, sy, _, sh := a.sidebarRect()
    for y := sy; y < sy+sh; y++ {
        node, ok := a.tree.HitTest(1, y-sy)
        if ok && node.Path == path {
            a.sidebarClick(sx+1, y)
            return
        }
    }
    t.Fatalf("tree path %q was not visible", path)
}
```

- [x] **Step 2: Add a failing confirmation test**

Create a PDF fixture, inject a channel-backed opener, click its tree row, and assert that the placeholder tab and modal exist before the opener runs:

```go
func TestSidebarClick_ExternalRequiresConfirmation(t *testing.T) {
    dir := t.TempDir()
    target := filepath.Join(dir, "manual.pdf")
    if err := os.WriteFile(target, []byte("%PDF-1.7"), 0o644); err != nil {
        t.Fatal(err)
    }
    a := newTestApp(t, dir)
    opened := make(chan string, 1)
    a.openPath = func(path string) error {
        opened <- path
        return nil
    }

    clickTreePath(t, a, target)

    if !a.confirmOpen || a.confirmHover != 0 {
        t.Fatalf("confirm open=%v hover=%d", a.confirmOpen, a.confirmHover)
    }
    if tab := a.activeTabPtr(); tab == nil || !tab.IsExternal() || tab.Path != target {
        t.Fatalf("active tab = %#v", tab)
    }
    select {
    case got := <-opened:
        t.Fatalf("opener ran before confirmation: %s", got)
    case <-time.After(50 * time.Millisecond):
    }

    a.confirmHover = 1
    a.confirmYes()
    if got := <-opened; got != target {
        t.Fatalf("opened path = %q, want %q", got, target)
    }
    a.handleEvent(waitForDefaultOpenDone(t, a))
}
```

- [x] **Step 3: Add cancel and repeated-click tests**

Verify that No leaves one active external tab, does not launch, and a second tree click opens the modal again without duplicating the tab. Also verify text and image tree clicks never open the confirmation modal.

- [x] **Step 4: Run focused tests and confirm failure**

Run: `go test ./internal/app -run 'TestSidebarClick_(External|Text|Image)'`

Expected: FAIL because `sidebarClick` still invokes the opener immediately and does not open confirmation.

---

### Task 2: Add the source-aware external-open policy

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/externalfile_test.go`

**Interfaces:**
- Produces: `type externalOpenPolicy uint8`.
- Produces: `externalOpenImmediate` and `externalOpenConfirm`.
- Produces: `(*App).openFileWithExternalPolicy(path string, policy externalOpenPolicy)`.
- Produces: `(*App).handleExternalFileOpen(path string, policy externalOpenPolicy)`.
- Consumed by: `openFile`, `OpenFile`, and `sidebarClick`.

- [x] **Step 1: Define the policy and shared open method**

Keep `openFile` as the immediate wrapper so every existing caller preserves behavior:

```go
type externalOpenPolicy uint8

const (
    externalOpenImmediate externalOpenPolicy = iota
    externalOpenConfirm
)

func (a *App) openFile(path string) {
    a.openFileWithExternalPolicy(path, externalOpenImmediate)
}

func (a *App) openFileWithExternalPolicy(path string, policy externalOpenPolicy) {
    // Existing openFile body. Replace each external-tab opener call with:
    // a.handleExternalFileOpen(t.Path, policy)
}
```

- [x] **Step 2: Add the confirmation dispatch**

Reuse the existing modal. Capture the canonical path after `openFileWithExternalPolicy` resolves it:

```go
func (a *App) handleExternalFileOpen(path string, policy externalOpenPolicy) {
    if policy == externalOpenConfirm {
        name := filepath.Base(path)
        a.openConfirm(
            "Open with default app?",
            fmt.Sprintf("Open %s with the system default application?", name),
            func(app *App) { app.openPathWithDefaultApp(path) },
        )
        return
    }
    a.openPathWithDefaultApp(path)
}
```

- [x] **Step 3: Wire only the file tree to confirmation**

Change the file branch in `sidebarClick`:

```go
a.setActiveFolder(filepath.Dir(n.Path))
a.openFileWithExternalPolicy(n.Path, externalOpenConfirm)
```

Do not change finder, command-line, menu, or leader callers.

- [x] **Step 4: Run focused tests**

Run: `go test ./internal/app -run 'Test(SidebarClick_|OpenFile_External|MenuOpenWithDefault|DefaultOpen)'`

Expected: PASS.

- [x] **Step 5: Run package tests**

Run: `go test ./internal/editor ./internal/app`

Expected: PASS.

---

### Task 3: Update documentation and complete regression checks

**Files:**
- Modify: `README.md`
- Modify: `website/content/docs/getting-started.md`
- Modify: `website/content/docs/mouse-controls.md`

**Interfaces:**
- Documents: tree-click confirmation while preserving explicit `Esc o` and menu behavior.

- [x] **Step 1: Update user-facing documentation**

Replace statements that unsupported tree clicks immediately launch the host application. State that SpiceEdit first activates the read-only placeholder and asks for confirmation. Keep the existing `Esc o`, platform, Linux dependency, and SSH explanations.

- [x] **Step 2: Format and inspect changes**

Run: `gofmt -w internal/app/app.go internal/app/externalfile_test.go`

Run: `git diff --check`

Expected: no output.

- [x] **Step 3: Run repository tests**

Run: `make test`

Expected: PASS under the race detector.

- [x] **Step 4: Build supported targets**

Run: `make build`

Run: `make build-linux`

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/spice-edit-app-windows.test.exe ./internal/app`

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/spiceedit-windows-amd64.exe .`

Expected: all four commands succeed.

- [x] **Step 5: Commit the implementation**

```bash
git add README.md website/content/docs/getting-started.md website/content/docs/mouse-controls.md \
  docs/superpowers/plans/2026-08-02-tree-external-open-confirmation.md \
  docs/superpowers/specs/2026-08-02-tree-external-open-confirmation-design.md \
  internal/app/app.go internal/app/externalfile_test.go
git commit -m "Confirm external file opens from tree"
```

- [x] **Step 6: Audit final status**

Run: `git status --short --branch`

Expected: only the pre-existing Kitty design-document changes remain uncommitted.
