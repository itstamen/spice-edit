# Shared External File Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the left-tree confirmation dialog for unsupported non-text files with one reusable editor tab that shows the selected filename and a clickable default-application action.

**Architecture:** Keep text and supported-image behavior unchanged. Continue using `editor.NewTab` as the only file classifier, render external-file content and its button geometry inside `internal/editor`, and let `internal/app` own tab reuse plus asynchronous default-application launching.

**Tech Stack:** Go, tcell simulation screens, the existing editor tab model, and same-package Go tests.

## Global Constraints

- Only unsupported files classified as `external` use the shared external-file tab; supported PNG, JPEG, and GIF files retain inline image previews.
- At most one external-file tab is created by normal open flows, and selecting a different external file replaces that tab's displayed path.
- A left file-tree click never opens a confirmation dialog and never launches the default application automatically.
- The editor surface displays the selected basename, including its extension, and a clickable `[ Open with default app ]` action.
- The existing asynchronous opener reports completion through the app event loop.
- Do not add dependencies or `Ctrl+` shortcuts.

---

### Task 1: External-File Surface And Hit Testing

**Files:**
- Modify: `internal/editor/external.go`
- Modify: `internal/editor/external_test.go`

**Interfaces:**
- Consumes: `Tab.Path`, `termtext.StringWidth`, `termtext.DrawString`, and `theme.Theme`.
- Produces: `Tab.RenderExternal(...)` with filename/action content and `Tab.ExternalOpenHitTest(px, py, x, y, w, h int) bool`.

- [x] **Step 1: Write failing rendering and hit-test tests**

Add simulation-screen assertions that `/tmp/manual.pdf` renders both `manual.pdf` and `[ Open with default app ]`. Compute the editor rectangle used by the test and assert that the action row's first, middle, and last visible cells hit while the filename row and adjacent cells miss.

- [x] **Step 2: Run the focused editor tests**

Run: `go test ./internal/editor -run 'TestRenderExternal|TestExternalOpenHitTest' -count=1`

Expected: FAIL because the current renderer contains only the old unsupported-file message and no hit-test API exists.

- [x] **Step 3: Implement one shared layout calculation**

Add a private layout helper that vertically centers a filename row and a bracketed action row, clips both to the supplied terminal rectangle, and returns the visible action bounds. Make both rendering and `ExternalOpenHitTest` consume that helper so drawn and clickable cells cannot drift apart. Render the filename with `theme.Text` and bold emphasis, and render the action with `theme.Accent`.

- [x] **Step 4: Format and pass the editor tests**

Run: `gofmt -w internal/editor/external.go internal/editor/external_test.go && go test ./internal/editor -run 'TestRenderExternal|TestExternalOpenHitTest' -count=1`

Expected: PASS.

---

### Task 2: Shared Tab And Click Behavior

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/externalfile_test.go`

**Interfaces:**
- Consumes: `editor.NewTab`, `Tab.IsExternal`, `Tab.ExternalOpenHitTest`, and `App.openPathWithDefaultApp`.
- Produces: left-tree external selection without a modal, one external tab reused across paths, and editor-button activation.

- [x] **Step 1: Replace confirmation expectations with failing shared-tab tests**

Assert that the first external tree click creates and activates an external tab without opening the confirmation or opener. Click a second external path and assert the tab count is unchanged, the same tab position now displays the second path, and text tabs retain their order. Drive a left mouse press over the rendered action and assert the injected opener receives exactly the active external path; presses outside the action must not launch it.

- [x] **Step 2: Run the focused app tests**

Run: `go test ./internal/app -run 'TestSidebarClick_External|TestExternalEditorAction|TestOpenFile_External' -count=1`

Expected: FAIL against the old confirmation policy and per-path external tabs.

- [x] **Step 3: Implement the shared external-tab policy**

Replace `externalOpenConfirmWithoutTab` with a tree policy that displays but does not launch. After classification, activate an exact existing path; otherwise replace the first external tab with the new external candidate or append it when none exists. Keep immediate non-tree opens asynchronous, but route them through the same shared-tab insertion helper. Special-case external tabs in `editorPress`: use `ExternalOpenHitTest` and call `openPathWithDefaultApp` only for a hit.

- [x] **Step 4: Format and pass package tests**

Run: `gofmt -w internal/app/app.go internal/app/externalfile_test.go && go test ./internal/app ./internal/editor -count=1`

Expected: PASS.

---

### Task 3: Documentation And Validation

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: the completed shared external-file behavior.
- Produces: accurate file-opening documentation and a verified build.

- [x] **Step 1: Update the file-opening documentation**

State that tree-selected unsupported files share one read-only editor tab, that selecting another unsupported file replaces its displayed filename, and that `[ Open with default app ]` performs the explicit host-local handoff. Retain the existing SSH host-local caveat.

- [x] **Step 2: Run repository validation**

Run: `go test -race ./...`

Expected: PASS.

Run: `make build`

Expected: PASS and `bin/spiceedit` is produced as the ignored build artifact.

- [x] **Step 3: Review the final diff**

Run: `git diff --check && git status --short`

Expected: no whitespace errors; only the implementation, tests, plan, existing user changes, and ignored build output are present.
