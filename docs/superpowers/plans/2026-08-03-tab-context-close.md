# Tab Context Close Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a tab-only right-click menu that closes the selected tab, every other tab, or all tabs without losing unsaved work.

**Architecture:** Generalize the existing file-tree context menu so rows capture closure actions and optional enabled predicates. Add pointer-based tab-close helpers that apply one target snapshot and one final active-tab/tree update. Hit-test Button3 against `lastTabRects`, activate the target tab, and open the shared popup with callbacks into the tested close helpers.

**Tech Stack:** Go 1.24, tcell mouse events and simulation screens, existing `editor.Tab`, existing context and dirty-close modals, Go tests, Make targets.

## Global Constraints

- Show `Close tab`, `Close other tabs`, and `Close all tabs` only when Button3 hits a rendered editor tab.
- Right-clicking an inactive tab must activate it and update the file-tree active-file highlight before the menu opens.
- Right-clicking a tab's `×` cell opens the tab context menu and must not close the tab immediately.
- Preserve existing file-tree context menus and the main-menu fallback outside actual tab rectangles.
- Keep `Close other tabs` disabled when only one tab is open.
- Keep Cancel as the safe default for every unsaved-changes prompt.
- Batch Save must save only dirty target tabs and close no targets when any required save fails.
- Successful saves before a later failure remain saved and clean.
- Preserve a surviving active tab by pointer identity and synchronize the tree highlight only after the final close set is applied.
- Add no new keyboard shortcut and preserve the existing `Esc w` / main-menu close path.
- Preserve unrelated Kitty design files in the worktree and do not stage them.

---

### Task 1: Generalize the shared context menu

**Files:**
- Modify: `internal/app/app.go:465-471`
- Modify: `internal/app/modals.go:37-49, 52-73, 829-970`
- Modify: `internal/app/modals_test.go:20-55, 420-570, 750-840`

**Interfaces:**
- Produces: `contextItem{label string, action func(*App), enabled func(*App) bool}`.
- Produces: `contextItem.isEnabled(app *App) bool`.
- Preserves: `openTreeContext`, `contextActivate`, context keyboard/mouse handling, drawing, and placement.
- Consumed by: the tab context rows in Task 3.

- [x] **Step 1: Rewrite context-action tests for closures**

Remove `contextNode` setup and assert that a row closure runs once:

```go
func TestContextActivate_RunsClosureAndCloses(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := 0
	a.contextOpen = true
	a.contextItems = []contextItem{{
		label:  "x",
		action: func(*App) { called++ },
	}}

	a.contextActivate()

	if called != 1 {
		t.Fatalf("action calls = %d, want 1", called)
	}
	if a.contextOpen {
		t.Fatal("contextActivate should close the menu")
	}
}
```

Update `TestCloseAllModals_ClearsEverything`, `TestHandleContextKey`, and `TestHandleContextMouse_HoverAndClick` to use `func(*App)` actions and require only `contextItems` cleanup.

- [x] **Step 2: Add disabled-row regression tests**

Add keyboard and mouse cases proving a disabled row remains open and does not invoke its action:

```go
func TestContextActivate_DisabledIsNoop(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := false
	a.contextOpen = true
	a.contextItems = []contextItem{{
		label:   "disabled",
		action:  func(*App) { called = true },
		enabled: func(*App) bool { return false },
	}}

	a.contextActivate()

	if called || !a.contextOpen {
		t.Fatalf("called=%v contextOpen=%v, want false/true", called, a.contextOpen)
	}
}
```

Draw a disabled row on the simulation screen and assert its label foreground is `theme.Muted` and its background is not `theme.Selection` even when `contextHover` points to it.

- [x] **Step 3: Run the focused tests and confirm failure**

Run: `go test ./internal/app -run 'Test(Context|HandleContext|OpenTreeContext|CloseAllModals)'`

Expected: FAIL to compile because `contextItem.action` still requires `*filetree.Node`, and disabled rows are not consulted.

- [x] **Step 4: Replace node-bound rows with closure actions**

Change the row type and add a nil-means-enabled helper:

```go
type contextItem struct {
	label   string
	action  func(*App)
	enabled func(*App) bool
}

// isEnabled reports whether this context row may run.
func (item contextItem) isEnabled(a *App) bool {
	return item.enabled == nil || item.enabled(a)
}
```

Remove `contextNode` from `App` and `closeAllModals`. In `openTreeContext`, capture `n` while preserving existing tree action functions:

