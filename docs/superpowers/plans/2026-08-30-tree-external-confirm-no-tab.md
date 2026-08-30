# Tree External Confirmation Without Placeholder Tabs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep the active editor tab unchanged when an unsupported file is selected in the left tree, while retaining inline text/image tabs and the existing default-application confirmation.

**Architecture:** Extend the existing source-aware external-open policy so the tree confirmation path can classify a candidate tab without appending or activating external placeholders. `editor.NewTab` remains the classification authority; text and supported images continue through the current tab lifecycle, while external candidates are discarded after opening the existing confirmation modal.

**Tech Stack:** Go, tcell simulation tests, existing editor file classification and asynchronous default-app opener.

**Spec:** `docs/superpowers/specs/2026-08-30-tree-external-confirm-no-tab-design.md`

## Global Constraints

- Apply the no-placeholder behavior only to left file-tree clicks.
- UTF-8 text files continue to create or activate editable tabs.
- Supported PNG, JPEG, and GIF files continue to create or activate image-preview tabs.
- Other files show `Open with default app?` with **No** selected without creating or activating a tab.
- Choosing Yes, No, Esc, or outside-click cancellation leaves the active tab unchanged.
- Existing non-tree entry points retain immediate external opening and placeholder-tab behavior.
- Keep file classification in `internal/editor`; do not duplicate extension or byte-sniffing rules in `internal/app`.

---

### Task 1: Confirm Tree External Files Without Tabs

**Files:**
- Modify: `internal/app/app.go:1932-1949,2250-2350`
- Test: `internal/app/externalfile_test.go`
- Modify: `README.md:233-248`

**Interfaces:**
- Consumes: `editor.NewTab(path) (*editor.Tab, error)`, `(*editor.Tab).IsExternal() bool`, and the existing `App.openConfirm`/`App.openPathWithDefaultApp` flow.
- Produces: `externalOpenConfirmWithoutTab` policy behavior inside `App.openFileWithExternalPolicy`.

- [ ] **Step 1: Rewrite the external tree regression tests to express the new behavior**

Update `TestSidebarClick_ExternalRequiresConfirmation` so the first tree click
asserts that `len(a.tabs) == 0` and `a.activeTabPtr() == nil`, while retaining
the confirmation-title/message/default-No and delayed-opener assertions. After
choosing Yes and consuming the completion event, assert again that no tab was
created.

Replace the cancellation/repeat assertions with a seeded text tab:

```go
a.openFile(textPath)
original := a.activeTabPtr()

clickTreePath(t, a, target)
a.confirmCancel()
if len(a.tabs) != 1 || a.activeTabPtr() != original {
	t.Fatalf("cancel changed tabs: count=%d active=%#v want=%#v", len(a.tabs), a.activeTabPtr(), original)
}

clickTreePath(t, a, target)
if !a.confirmOpen || len(a.tabs) != 1 || a.activeTabPtr() != original {
	t.Fatalf("repeat changed editor state: confirm=%v tabs=%d active=%#v", a.confirmOpen, len(a.tabs), a.activeTabPtr())
}
```

Rename `TestSidebarClick_TextAndImageStayInline` to
`TestSidebarClick_TextAndImageOpenTabs` and assert the text click creates one
editable tab, the image click creates a second image tab, and neither click
opens confirmation or invokes the external opener.

Add `TestSidebarClick_ExistingExternalTabIsNotActivated`:

```go
a.openFile(target)
if got := <-opened; got != target {
	t.Fatalf("initial opened path = %q, want %q", got, target)
}
a.handleEvent(waitForDefaultOpenDone(t, a))
a.openFile(textPath)
original := a.activeTabPtr()

clickTreePath(t, a, target)
if !a.confirmOpen {
	t.Fatal("tree click did not open confirmation")
}
if len(a.tabs) != 2 || a.activeTabPtr() != original {
	t.Fatalf("tree click activated external placeholder: tabs=%d active=%#v", len(a.tabs), a.activeTabPtr())
}
assertDefaultAppNotOpened(t, opened)
```

- [ ] **Step 2: Run the focused tests and verify failure**

Run:

```bash
go test ./internal/app -run 'TestSidebarClick_(External|TextAndImage|ExistingExternal)' -count=1
```

Expected: the external tests fail because the current tree path appends and
activates a placeholder tab before opening confirmation.

- [ ] **Step 3: Implement the confirmation-without-tab policy**

Rename the tree-specific policy constant and use it from `sidebarClick`:

```go
const (
	externalOpenImmediate externalOpenPolicy = iota
	externalOpenConfirmWithoutTab
)
```

In the existing-tab loop, handle external placeholders before assigning
`a.activeTab`:

```go
for i, t := range a.tabs {
	if t.Path != path {
		continue
	}
	if t.IsExternal() && policy == externalOpenConfirmWithoutTab {
		a.handleExternalFileOpen(t.Path, policy)
		return
	}
	a.activeTab = i
	if t.IsExternal() {
		a.handleExternalFileOpen(t.Path, policy)
		return
	}
	if !t.IsReadOnly() {
		a.reopenTextTab(t)
	}
	return
}
```

After `editor.NewTab(path)` succeeds, intercept an external candidate before
appending it:

```go
if t.IsExternal() && policy == externalOpenConfirmWithoutTab {
	a.handleExternalFileOpen(t.Path, policy)
	return
}
a.tabs = append(a.tabs, t)
a.activeTab = len(a.tabs) - 1
```

Update `handleExternalFileOpen` to test the renamed policy. Keep the existing
modal title, message, default focus, callback, and immediate policy unchanged.
Update comments to state that the tree policy does not create or activate an
external placeholder.

- [ ] **Step 4: Update README behavior**

Replace the first paragraph under `### File types and default applications`
with:

```markdown
SpiceEdit edits UTF-8 text files and previews PNG, JPEG, and the first
frame of GIF images inside the terminal. From the left file tree, text files
open editable tabs and supported images open preview tabs. Clicking a PDF,
office document, archive, database, media file, executable, or other
unsupported file keeps the current editor tab unchanged and asks whether to
launch the system default application. Choosing No closes the prompt without
creating a tab or launching another application.
```

Keep the existing explicit `Esc o`, file-manager, and SSH behavior paragraphs.

- [ ] **Step 5: Format and run focused verification**

Run:

```bash
gofmt -w internal/app/app.go internal/app/externalfile_test.go
go test ./internal/app -run 'TestSidebarClick_(External|TextAndImage|ExistingExternal)' -count=1
git diff --check
```

Expected: all focused tests and whitespace checks pass.

- [ ] **Step 6: Run repository and platform validation**

Run:

```bash
make test
CGO_ENABLED=0 go test ./internal/app -count=1
make build
make build-linux
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
```

Expected: all tests and builds pass, and `bin/spiceedit` is produced.

- [ ] **Step 7: Commit the implementation**

```bash
git add README.md internal/app/app.go internal/app/externalfile_test.go
git commit -m "Keep external tree files out of tabs"
```
