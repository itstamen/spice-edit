# Sidebar Path Info Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a compact, interactive path-information panel below the project tree that computes and automatically copies the clicked node's relative or absolute path.

**Architecture:** Keep tree ownership in `filetree.Tree` and add path-panel state and behavior to `app.App`. Split the sidebar into a tree rectangle and a fixed eight-row information rectangle, then route mouse, keyboard, and authorized bracketed-paste events to the panel without turning it into a modal. Clipboard writes occur only on explicit selection or panel-state changes, never during drawing or generic tab/tree synchronization.

**Tech Stack:** Go, tcell simulation screens, existing `termtext`, `theme`, clipboard injection, and same-package Go tests.

## Global Constraints

- The panel appears only in project mode while the sidebar and tree are visible; single-file mode remains unchanged.
- All visible labels are English: `PATH INFO`, `Path type`, `Relative`, `Absolute`, `Strip prefix`, and `Current path`.
- Relative is the zero-value/default path type and is rooted at the opened project directory.
- Prefix stripping is a case-sensitive literal prefix operation applied after relative/absolute conversion.
- An empty prefix is valid; an invalid non-empty prefix leaves the full unstripped value visible and colors the input text with `theme.Error`.
- The full computed value is copied even when its on-screen rendering is shortened.
- Clipboard writes happen only on left/right tree-node clicks, path-type changes, and prefix content changes or paste; drawing, tab switching, and generic tree synchronization must not write the clipboard.
- Do not add dependencies or `Ctrl+` shortcuts.
- Preserve the existing source-file header style and short intent comments.

---

### Task 1: Path State, Layout, And Rendering

**Files:**
- Create: `internal/app/sidebarpath.go`
- Create: `internal/app/sidebarpath_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Consumes: `App.tree.SelectedPath`, `App.relativePathFor(string) string`, `absolutePathFor(string) string`, `drawTextInput`, `termtext.DrawString`, and `theme.Error`.
- Produces: `sidebarPathType`, `sidebarPathRect()`, `currentSidebarPath() (string, bool)`, and `drawSidebarPath()` for Task 2.

- [ ] **Step 1: Write failing state and layout tests**

Create table-driven tests that assert the exact behavior below:

```go
func TestCurrentSidebarPath(t *testing.T) {
    root := t.TempDir()
    child := filepath.Join(root, "pkg", "file.go")
    a := newTestApp(t, root)
    a.tree.SelectedPath = child

    got, valid := a.currentSidebarPath()
    if got != filepath.Join("pkg", "file.go") || !valid {
        t.Fatalf("default relative = %q, %v", got, valid)
    }

    a.sidebarPathType = sidebarPathAbsolute
    got, valid = a.currentSidebarPath()
    if got != child || !valid {
        t.Fatalf("absolute = %q, %v", got, valid)
    }

    a.sidebarPathPrefix = []rune(root + string(filepath.Separator))
    got, valid = a.currentSidebarPath()
    if got != filepath.Join("pkg", "file.go") || !valid {
        t.Fatalf("stripped = %q, %v", got, valid)
    }

    a.sidebarPathPrefix = []rune("wrong/")
    got, valid = a.currentSidebarPath()
    if got != child || valid {
        t.Fatalf("invalid prefix = %q, %v", got, valid)
    }
}
```

Also cover no selected node (`"", true`), the project root (`"."` in relative mode), an empty prefix, and a prefix equal to the whole converted path (valid with empty output). Update `TestSidebarRect` so the tree height is `a.height-1-sidebarPathPanelHeight`, and assert `sidebarPathRect` starts immediately below it, has width `sidebarWidth-1`, has height `sidebarPathPanelHeight`, and collapses when the sidebar/tree is unavailable.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app -run 'Test(CurrentSidebarPath|SidebarRect|SidebarPathRect)' -count=1`

Expected: FAIL because the new state, constants, and helpers do not exist and the old sidebar rectangle still consumes the entire sidebar.

- [ ] **Step 3: Add App state and split layout**

Add this state to `App` near the existing sidebar fields:

```go
sidebarPathType          sidebarPathType
sidebarPathPrefix        []rune
sidebarPathPrefixCursor  int
sidebarPathPrefixScroll  int
sidebarPathPrefixFocused bool
sidebarPathLastCopied    string
```

Define the zero-value-relative enum and compact height in `sidebarpath.go`:

