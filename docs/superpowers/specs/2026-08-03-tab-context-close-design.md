# Tab Context Close Design

## Document information

- **Date:** 2026-08-03
- **Status:** Approved
- **Related code:** `internal/app/app.go`, `internal/app/modals.go`
- **Selected approach:** Reuse the existing context-menu and unsaved-changes modal infrastructure, with menu actions captured as closures.

## 1. Goal

Add a tab-specific right-click menu to the editor tab bar. Right-clicking an actual tab must activate that tab and offer these actions:

- `Close tab`
- `Close other tabs`
- `Close all tabs`

The menu must appear only when the pointer hits a rendered editor tab. Right-clicks on the file tree, editor body, tab-bar whitespace, or the `≡` menu button must keep their existing behavior.

## 2. Interaction rules

### 2.1 Opening the menu

The app will hit-test right-clicks against `lastTabRects`, the same geometry used by normal tab-bar clicks.

- Right-clicking any part of a rendered tab, including its close-cell area, opens the tab context menu. It does not close the tab immediately.
- If the hit tab is not active, SpiceEdit sets `activeTab` to that tab before opening the menu.
- Activating the hit tab also calls `syncActiveTreeFile()` so the file-tree highlight follows the selected tab.
- Right-clicking the `≡` button opens the existing main action menu.
- Right-clicking a file-tree node opens the existing tree context menu.
- Right-clicking the editor body or tab-bar whitespace keeps the existing fallback to the main action menu.

The tab menu is mouse-discoverable, but no new keyboard shortcut is added. Closing the active tab remains available from the main menu as required for SSH and tmux workflows.

### 2.2 Menu rows

| Row | Enabled state | Result |
|---|---|---|
| `Close tab` | Always enabled while the target tab exists | Close only the right-clicked tab |
| `Close other tabs` | Enabled when more than one tab is open | Keep the right-clicked tab and close every other tab |
| `Close all tabs` | Enabled when at least one tab is open | Close every open tab |

Disabled rows use the existing muted context-menu style and ignore mouse or Enter activation.

## 3. Shared context-menu architecture

The current `contextItem` type is coupled to file-tree nodes. The implementation will generalize it so every row owns a closure:

```go
type contextItem struct {
	label   string
	action  func(*App)
	enabled func(*App) bool
}
```

`openTreeContext` will capture its `*filetree.Node` in each row closure. A new `openTabContext` helper will capture the target tab identity needed by its three close actions.

The existing context-menu implementation remains responsible for:

- popup placement and screen-edge clamping;
- drawing borders, hover state, and disabled rows;
- Up, Down, Enter, and Esc handling;
- mouse hover, activation, and outside-click dismissal;
- modal exclusivity through `closeAllModals()`.

The app no longer needs `contextNode` after tree actions capture their node. Removing that field prevents stale tree state from leaking into tab actions.

## 4. Tab targeting

The right-click handler will resolve the target from `lastTabRects`. The menu action must not depend on whichever tab happens to be active when the row is later activated.

The implementation should capture the target `*editor.Tab` and resolve its current index immediately before closing. Pointer identity is stable while the tab remains open and avoids an incorrect close if another callback changes indices before activation.

If the captured tab no longer exists, the action closes the context menu and makes no change.

## 5. Close-set behavior

Each action computes a target set of tab pointers:

- **Close tab:** the captured tab only.
- **Close other tabs:** every open tab except the captured tab.
- **Close all tabs:** every open tab.

The target set is a snapshot taken when the action starts. Closing operates by tab identity, not by stale numeric indices. When removal begins, targets are removed from highest current index to lowest, or through an equivalent identity-based helper, so index shifts cannot close the wrong tab.

`Close tab` must reuse the current single-tab `requestCloseTab` behavior. The batch actions use one shared batch-close helper because opening one confirmation per dirty tab would be slow and ambiguous.

## 6. Unsaved changes

### 6.1 Clean targets

