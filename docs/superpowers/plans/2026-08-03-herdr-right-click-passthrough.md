# Herdr Right-Click Passthrough Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make tab batch-close actions usable inside Herdr through Ctrl+right-click passthrough and SpiceEdit's main action menu.

**Architecture:** Keep unmodified right-click ownership in Herdr and configure its supported Ctrl modifier to forward Button3 into pane applications. Add `Close other tabs` and `Close all tabs` to SpiceEdit's existing tab-action menu group, delegating to the pointer-based `requestCloseTabs` implementation already shared by the tab context menu.

**Tech Stack:** Go 1.24, tcell action menu, Herdr 0.7.5 TOML configuration, Go tests, Make targets.

## Global Constraints

- Preserve Herdr's plain right-click pane menu.
- Configure `ui.right_click_passthrough_modifier = "ctrl"`; do not use Shift.
- Preserve every unrelated value in `~/.config/herdr/config.toml`.
- Add no new SpiceEdit keyboard shortcuts.
- Keep `Close tab`, `Close other tabs`, and `Close all tabs` in one main-menu group.
- Reuse `requestCloseTab`, `tabsExcept`, and `requestCloseTabs`; do not create a second close flow.
- Keep batch Save / Discard / Cancel and save-failure semantics unchanged.
- Preserve unrelated Kitty draft files in the repository worktree.

---

### Task 1: Add main-menu batch-close tests

**Files:**
- Create: `internal/app/tabmenu_test.go`
- Modify: `internal/app/app_test.go:1773-1864, 1960-1985`

**Interfaces:**
- Consumes: `menuItemByLabel`, `seedCloseTabs`, `requestCloseTabs`, and `menuLayout`.
- Produces: failing coverage for `menuCloseOtherTabs`, `menuCloseAllTabs`, and `hasOtherTabs`.

- [ ] **Step 1: Test menu labels, order, and shortcuts**

Assert the first built-in group contains these rows in order:

```go
want := []string{
	"Save",
	"Save & close tab",
	"Close tab",
	"Close other tabs",
	"Close all tabs",
}
group := builtinMenuGroups()[0]
for i, label := range want {
	if group[i].label != label {
		t.Fatalf("tab action %d = %q, want %q", i, group[i].label, label)
	}
}
```

Extend `TestMenuLayout_Shortcuts` with empty shortcut values for the two new rows.

- [ ] **Step 2: Test enabled states**

Use `menuItemByLabel` to verify:

- no tabs: both rows disabled;
- one tab: `Close other tabs` disabled, `Close all tabs` enabled;
- two tabs: both rows enabled.

- [ ] **Step 3: Test clean action callbacks**

```go
func TestMenuCloseOtherTabs_KeepsActiveTab(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt", "c.txt")
	a.activeTab = 1
	a.menuOpen = true

	a.menuCloseOtherTabs()

	if a.menuOpen || len(a.tabs) != 1 || a.tabs[0] != tabs[1] {
		t.Fatalf("menuOpen=%v tabs=%v, want only active b", a.menuOpen, a.tabs)
	}
}

func TestMenuCloseAllTabs_ClosesCleanTabs(t *testing.T) {
	a, _ := seedCloseTabs(t, "a.txt", "b.txt")
	a.menuOpen = true

	a.menuCloseAllTabs()

	if a.menuOpen || len(a.tabs) != 0 {
		t.Fatalf("menuOpen=%v tabs=%d, want closed", a.menuOpen, len(a.tabs))
	}
}
```

- [ ] **Step 4: Test dirty-action delegation**

Dirty two tabs, call `menuCloseAllTabs`, and assert one existing dirty-close modal opens with `2 files have unsaved changes.` and Cancel selected. Cancel and verify both tabs remain dirty and open.

- [ ] **Step 5: Update expected menu geometry**

Adding two rows to the first group changes the no-custom layout to:

```go
wantHeight := 36
wantItems := 26
wantDividers := []int{2, 8, 12, 15, 25, 30, 33}
```

Update the custom-actions test height from `37` to `39` while preserving its placement assertions.

- [ ] **Step 6: Run tests and confirm failure**

Run: `go test ./internal/app -run 'Test(MenuClose|MenuLayout_(NoCustomActions|WithCustomActions|Shortcuts|TabClose))'`

Expected: FAIL because the rows, predicates, and menu action methods do not exist.

---

### Task 2: Implement main-menu batch-close actions

**Files:**
- Modify: `internal/app/app.go:195-230, 2373-2400, 2746-2760`
- Test: `internal/app/tabmenu_test.go`
- Test: `internal/app/app_test.go`

**Interfaces:**
- Produces: `(*App).hasOtherTabs() bool`.
- Produces: `(*App).menuCloseOtherTabs()`.
- Produces: `(*App).menuCloseAllTabs()`.
- Updates: the first group returned by `builtinMenuGroups`.

- [ ] **Step 1: Add the two menu rows**

```go
{
	{label: "Save", shortcut: "Esc s", action: (*App).menuSave, enabled: (*App).hasSavableTab},
	{label: "Save & close tab", action: (*App).menuSaveAndClose, enabled: (*App).hasSavableTab},
	{label: "Close tab", shortcut: "Esc w", action: (*App).menuClose, enabled: (*App).hasTab},
	{label: "Close other tabs", action: (*App).menuCloseOtherTabs, enabled: (*App).hasOtherTabs},
	{label: "Close all tabs", action: (*App).menuCloseAllTabs, enabled: (*App).hasTab},
},
```

- [ ] **Step 2: Add the enabled predicate**

