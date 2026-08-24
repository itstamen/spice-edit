# Finder Active Folder Reveal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `Esc+e` follow the most recently selected project directory in
the left file tree while preserving active-file reveal behavior in single-file
mode.

**Architecture:** Add a deterministic App helper that resolves the file-manager
target from existing state. Project mode prefers `activeFolder`; without a file
tree the helper falls back to the active file, then the project root. Existing
asynchronous reveal and platform adapters remain unchanged.

**Tech Stack:** Go 1.24+, tcell v2.13.9, Go tests, Make.

## Global Constraints

- The authoritative spec is
  `docs/superpowers/specs/2026-08-24-finder-active-folder-reveal-design.md`.
- Project mode uses `activeFolder` with `selectFile=false` whenever it is
  non-empty.
- Single-file mode preserves the active file path with `selectFile=true`.
- Empty project context falls back to the active file, then `rootDir`.
- Existing asynchronous validation, status messages, and platform adapters
  remain unchanged.
- Finder AppleScript tab creation and reuse are out of scope.
- Do not modify or revert unrelated user files in the main worktree.
- Run `gofmt` on touched Go files and preserve repository comments/header
  conventions.

---

### Task 1: Resolve Reveal Target From Project Context

**Files:**
- Modify: `internal/app/reveal.go`
- Modify: `internal/app/reveal_test.go`
- Modify: `README.md:247-258`

**Interfaces:**
- Produces: `func (a *App) fileManagerRevealTarget() (string, bool)`.
- Preserves: `showInFileManager`, `revealPathInFileManager`, `pathRevealer`,
  and every platform adapter signature.

- [ ] **Step 1: Add failing target-resolution tests**

Add these tests to `internal/app/reveal_test.go` after
`TestMenuLayout_ShowInFileManager`:

```go
// TestFileManagerRevealTarget_ProjectFolderWins verifies a directory selected
// in the project tree remains authoritative even while a file tab is active.
func TestFileManagerRevealTarget_ProjectFolderWins(t *testing.T) {
    root := t.TempDir()
    selected := filepath.Join(root, "selected")
    if err := os.Mkdir(selected, 0o755); err != nil {
        t.Fatal(err)
    }
    file := filepath.Join(root, "notes.txt")
    if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
        t.Fatal(err)
    }
    tab, err := editor.NewTab(file)
    if err != nil {
        t.Fatal(err)
    }

    a := newTestApp(t, root)
    a.tabs = []*editor.Tab{tab}
    a.activeTab = 0
    a.setActiveFolder(selected)

    path, selectFile := a.fileManagerRevealTarget()
    if path != selected || selectFile {
        t.Fatalf("target = %q selectFile=%v, want %q false", path, selectFile, selected)
    }
}

// TestFileManagerRevealTarget_ProjectFileParent verifies opening a project
// file makes its parent directory the Finder target.
func TestFileManagerRevealTarget_ProjectFileParent(t *testing.T) {
    root := t.TempDir()
    parent := filepath.Join(root, "docs")
    if err := os.Mkdir(parent, 0o755); err != nil {
        t.Fatal(err)
    }
    file := filepath.Join(parent, "guide.md")
    if err := os.WriteFile(file, []byte("guide"), 0o644); err != nil {
        t.Fatal(err)
    }

    a := newTestApp(t, root)
    a.openFile(file)

    path, selectFile := a.fileManagerRevealTarget()
    if path != parent || selectFile {
        t.Fatalf("target = %q selectFile=%v, want %q false", path, selectFile, parent)
    }
}

// TestFileManagerRevealTarget_SingleFileUsesActiveFile preserves file-selection
// behavior when no project tree exists.
func TestFileManagerRevealTarget_SingleFileUsesActiveFile(t *testing.T) {
    root := t.TempDir()
    file := filepath.Join(root, "notes.txt")
    if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
        t.Fatal(err)
    }
    tab, err := editor.NewTab(file)
    if err != nil {
        t.Fatal(err)
    }

    a := newTestApp(t, root)
    a.tree = nil
    a.tabs = []*editor.Tab{tab}
    a.activeTab = 0

    path, selectFile := a.fileManagerRevealTarget()
    if path != file || !selectFile {
        t.Fatalf("target = %q selectFile=%v, want %q true", path, selectFile, file)
    }
}

// TestFileManagerRevealTarget_Fallbacks covers an empty project context and
// the final project-root fallback.
func TestFileManagerRevealTarget_Fallbacks(t *testing.T) {
    root := t.TempDir()
    file := filepath.Join(root, "notes.txt")
    if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
        t.Fatal(err)
    }
    tab, err := editor.NewTab(file)
    if err != nil {
        t.Fatal(err)
    }

    a := newTestApp(t, root)
    a.activeFolder = ""
    a.tabs = []*editor.Tab{tab}
    a.activeTab = 0
    path, selectFile := a.fileManagerRevealTarget()
    if path != file || !selectFile {
        t.Fatalf("file fallback = %q selectFile=%v, want %q true", path, selectFile, file)
    }

    a.tabs = nil
    a.activeTab = -1
    path, selectFile = a.fileManagerRevealTarget()
    if path != root || selectFile {
        t.Fatalf("root fallback = %q selectFile=%v, want %q false", path, selectFile, root)
    }
}
```