```go
type sidebarPathType uint8

const (
    sidebarPathRelative sidebarPathType = iota
    sidebarPathAbsolute
    sidebarPathPanelHeight = 8
)
```

Change `sidebarRect()` to return only the tree portion of the full `a.height-1` sidebar content. Add `sidebarPathRect()` returning the bottom eight rows only when `sidebarShown`, `tree != nil`, and the available sidebar height is at least `sidebarPathPanelHeight`; otherwise return a zero rectangle. Keep both rectangles one column narrower than `sidebarWidth` so the splitter owns the final column.

- [ ] **Step 4: Implement exact path computation and drawing**

Implement:

```go
func (a *App) currentSidebarPath() (string, bool) {
    if a.tree == nil || a.tree.SelectedPath == "" {
        return "", true
    }
    value := a.relativePathFor(a.tree.SelectedPath)
    if a.sidebarPathType == sidebarPathAbsolute {
        value = absolutePathFor(a.tree.SelectedPath)
    }
    prefix := string(a.sidebarPathPrefix)
    if prefix == "" {
        return value, true
    }
    if !strings.HasPrefix(value, prefix) {
        return value, false
    }
    return strings.TrimPrefix(value, prefix), true
}
```

Draw an unframed eight-row panel with `SidebarBG`: a top separator/title row, `Path type`, one radio row containing `(x) Relative  ( ) Absolute` or its selected inverse, `Strip prefix`, a one-row input, `Current path`, and two rows for a terminal-cell-aware tail-preserving representation of the current value. Use `drawTextInput` for the input; use `theme.Error` only for the input foreground when `currentSidebarPath` reports an invalid prefix. Never call clipboard code from drawing.

- [ ] **Step 5: Add rendering regression tests and pass Task 1**

Use `tcell.SimulationScreen` to assert labels and selected radio text appear inside the panel, no panel text appears in the tree rectangle or status row, invalid prefix input cells use `theme.Error`, valid input cells do not, and a long path stays within the final two rows. Assert repeated `draw()` calls make no clipboard writes.

Run: `gofmt -w internal/app/app.go internal/app/app_test.go internal/app/sidebarpath.go internal/app/sidebarpath_test.go && go test ./internal/app -run 'Test(CurrentSidebarPath|SidebarRect|SidebarPath|DrawSidebarPath)' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit Task 1**

```bash
git add internal/app/app.go internal/app/app_test.go internal/app/sidebarpath.go internal/app/sidebarpath_test.go
git commit -m "Add sidebar path information panel"
```

---

### Task 2: Mouse, Keyboard, Paste, And Clipboard Integration

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/sidebarpath.go`
- Modify: `internal/app/sidebarpath_test.go`
- Modify: `internal/app/externalfile_test.go`
- Modify: `internal/app/fileops.go`
- Modify: `internal/app/finder_test.go`

**Interfaces:**
- Consumes: Task 1's `sidebarPathRect`, `currentSidebarPath`, state fields, and drawing.
- Produces: `handleSidebarPathMouse(int, int) bool`, `handleSidebarPathKey(*tcell.EventKey)`, `copyCurrentSidebarPath(bool)`, and tree-only hit/scroll/reveal behavior.

- [ ] **Step 1: Write failing interaction and clipboard tests**

Add tests proving all of these behaviors:

```go
// Tree-node selection is the only selection source for this feature.
// Explicit left and right clicks force one clipboard write, including a
// repeated click on the same node. Drawing and tab synchronization write none.
// Mode switches and prefix edits write the new computed value once.
// A clipboard failure leaves sidebarPathLastCopied unchanged and flashes
// "Copy failed: <error>" so a later change can retry.
```