```go
// hasOtherTabs reports whether closing every tab except the active one would
// remove at least one tab.
func (a *App) hasOtherTabs() bool {
	return a.activeTabPtr() != nil && len(a.tabs) > 1
}
```

Place it beside the existing main-menu predicates such as `hasTab`.

- [ ] **Step 3: Add action wrappers**

```go
// menuCloseOtherTabs closes every tab except the active tab.
func (a *App) menuCloseOtherTabs() {
	a.closeMenu()
	target := a.activeTabPtr()
	if target == nil {
		return
	}
	a.requestCloseTabs(a.tabsExcept(target))
}

// menuCloseAllTabs closes every open tab through the shared batch flow.
func (a *App) menuCloseAllTabs() {
	a.closeMenu()
	a.requestCloseTabs(append([]*editor.Tab(nil), a.tabs...))
}
```

- [ ] **Step 4: Run focused tests**

Run: `go test ./internal/app -run 'Test(MenuClose|MenuLayout_(NoCustomActions|WithCustomActions|Shortcuts|TabClose)|RequestCloseTabs)'`

Expected: PASS.

- [ ] **Step 5: Run all app tests**

Run: `go test ./internal/app`

Expected: PASS.

---

### Task 3: Configure Herdr Ctrl+right-click passthrough

**Files:**
- Modify: `~/.config/herdr/config.toml`

**Interfaces:**
- Adds: `[ui].right_click_passthrough_modifier = "ctrl"`.
- Preserves: `onboarding = false` and `[experimental].kitty_graphics = true`.

- [ ] **Step 1: Inspect the current config immediately before editing**

Run: `sed -n '1,260p' ~/.config/herdr/config.toml`

Expected: confirm whether `[ui]` already exists so the edit does not create a duplicate TOML table.

- [ ] **Step 2: Merge the passthrough setting**

For the currently observed config, append:

```toml

[ui]
right_click_passthrough_modifier = "ctrl"
```

If the user changes the file before this task executes and `[ui]` exists, add or replace only the key inside that table.

- [ ] **Step 3: Validate the config**

Run: `herdr config check`

Expected: `config: ok`.

- [ ] **Step 4: Reload the active Herdr server**

Run: `herdr server reload-config`

Expected: successful reload. If no server is running, retain the validated config for the next launch and report the reload result.

- [ ] **Step 5: Confirm the live file value**

Run: `rg -n -C 2 'right_click_passthrough_modifier' ~/.config/herdr/config.toml`

Expected: exactly one active setting with value `"ctrl"`.

---

### Task 4: Document Herdr behavior and complete verification

**Files:**
- Modify: `README.md:165-175`
- Modify: `website/content/docs/mouse-controls.md:34-44`
- Modify: `website/content/docs/running-over-ssh.md`
- Modify: `website/content/docs/troubleshooting.md:10-20`
- Modify: `docs/superpowers/plans/2026-08-03-herdr-right-click-passthrough.md`
- Verify: `docs/superpowers/specs/2026-08-03-herdr-right-click-passthrough-design.md`

**Interfaces:**
- Documents: plain Herdr right-click, Ctrl+right-click passthrough, and the main-menu fallback.
- Verifies: repository tests and native/Linux/Windows builds.

- [ ] **Step 1: Update README and website documentation**

Add this Herdr configuration wherever right-click behavior is documented:

```toml
[ui]
right_click_passthrough_modifier = "ctrl"
```

State that plain right-click opens Herdr's pane menu, Ctrl+right-click reaches SpiceEdit after reload, and `≡` or double-Esc exposes all three close actions without mouse passthrough.

- [ ] **Step 2: Format and inspect**

Run: `gofmt -w internal/app/app.go internal/app/app_test.go internal/app/tabmenu_test.go`

Run: `git diff --check`

Expected: no output.

- [ ] **Step 3: Run focused regressions**

Run: `go test ./internal/app -run 'Test(MenuClose|MenuLayout_|RequestCloseTabs|OpenTabContext|HandleMouse_RightClick)'`

Expected: PASS.

- [ ] **Step 4: Run full tests**

Run: `make test`

Expected: PASS under the race detector.

- [ ] **Step 5: Build native and Linux binaries**

Run: `make build`

Run: `make build-linux`

Expected: both commands succeed.

- [ ] **Step 6: Cross-test and build Windows**

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/spice-edit-app-windows.test.exe ./internal/app`

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/spiceedit-windows-amd64.exe .`

Expected: both commands succeed.

- [ ] **Step 7: Commit repository changes**

Stage only the menu fallback, tests, docs, plan, and approved design:

```bash
git add README.md \
  docs/superpowers/plans/2026-08-03-herdr-right-click-passthrough.md \
  docs/superpowers/specs/2026-08-03-herdr-right-click-passthrough-design.md \
  internal/app/app.go internal/app/app_test.go internal/app/tabmenu_test.go \
  website/content/docs/mouse-controls.md \
  website/content/docs/running-over-ssh.md \
  website/content/docs/troubleshooting.md
git commit -m "Add Herdr tab-close fallback"
```

The global Herdr config is intentionally not part of the Git commit.

- [ ] **Step 8: Audit final state**

Run: `git status --short --branch`

Expected: only the pre-existing Kitty draft files remain uncommitted, and the local branch contains the new feature commit.

- [ ] **Step 9: Manual smoke test**

In the active Herdr pane, start the newly built `bin/spiceedit`, open two tabs, then verify:

1. plain right-click still opens the Herdr pane menu;
2. Ctrl+right-click on a tab opens the SpiceEdit tab context menu;
3. the `≡` menu exposes all three close actions.

If automated control cannot synthesize the physical modified mouse gesture, report that only steps 1-8 were machine-verified and leave step 9 as the user's interactive check.