If every target tab is clean, SpiceEdit closes the complete target set immediately without showing a modal.

### 6.2 One or more dirty targets

If any target is dirty, SpiceEdit opens the existing Save / Discard / Cancel modal once for the complete operation.

- For one dirty target, the message names that file using the existing `untitled` fallback.
- For multiple dirty targets, the message states the count, for example `3 files have unsaved changes.`
- **Cancel** closes the modal and leaves every target open.
- **Discard** closes every target without saving.
- **Save** saves only dirty tabs in the target set. It does not save dirty tabs that are meant to remain open.

The safe default remains Cancel.

### 6.3 Save failure semantics

The Save path is close-atomic:

1. Save dirty target tabs in their current tab order.
2. Stop at the first failure and keep every target tab open.
3. Preserve successful writes and their clean state; do not roll them back.
4. Leave unsaved or failed targets open and let `saveTabAt` display the existing error message.
5. Close the full target set only after every required save succeeds.

This prevents a partial close from hiding which files remain open after an error.

## 7. Active-tab and tree state after closing

After a successful close operation:

- If the active tab remains open, preserve it as active even if its numeric index changes.
- If the active tab closes and tabs remain, activate the nearest surviving tab using the current close behavior: prefer the tab now occupying the removed position, otherwise the preceding tab.
- If no tabs remain, keep `activeTab` in its current empty-state convention.
- Call `syncActiveTreeFile()` after the final target set has been removed so `tree.ActiveFile` reflects the final active tab once, rather than transient intermediate tabs.
- Keep `activeFolder` unchanged.

For `Close other tabs`, the right-clicked tab was activated before the menu opened and is guaranteed to survive, so it remains active after the operation.

## 8. Event routing

Right-click dispatch keeps the following priority:

```text
open modal/menu absorbs event
        |
        v
file-tree node hit? ------ yes --> tree context menu
        |
        no
        v
rendered tab hit? -------- yes --> activate tab, tab context menu
        |
        no
        v
existing main-menu fallback
```

This order prevents a tab menu from appearing over the file tree and preserves all existing context-menu and main-menu behavior outside actual tab rectangles.

## 9. Testing

App tests will cover:

- right-clicking an active tab opens exactly the three tab rows;
- right-clicking an inactive tab activates it and updates the tree highlight before the menu opens;
- right-clicking the tab close cell opens the context menu instead of closing immediately;
- file-tree right-click still opens the tree menu;
- right-clicking the `≡` button, editor body, and tab-bar whitespace does not open the tab menu;
- `Close other tabs` is disabled for one tab and enabled for two or more tabs;
- disabled context rows ignore Enter and mouse activation;
- `Close tab` preserves the existing clean and dirty single-tab flows;
- clean batch targets close immediately;
- `Close other tabs` preserves the right-clicked tab and closes only the others;
- batch Save saves only dirty target tabs, then closes the full target set;
- batch Discard closes the full target set without writes;
- batch Cancel closes nothing;
- a batch save failure closes no target tabs while retaining successful saves;
- closing targets preserves a surviving active tab or selects the nearest survivor;
- final file-tree highlighting matches the final active tab;
- the generalized closure-based context menu preserves tree context actions, drawing, keyboard navigation, pointer hover, placement, and dismissal.

Run focused `internal/app` tests, `make test`, `make build`, `make build-linux`, and a Windows `GOOS=windows GOARCH=amd64` cross-build.

## 10. Documentation

Update the README tab-management section to mention the three right-click actions. Keep the existing main-menu close command documented so users without reliable right-click events retain a discoverable close path.

## 11. Non-goals

- Do not add tab reordering, pinning, duplication, reopening, or close-to-left/right actions.
- Do not add new close shortcuts.
- Do not show the tab context menu on tab-bar whitespace or editor content.
- Do not alter file-tree context-menu labels or behavior.
- Do not autosave tabs before showing the unsaved-changes modal.
- Do not persist context-menu state across sessions.
