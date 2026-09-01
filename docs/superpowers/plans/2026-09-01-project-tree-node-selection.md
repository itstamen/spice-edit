# Project Tree Node Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every clicked project-tree node a single, persistent yellow-and-bold selection and make all requested file actions available from unsupported-file context menus without opening an editor tab.

**Architecture:** Add an explicit selected-path field to `filetree.Tree`, keeping selection separate from the app's active working folder and active editor tab. App click and tab synchronization paths update that selection deliberately. Tree context actions continue capturing the clicked node directly, with a new default-application action for files.

**Tech Stack:** Go, tcell simulation screens, existing `internal/app`, `internal/filetree`, and clipboard/default-opener abstractions.

## Global Constraints

- Text files continue opening editable tabs.
- Supported images continue opening preview tabs.
- Unsupported files selected from the tree do not create or activate tabs and keep the existing confirmation flow.
- Both left-click and right-click select the exact tree node clicked.
- The selected node is yellow and bold even when it has Git status coloring.
- Unsupported-file context menus expose Open with default app, Rename file, Delete file, Copy relative path, and Copy absolute path.
- Existing uncommitted files in the main worktree are out of scope and must remain untouched.

---

### Task 1: Independent Tree Selection and Rendering

**Files:**
- Modify: `internal/filetree/filetree.go`
- Modify: `internal/filetree/filetree_test.go`
- Modify: `internal/theme/theme.go`
- Modify: `internal/theme/theme_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/externalfile_test.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Produces: `Tree.SelectedPath string`, the single source used to render the selected row, and `Theme.TreeSelected`, its semantic yellow foreground.
- Consumes: existing `Tree.ActiveFolder`, `Tree.ActiveFile`, `App.sidebarClick`, `App.tryTreeContextClick`, and tab synchronization behavior.

- [x] **Step 1: Add failing rendering tests**

Add simulation-screen tests proving that `SelectedPath` makes a file or folder use `theme.TreeSelected` and bold, that Git status cannot override the selection color, and that only the exact selected node is highlighted. Add a theme test requiring a non-default `TreeSelected` color.

- [x] **Step 2: Run the focused tests and verify failure**

Run: `go test ./internal/filetree -run 'TestRender_Selected' -count=1`

Expected: FAIL because `Tree.SelectedPath` and the new selection behavior do not exist.

- [x] **Step 3: Implement selected-path rendering**

Add `SelectedPath` to `Tree` and the Tokyo Night yellow `TreeSelected` theme color. Render the root and child row whose path equals `SelectedPath` with `theme.TreeSelected` and bold after applying dotfile and Git-status styles. Preserve a compatibility fallback to the prior active file/folder fields when no explicit selection exists. Apply the selected color to an enabled Nerd Font glyph as well as the row label.

- [x] **Step 4: Add failing app interaction tests**

Add tests proving left-click and right-click select folders, text files, images, and unsupported files; unsupported selection remains after confirmation cancellation and leaves the current tab unchanged.

- [x] **Step 5: Implement app selection synchronization**

Set the exact node path before processing a tree click. Right-click selection must not open or activate tabs. File opens and tab switches update selection to the active file; when no tab remains, select the active folder. Rename a selected path along with the filesystem node, and move selection to the surviving active folder when deletion removes the selected path or one of its ancestors.

- [x] **Step 6: Run focused tests**

Run: `go test ./internal/filetree ./internal/app -run 'Selected|SidebarClick|TreeContext' -count=1`

Expected: PASS.

### Task 2: Unsupported-File Context Actions and Documentation

**Files:**
- Modify: `internal/app/modals.go`
- Modify: `internal/app/fileops.go`
- Modify: `internal/app/modals_test.go`
- Modify: `internal/app/externalfile_test.go`
- Modify: `README.md`

**Interfaces:**
- Produces: `ctxOpenWithDefaultApp(*App, *filetree.Node)`.
- Consumes: existing `App.openPathWithDefaultApp`, `ctxRename`, `ctxDelete`, `ctxCopyRelativePath`, and `ctxCopyAbsolutePath`.

- [x] **Step 1: Add failing context-menu tests**

Assert that a file context menu contains the exact requested labels in usable form and that Open with default app invokes the opener for the clicked unsupported file without creating or activating a tab. Retain folder/root safety rules.

- [x] **Step 2: Run focused tests and verify failure**

Run: `go test ./internal/app -run 'TreeContext|Unsupported.*Context' -count=1`

Expected: FAIL because the file context menu lacks Open with default app.

- [x] **Step 3: Implement the node-targeted action**

Add Open with default app only for file nodes. Keep rename, delete, and copy actions captured against the clicked node so they remain usable even when that file has no editor tab. Use the requested full labels where the popup width permits them.

- [x] **Step 4: Update README**

Document exact-node yellow/bold selection and unsupported-file context actions without claiming that unsupported files create tabs.

- [x] **Step 5: Run full validation and build**

Run: `gofmt -w` on changed Go files, `make test`, `make build`, and `git diff --check`.

Expected: all tests pass and `bin/spiceedit` is generated.

### Task 3: Main Menu Targets the Selected Tree Node

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/fileops.go`
- Modify: `internal/app/externalfile_test.go`
- Modify: `internal/app/fileops_test.go`
- Modify: `internal/app/leader_test.go`
- Modify: `README.md`

**Interfaces:**
- Produces: `App.fileActionTarget() (string, bool)` and `App.hasFileActionTarget() bool`.
- Consumes: `Tree.SelectedPath`, the active tab fallback used in single-file mode, and the existing five file actions.

- [x] **Step 1: Add failing menu-target tests**

Assert that selecting an unsupported tree file enables Open with default app, Rename file, Delete file, Copy relative path, and Copy absolute path in the `Esc+Esc` menu with no tab or an unrelated active tab. Activate each action and prove it targets the selected node. Assert that selecting a folder disables file actions and enables only applicable folder actions.

- [x] **Step 2: Add fallback and shortcut tests**

Assert that `SelectedPath == ""` can fall back to an active file-backed tab for compatibility, `tree == nil` preserves single-file behavior, and `Esc+o` opens the selected unsupported tree file rather than an unrelated active tab.

- [x] **Step 3: Implement shared target resolution**

Add one resolver used by the five menu predicates and actions. A non-empty tree selection is authoritative: a selected existing file is the target; a selected directory, project root, or stale path yields no file target and must not fall back to an unrelated tab. With no explicit tree selection, or without a tree, retain the active file-backed tab fallback. Make folder-action enablement require an explicitly selected folder when a tree selection exists.

- [x] **Step 4: Update documentation and validate**

Update README to state that both the tree context menu and `Esc+Esc` file actions target the exact selected node. Run `gofmt`, focused tests, `go test -race -count=1 ./...`, `make build`, and `git diff --check`.