```go
items := []contextItem{}
add := func(label string, action func(*App, *filetree.Node)) {
	items = append(items, contextItem{
		label: label,
		action: func(app *App) {
			action(app, n)
		},
	})
}
```

Use `add` for `New File`, `Rename`, `Delete`, `Copy rel path`, and `Copy abs path` with the same visibility conditions and order as today.

- [x] **Step 5: Enforce enabled state in activation and drawing**

Keep disabled rows open and skip their callbacks:

```go
func (a *App) contextActivate() {
	if a.contextHover < 0 || a.contextHover >= len(a.contextItems) {
		return
	}
	item := a.contextItems[a.contextHover]
	if !item.isEnabled(a) {
		return
	}
	a.closeAllModals()
	if item.action != nil {
		item.action(a)
	}
}
```

In `drawContext`, compute `enabled := item.isEnabled(a)` and `hovered := enabled && i == a.contextHover`. Use the existing normal and hover styles for enabled rows and `a.theme.Muted` for the chevron and label of disabled rows.

- [x] **Step 6: Run context-menu tests**

Run: `go test ./internal/app -run 'Test(Context|HandleContext|OpenTreeContext|CloseAllModals)'`

Expected: PASS, including all existing tree menu label and activation cases.

- [x] **Step 7: Commit the generalized context menu**

```bash
git add internal/app/app.go internal/app/modals.go internal/app/modals_test.go
git commit -m "Generalize context menu actions"
```

---

### Task 2: Implement identity-based single and batch tab closing

**Files:**
- Create: `internal/app/tabclose_test.go`
- Modify: `internal/app/app.go:2142-2250`
- Test: `internal/app/modals_test.go:850-950`

**Interfaces:**
- Produces: `(*App).tabIndex(target *editor.Tab) int`.
- Produces: `(*App).tabsExcept(target *editor.Tab) []*editor.Tab`.
- Produces: `(*App).requestCloseTabs(targets []*editor.Tab)`.
- Produces: `(*App).saveDirtyTabs(targets []*editor.Tab) bool`.
- Produces: `(*App).closeTabs(targets []*editor.Tab)`.
- Updates: `requestCloseTab` and `closeTab` to resolve targets by pointer identity.
- Consumed by: the three tab-context rows in Task 3.

- [x] **Step 1: Add clean close-set and active-tab tests**

Create a helper that opens named text files and returns their tab pointers. Cover a `Close other tabs`-shaped target set:

```go
func TestRequestCloseTabs_CleanOtherTabsKeepsTarget(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt", "c.txt")
	a.activeTab = 1
	a.requestCloseTabs([]*editor.Tab{tabs[0], tabs[2]})

	if len(a.tabs) != 1 || a.tabs[0] != tabs[1] || a.activeTabPtr() != tabs[1] {
		t.Fatalf("tabs=%v active=%v, want only b active", a.tabs, a.activeTabPtr())
	}
}
```

Also close tabs before a surviving active tab and assert the same pointer remains selected after indices shift. Close the active tab in a mixed target set and assert the nearest right survivor wins, otherwise the nearest left survivor. Assert `tree.ActiveFile` matches the final active tab and becomes empty when all tabs close.

- [x] **Step 2: Add one-modal dirty batch tests**

Cover all modal actions:

- Cancel leaves every target open and dirty.
- Discard closes every target without changing on-disk contents.
- Save writes only dirty targets, leaves a non-target dirty tab untouched, and closes the complete target set after all target saves succeed.
- One dirty target uses `<name> has unsaved changes.`; multiple dirty targets use `<count> files have unsaved changes.`.

Drive the modal through `dirtyHover` and `dirtyActivate` so tests exercise the existing callback lifecycle.

- [x] **Step 3: Add save-failure atomicity coverage**

Use one dirty file-backed target followed by a dirty untitled target:

```go
untitled, err := editor.NewTab("")
if err != nil {
	t.Fatal(err)
}
untitled.InsertString("cannot save")
a.tabs = append(a.tabs, untitled)

a.requestCloseTabs([]*editor.Tab{fileTab, untitled})
a.dirtyHover = 2
a.dirtyActivate()
```

Assert both tabs remain open, the file-backed tab was written and is now clean, the untitled tab remains dirty, and status is `Saving untitled tabs is not supported yet`.

- [x] **Step 4: Run close tests and confirm failure**

Run: `go test ./internal/app -run 'Test(RequestCloseTabs|CloseTabs|RequestCloseTab)'`

Expected: FAIL because batch close helpers do not exist and single-tab callbacks still capture numeric indices.

- [x] **Step 5: Add pointer identity and target filtering**