Cover clicks for root, folder, text file, and unsupported file; the unsupported file must be copied before its confirmation opens and must not create a tab. Cover radio clicks, input focus and CJK cursor placement, Rune/Left/Right/Home/End/Backspace/Delete, `Esc`/`Enter`/`Tab` blur, editor immutability while focused, authorized bracketed paste into the prefix, no copy for cursor-only keys, and no duplicate clipboard write when an edit leaves the derived value unchanged. Cover wheel events in the tree rectangle versus panel rectangle and right-clicking the panel versus a tree row.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app -run 'TestSidebarPath.*(Mouse|Key|Paste|Copy|Wheel)|TestSidebarClick.*Copies|TestTreeContextClick.*Copies' -count=1`

Expected: FAIL because interaction routing and event-scoped copying are not implemented.

- [ ] **Step 3: Implement event-scoped copying**

Implement `copyCurrentSidebarPath(force bool)` with these rules:

```go
value, _ := a.currentSidebarPath()
if a.tree == nil || a.tree.SelectedPath == "" || (!force && value == a.sidebarPathLastCopied) {
    return
}
copyToSystem := a.copyToSystem
if copyToSystem == nil {
    copyToSystem = clipboard.CopyToSystem
}
if err := copyToSystem(value); err != nil {
    a.flash(fmt.Sprintf("Copy failed: %v", err))
    return
}
a.sidebarPathLastCopied = value
```

Do not mutate `clipBuf` and do not show a success toast for every prefix keystroke. Call with `force=true` immediately after `SelectedPath` is set in `sidebarClick` and `tryTreeContextClick`, before an unsupported-file confirmation can open. Call with `force=false` only after a path-type or prefix-content change.

- [ ] **Step 4: Route panel mouse and keyboard events**

Route panel clicks before tree hit-testing. Clicking Relative/Absolute sets the enum, blurs the prefix field, and copies when the mode changes. Clicking the input row focuses it and sets the rune cursor via `textInputCursorAt`; clicking elsewhere blurs it. Information-panel right clicks must not call `Tree.HitTest` or open a tree context menu.

In `handleKey`, after true modal handlers but before Esc-leader logic, send all keys to `handleSidebarPathKey` when the prefix is focused. Printable runes and edit keys update the rune slice and call `copyCurrentSidebarPath(false)` only when content changed. `Esc`, `Enter`, and `Tab` blur without changing/copying; `Esc` must also clear `lastEscape`. All other keys are absorbed so they cannot edit the active buffer.

- [ ] **Step 5: Route authorized paste and confine tree geometry**

In `routePastedText`, after modal/menu branches but before editor insertion, normalize an authorized payload with `normalizeSingleLinePaste`, insert it into `sidebarPathPrefix`, and copy the recomputed value once when the prefix is focused.

Make `scrollAt` scroll the tree only when `(x,y)` is inside `sidebarRect`; wheel events in the path panel do nothing. Continue passing the now-smaller `sidebarRect` height to `Tree.Render`, `Tree.Reveal` in `openFileWithExternalPolicy`, and `Tree.Reveal` in `doCreateFolder`. Update helper tests that scan rendered tree rows so they only scan `sidebarRect`.

- [ ] **Step 6: Pass interaction tests and commit Task 2**

Run: `gofmt -w internal/app/app.go internal/app/sidebarpath.go internal/app/sidebarpath_test.go internal/app/externalfile_test.go internal/app/fileops.go internal/app/finder_test.go && go test ./internal/app -count=1`

Expected: PASS.

```bash
git add internal/app/app.go internal/app/sidebarpath.go internal/app/sidebarpath_test.go internal/app/externalfile_test.go internal/app/fileops.go internal/app/finder_test.go
git commit -m "Wire sidebar path controls to tree selection"
```

---

### Task 3: Documentation And Full Validation

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: completed sidebar path panel and its exact English labels.
- Produces: user-facing behavior documentation and a validated binary at `bin/spiceedit`.

- [ ] **Step 1: Document the feature**

Extend the file-tree section with this exact behavior, edited into the surrounding prose:

```markdown
The compact **PATH INFO** panel below the tree follows the exact node clicked
with either mouse button. **Relative** (the default) copies a path rooted at
the opened project directory; **Absolute** copies the full filesystem path.
**Strip prefix** removes a matching literal prefix from **Current path**. An
invalid prefix turns red and leaves the unstripped path intact. Selecting a
node or changing either control automatically copies the full current value
to the system clipboard, even when the sidebar shortens its on-screen display.
```

- [ ] **Step 2: Run formatting, race tests, build, and diff checks**

Run:

```bash
gofmt -w internal/app/app.go internal/app/app_test.go internal/app/sidebarpath.go internal/app/sidebarpath_test.go internal/app/externalfile_test.go internal/app/fileops.go internal/app/finder_test.go
go test -race -count=1 ./...
make build
git diff --check
```

Expected: all tests pass, `./bin/spiceedit` is rebuilt, and `git diff --check` emits no output. Do not stage `bin/spiceedit`; `bin/` is a validation artifact for this branch.

- [ ] **Step 3: Commit documentation**

```bash
git add README.md docs/superpowers/plans/2026-09-03-sidebar-path-info-panel.md
git commit -m "Document sidebar path information"
```
