# Tree Folder Copy Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable `Copy relative path` and `Copy absolute path` in the `Esc Esc` menu for an explicitly selected project-tree folder while preserving file-only action guards.

**Architecture:** Add a path-action resolver separate from the existing file and folder resolvers. An explicit tree selection is authoritative and may name any existing filesystem node; only an empty selection or tree-less mode falls back to an active file-backed tab.

**Tech Stack:** Go, tcell app tests, existing clipboard injection.

## Global Constraints

- Open with default app, Rename file, and Delete file remain file-only.
- Rename folder and Delete folder remain non-root directory-only.
- Copy relative path and Copy absolute path accept selected files, folders, project root, and symlinks.
- A stale explicit selection disables copy actions and never redirects to an unrelated tab.
- Main-worktree uncommitted files remain untouched.

---

### Task 1: Folder-Aware Main-Menu Path Copy

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/fileops.go`
- Modify: `internal/app/fileops_test.go`
- Modify: `internal/app/externalfile_test.go`
- Modify: `README.md`

**Interfaces:**
- Produces: `App.pathActionTarget() (string, bool)` and `App.hasPathActionTarget() bool`.
- Consumes: `Tree.SelectedPath`, `App.fileActionTarget`, and existing path-copy actions.

- [x] **Step 1: Add failing resolver and menu tests**

Assert that a selected folder and project root enable both copy rows while file-specific rows remain disabled. Cover selected file, directory symlink, broken symlink, stale path, empty selection with active tab, tree-less active tab, and no target.

- [x] **Step 2: Add failing action tests**

Use injected clipboard output to prove selected folder relative and absolute paths are copied, including `.` for the project root. Prove an unrelated active tab is never copied while a tree node is explicitly selected.

- [x] **Step 3: Implement the path resolver and wire the menu**

Use `os.Lstat` for a non-empty `Tree.SelectedPath`, accepting any existing node without following symlinks. Return no target on error. When there is no explicit tree selection, delegate to `fileActionTarget`. Use the new resolver for the two menu predicates and action callbacks only.

- [x] **Step 4: Update README and validate**

Clarify that path-copy actions apply to exact selected files or folders in both menus. Run `gofmt`, focused tests, `go test -race -count=1 ./...`, `make build`, and `git diff --check`.