```go
// tabIndex returns target's current index, or -1 when it is no longer open.
func (a *App) tabIndex(target *editor.Tab) int {
	for i, tab := range a.tabs {
		if tab == target {
			return i
		}
	}
	return -1
}

// tabsExcept snapshots every open tab except target in current tab order.
func (a *App) tabsExcept(target *editor.Tab) []*editor.Tab {
	out := make([]*editor.Tab, 0, len(a.tabs))
	for _, tab := range a.tabs {
		if tab != target {
			out = append(out, tab)
		}
	}
	return out
}

// openTargetTabs deduplicates targets and returns only tabs still open.
func (a *App) openTargetTabs(targets []*editor.Tab) []*editor.Tab {
	wanted := make(map[*editor.Tab]struct{}, len(targets))
	for _, tab := range targets {
		if tab != nil {
			wanted[tab] = struct{}{}
		}
	}
	out := make([]*editor.Tab, 0, len(wanted))
	for _, tab := range a.tabs {
		if _, ok := wanted[tab]; ok {
			out = append(out, tab)
		}
	}
	return out
}
```

- [x] **Step 6: Add the batch request and targeted save flow**

```go
func (a *App) requestCloseTabs(targets []*editor.Tab) {
	targets = a.openTargetTabs(targets)
	if len(targets) == 0 {
		return
	}
	if len(targets) == 1 {
		a.requestCloseTab(a.tabIndex(targets[0]))
		return
	}
	dirty := dirtyTabs(targets)
	if len(dirty) == 0 {
		a.closeTabs(targets)
		return
	}
	message := fmt.Sprintf("%d files have unsaved changes.", len(dirty))
	if len(dirty) == 1 {
		message = dirty[0].DisplayName() + " has unsaved changes."
	}
	a.openDirtyClose(
		"Unsaved changes",
		message,
		func(app *App) {
			if app.saveDirtyTabs(targets) {
				app.closeTabs(targets)
			}
		},
		func(app *App) { app.closeTabs(targets) },
	)
}

// dirtyTabs returns dirty tabs from tabs in the supplied order.
func dirtyTabs(tabs []*editor.Tab) []*editor.Tab {
	out := make([]*editor.Tab, 0, len(tabs))
	for _, tab := range tabs {
		if tab.Dirty {
			out = append(out, tab)
		}
	}
	return out
}

// tabPointerSet builds an identity set for target membership checks.
func tabPointerSet(tabs []*editor.Tab) map[*editor.Tab]struct{} {
	out := make(map[*editor.Tab]struct{}, len(tabs))
	for _, tab := range tabs {
		if tab != nil {
			out[tab] = struct{}{}
		}
	}
	return out
}

// saveDirtyTabs saves dirty targets in their current tab order.
func (a *App) saveDirtyTabs(targets []*editor.Tab) bool {
	targeted := tabPointerSet(targets)
	for i, tab := range a.tabs {
		if _, ok := targeted[tab]; !ok || !tab.Dirty {
			continue
		}
		if !a.saveTabAt(i) {
			return false
		}
	}
	return true
}
```

- [x] **Step 7: Apply each close set in one state transition**

Implement `closeTabs` with this selection policy:

```go
func (a *App) closeTabs(targets []*editor.Tab) {
	targets = a.openTargetTabs(targets)
	if len(targets) == 0 {
		return
	}
	closing := tabPointerSet(targets)
	active := a.activeTabPtr()
	oldActive := a.activeTab

	var fallback *editor.Tab
	if _, activeCloses := closing[active]; activeCloses {
		for i := oldActive + 1; i < len(a.tabs); i++ {
			if _, closes := closing[a.tabs[i]]; !closes {
				fallback = a.tabs[i]
				break
			}
		}
		if fallback == nil {
			for i := oldActive - 1; i >= 0; i-- {
				if _, closes := closing[a.tabs[i]]; !closes {
					fallback = a.tabs[i]
					break
				}
			}
		}
	}

	survivors := make([]*editor.Tab, 0, len(a.tabs)-len(targets))
	for _, tab := range a.tabs {
		if _, closes := closing[tab]; !closes {
			survivors = append(survivors, tab)
		}
	}
	a.tabs = survivors
	switch {
	case len(a.tabs) == 0:
		a.activeTab = 0
	case active != nil && a.tabIndex(active) >= 0:
		a.activeTab = a.tabIndex(active)
	case fallback != nil:
		a.activeTab = a.tabIndex(fallback)
	default:
		a.activeTab = 0
	}
	a.syncActiveTreeFile()
}
```