Update `TestShowInFileManager_UsesActiveFilePath` so every subtest sets
`a.tree = nil`; rename it to
`TestShowInFileManager_SingleFileUsesActiveFilePath` and update its comment to
state that it covers single-file mode.

Update `TestShowInFileManager_FallsBackToProjectRoot` by removing the
`not-the-target` assignment and setting `a.activeFolder = ""`; update its
comment to state that it covers empty project context.

- [ ] **Step 2: Run the focused tests and verify red**

Run:

```sh
go test ./internal/app -run 'TestFileManagerRevealTarget|TestShowInFileManager' -count=1
```

Expected: FAIL because `fileManagerRevealTarget` does not exist and the current
`showInFileManager` still ignores project context.

- [ ] **Step 3: Implement target resolution**

Replace the current `showInFileManager` block in `internal/app/reveal.go` with:

```go
// fileManagerRevealTarget resolves the directory or file represented by the
// current project context. Project mode follows the active tree folder;
// single-file mode preserves file-selection behavior.
func (a *App) fileManagerRevealTarget() (string, bool) {
    if a.tree != nil && a.activeFolder != "" {
        return a.activeFolder, false
    }
    if tab := a.activeTabPtr(); tab != nil && tab.Path != "" {
        return tab.Path, true
    }
    return a.rootDir, false
}

// showInFileManager opens the current project folder or selects the active
// file when SpiceEdit is running without a project tree.
func (a *App) showInFileManager() {
    path, selectFile := a.fileManagerRevealTarget()
    a.revealPathInFileManager(path, selectFile)
}
```

- [ ] **Step 4: Run focused tests and verify green**

Run:

```sh
gofmt -w internal/app/reveal.go internal/app/reveal_test.go
go test ./internal/app -run 'TestFileManagerRevealTarget|TestShowInFileManager' -count=1
```

Expected: PASS.

- [ ] **Step 5: Update README behavior**

Replace the `Esc e` paragraph in `README.md` with:

```markdown
Press `Esc e` or choose **Show in file manager** to open the current project
context in macOS Finder or Windows Explorer. In project mode, the action
follows the left file tree: clicking a directory or the project root opens that
directory, while clicking a file opens its containing directory. The selected
tree context remains active even when a file tab is still visible. In
single-file mode, SpiceEdit selects the active file instead.

On macOS, SpiceEdit searches every open Finder window and first activates an
existing tab for the target directory. When the folder appears in multiple
windows, it reuses the tab in Finder's frontmost window. When no matching tab
exists, it opens the folder in a new tab of an existing Finder window; when
Finder has no open window, SpiceEdit opens one. This requires Accessibility
permission for the terminal application running SpiceEdit. Windows uses the
same project-directory context and continues to select the active file in
single-file mode. If no tree context or file path is available, SpiceEdit opens
the project root. This action is not supported on Linux.
```

- [ ] **Step 6: Run task validation**

Run:

```sh
go test ./internal/app -count=1
git diff --check
```

Expected: PASS with changes limited to `reveal.go`, `reveal_test.go`, and
`README.md`.

- [ ] **Step 7: Commit the behavior change**

Run:

```sh
git add internal/app/reveal.go internal/app/reveal_test.go README.md
git commit -m "Follow active folder when opening Finder"
```

### Task 2: Full Validation And Binary Build

**Files:**
- Generate ignored artifact: `bin/spiceedit`
- No tracked source changes.

**Interfaces:**
- Consumes: Task 1's target resolver.
- Produces: tested macOS arm64 binary for manual `Esc+e` verification.

- [ ] **Step 1: Run full automated validation**

Run:

```sh
make test
CGO_ENABLED=0 go test ./internal/app -count=1
git diff --check
```

Expected: all commands pass.

- [ ] **Step 2: Build and inspect the binary**

Run:

```sh
make build
file ./bin/spiceedit
go version -m ./bin/spiceedit
otool -L ./bin/spiceedit
```

Expected: macOS arm64, `CGO_ENABLED=1`, and ApplicationServices/CoreGraphics
linked.

- [ ] **Step 3: Confirm repository state**

Run:

```sh
git status --short --branch
git log --oneline -3
```

Expected: no tracked changes after the Task 1 commit; `bin/spiceedit` remains
ignored.

## Plan Self-Review

- **Spec coverage:** Task 1 implements every project/single-file priority rule,
  updates documentation, and leaves platform adapters unchanged. Task 2 runs
  full tests and produces the requested binary.
- **Placeholder scan:** Every edit step includes exact code, commands, and
  expected outcomes.
- **Type consistency:** The helper signature, tests, and `showInFileManager`
  call site all use `(string, bool)` with the boolean meaning `selectFile`.