Update `closeTab(idx)` to call `closeTabs([]*editor.Tab{a.tabs[idx]})`. Update dirty `requestCloseTab` callbacks to capture the target pointer and resolve `tabIndex(target)` immediately before saving; the discard callback closes by pointer.

- [x] **Step 8: Run close and modal tests**

Run: `go test ./internal/app -run 'Test(RequestCloseTabs|CloseTabs|RequestCloseTab|OpenDirtyClose|HandleDirty)'`

Expected: PASS, including save failure and final active-file synchronization.

- [x] **Step 9: Commit the close model**

```bash
git add internal/app/app.go internal/app/modals_test.go internal/app/tabclose_test.go
git commit -m "Add batch tab close handling"
```

---

### Task 3: Route tab right-clicks to the close menu

**Files:**
- Modify: `internal/app/app.go:1480-1535, 1673-1765`
- Modify: `internal/app/app_test.go:680-740, 1575-1600`
- Modify: `internal/app/modals.go:37-40, 829-970`
- Modify: `internal/app/modals_test.go:420-570`

**Interfaces:**
- Produces: `(*App).tabAtPoint(x, y int) (*editor.Tab, bool)`.
- Produces: `(*App).tryTabContextClick(x, y int) bool`.
- Produces: `(*App).openTabContext(target *editor.Tab, x, y int)`.
- Consumes: `lastTabRects`, `syncActiveTreeFile`, closure-based `contextItem`, `tabIndex`, `tabsExcept`, and `requestCloseTabs`.

- [x] **Step 1: Add tab-menu and right-click routing tests**

Create two text fixtures, draw the app to populate `lastTabRects`, and right-click the inactive tab:

```go
func TestHandleMouse_RightClickTabActivatesAndOpensContext(t *testing.T) {
	a, paths := seedTabContextApp(t, "a.txt", "b.txt")
	a.draw()
	r := a.lastTabRects[0]

	a.handleMouse(tcell.NewEventMouse(r.X+1, 0, tcell.Button3, tcell.ModNone))

	if a.activeTab != 0 || a.tree.ActiveFile != paths[0] {
		t.Fatalf("activeTab=%d activeFile=%q", a.activeTab, a.tree.ActiveFile)
	}
	if !a.contextOpen {
		t.Fatal("tab context should open")
	}
	assertContextLabels(t, a.contextItems, []string{
		"Close tab", "Close other tabs", "Close all tabs",
	})
}
```

Add cases for the tab `CloseX` cell, `≡` button, tab-bar whitespace, editor body, and a file-tree row. Assert only actual tab hits expose the three tab rows and Button3 never closes a tab immediately.

- [x] **Step 2: Add enabled-state and callback wiring tests**

Open one tab and assert `Close other tabs` is disabled while the other rows are enabled. Open a second tab and assert all rows are enabled. Activate each row against clean tabs and verify it delegates to the tested close model: one closes the target, one preserves only the target, and one leaves no tabs.

Add a width assertion so `Close other tabs` is not truncated:

```go
if contextMenuWidth-5 < textWidth("Close other tabs") {
	t.Fatalf("context menu content width is too small")
}
```

- [x] **Step 3: Run routing tests and confirm failure**

Run: `go test ./internal/app -run 'Test(HandleMouse_RightClick|OpenTabContext|TabAtPoint|TabContext)'`

Expected: FAIL because every non-tree right-click still opens the main menu and `openTabContext` does not exist.

- [x] **Step 4: Add tab hit testing and Button3 routing**

```go
// tabAtPoint returns the rendered tab under the screen coordinate.
func (a *App) tabAtPoint(x, y int) (*editor.Tab, bool) {
	if y != 0 {
		return nil, false
	}
	for _, r := range a.lastTabRects {
		if x >= r.X && x < r.X+r.Width && r.Index >= 0 && r.Index < len(a.tabs) {
			return a.tabs[r.Index], true
		}
	}
	return nil, false
}

// tryTabContextClick activates a hit tab and opens its context menu.
func (a *App) tryTabContextClick(x, y int) bool {
	tab, ok := a.tabAtPoint(x, y)
	if !ok {
		return false
	}
	a.activeTab = a.tabIndex(tab)
	a.syncActiveTreeFile()
	a.openTabContext(tab, x, y)
	return true
}
```

In the Button3 branch, keep `tryTreeContextClick` first, then call `tryTabContextClick`, then preserve `openMenu()` as the fallback.

- [x] **Step 5: Build the three tab context rows**

Increase `contextMenuWidth` from 19 to 22 so the longest label fits. Add `openTabContext` beside `openTreeContext`:

```go
func (a *App) openTabContext(target *editor.Tab, x, y int) {
	a.closeAllModals()
	a.contextItems = []contextItem{
		{
			label: "Close tab",
			action: func(app *App) {
				if idx := app.tabIndex(target); idx >= 0 {
					app.requestCloseTab(idx)
				}
			},
			enabled: func(app *App) bool { return app.tabIndex(target) >= 0 },
		},
		{
			label:   "Close other tabs",
			action:  func(app *App) { app.requestCloseTabs(app.tabsExcept(target)) },
			enabled: func(app *App) bool { return app.tabIndex(target) >= 0 && len(app.tabs) > 1 },
		},
		{
			label:   "Close all tabs",
			action:  func(app *App) { app.requestCloseTabs(append([]*editor.Tab(nil), app.tabs...)) },
			enabled: func(app *App) bool { return app.tabIndex(target) >= 0 && len(app.tabs) > 0 },
		},
	}
	a.contextHover = 0
	a.contextX, a.contextY = a.placeContext(x, y, len(a.contextItems))
	a.contextOpen = true
}
```

- [x] **Step 6: Run tab routing and existing mouse tests**

Run: `go test ./internal/app -run 'Test(HandleMouse_RightClick|OpenTabContext|TabAtPoint|TabContext|OpenTreeContext|TabBarClick)'`

Expected: PASS. Editor-body and tab-bar-whitespace Button3 still open the main menu.

- [x] **Step 7: Run all app tests and commit**

Run: `go test ./internal/app`

Expected: PASS.

```bash
git add internal/app/app.go internal/app/app_test.go internal/app/modals.go internal/app/modals_test.go
git commit -m "Add tab context close actions"
```

---

### Task 4: Document and verify the completed feature

**Files:**
- Modify: `README.md:163-172`
- Modify: `website/content/docs/mouse-controls.md:36-43`
- Modify: `docs/superpowers/plans/2026-08-03-tab-context-close.md`
- Verify: `docs/superpowers/specs/2026-08-03-tab-context-close-design.md`

**Interfaces:**
- Documents: tab right-click activation, the three close rows, the one-tab disabled rule, and dirty-tab prompting.
- Verifies: all repository tests plus native, Linux, and Windows builds.

- [x] **Step 1: Update user-facing tab instructions**

In README, extend the tab instruction to state:

```markdown
- Click a tab to switch, click the `×` to close it. Right-click an actual tab
  to close that tab, close the other tabs, or close all tabs. Dirty targets use
  one Save / Discard / Cancel prompt.
```

In `website/content/docs/mouse-controls.md`, add this Tab bar row:

```markdown
| Right-click a tab                     | Activate it, then open Close tab / Close other tabs / Close all tabs. |
```

Keep the existing Button3/tmux warning and `Esc w` documentation unchanged.

- [x] **Step 2: Format and inspect the change set**

Run: `gofmt -w internal/app/app.go internal/app/app_test.go internal/app/modals.go internal/app/modals_test.go internal/app/tabclose_test.go`

Run: `git diff --check`

Expected: no output.

- [x] **Step 3: Run focused regression tests**

Run: `go test ./internal/app -run 'Test(Context|OpenTreeContext|HandleContext|HandleMouse_RightClick|OpenTabContext|TabAtPoint|TabContext|RequestCloseTabs|CloseTabs|RequestCloseTab)'`

Expected: PASS.

- [x] **Step 4: Run the full race-enabled suite**

Run: `make test`

Expected: PASS under the race detector.

- [x] **Step 5: Build native and Linux binaries**

Run: `make build`

Run: `make build-linux`

Expected: `bin/spiceedit` and `bin/spiceedit-linux-amd64` build successfully.

- [x] **Step 6: Cross-test and build Windows**

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/spice-edit-app-windows.test.exe ./internal/app`

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/spiceedit-windows-amd64.exe .`

Expected: both commands succeed.

- [x] **Step 7: Mark the plan complete and commit documentation**

Mark every plan checkbox complete after all commands pass, then stage only this feature's documentation:

```bash
git add README.md website/content/docs/mouse-controls.md \
  docs/superpowers/plans/2026-08-03-tab-context-close.md \
  docs/superpowers/specs/2026-08-03-tab-context-close-design.md
git commit -m "Document tab context close controls"
```

- [x] **Step 8: Audit final repository state**

Run: `git status --short --branch`

Expected: the branch contains the feature commits, and only the pre-existing Kitty draft files remain uncommitted.
