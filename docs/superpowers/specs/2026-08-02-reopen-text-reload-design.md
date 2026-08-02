# Reopen Text Reload Design

## Document information

- **Date:** 2026-08-02
- **Status:** Proposed
- **Related code:** `internal/app/app.go`, `internal/editor/tab.go`
- **Selected approach:** Reload every clean text tab on repeated open and confirm before replacing a dirty buffer.

## 1. Goal

When a user opens a text file that already has a SpiceEdit tab, the editor must read the file from disk again instead of showing the tab's old buffer. This makes a second file-tree click immediately show changes written by another process.

## 2. Scope

The behavior applies to existing editable text tabs reached through the shared file-opening path, including:

- left file-tree clicks;
- project finder results;
- exported `App.OpenFile` calls;
- other internal callers of `openFile`.

New tabs keep their current behavior. Image tabs, external placeholder tabs, `Esc o`, and `Esc e` are unchanged.

## 3. Clean text tabs

When the existing text tab is not dirty:

1. Activate the tab.
2. Call `Tab.Reload()` unconditionally.
3. Do not compare modification timestamps before reloading.
4. Refresh Git line-change metadata after a successful reload.
5. Flash `Reloaded <name> from disk`.

The unconditional read is required because some filesystems and tools can preserve timestamps or write more than once within the timestamp resolution.

If `Reload()` fails, keep the previous buffer and flash `Could not reload <name>: <reason>`.

## 4. Dirty text tabs

If the tab has unsaved changes, repeated open must not replace the buffer immediately. SpiceEdit opens the existing Yes/No confirmation modal with:

- title: `Reload file from disk?`;
- message: `Reload <name> and discard unsaved changes?`;
- default selection: **No**.

Choosing Yes calls `Tab.Reload()`. A successful reload replaces the buffer with the current disk contents, clears `Dirty`, resets undo and redo history through the existing `Reload` implementation, refreshes Git line-change metadata, and flashes the reload success message.

Choosing No, pressing Esc, or clicking outside the modal closes the modal without changing the buffer, dirty state, cursor, selection, scroll position, or undo history.

## 5. Architecture

`openFileWithExternalPolicy` remains the shared open path. Its existing-tab branch delegates editable text tabs to a focused helper after setting `activeTab`:

```go
func (a *App) reopenTextTab(tab *editor.Tab)
func (a *App) reloadTextTabFromDisk(tab *editor.Tab)
```

`reopenTextTab` decides whether confirmation is required. `reloadTextTabFromDisk` performs the reload, refreshes Git metadata, and owns success or error status messages.

The helper receives the existing `*editor.Tab` instead of looking up the active tab again. The confirmation callback captures that pointer, so the approved action reloads the exact tab selected by the repeated open.

## 6. Open flow

```text
open path
   |
   v
existing tab?
   | no -----------------> existing new-tab flow
   |
  yes
   |
   +---- external -------> existing default-app policy
   |
   +---- image ----------> activate only; unchanged
   |
   +---- text, clean ----> reload immediately
   |
   +---- text, dirty ----> confirm discard
                               /      \
                             No        Yes
                             |          |
                        keep buffer    reload
```

## 7. Error handling

- Missing, unreadable, or invalid paths keep the old text buffer and show a reload error.
- A file removed after the confirmation opens fails safely when the user chooses Yes.
- A reload error does not clear Dirty or undo history because `Tab.Reload()` replaces state only after successful reads and stat calls.
- Repeated open never creates a duplicate tab.

## 8. Testing

App regression tests cover:

- a clean existing text tab reloads changed disk contents on repeated open;
- reload occurs even when the disk modification time is restored to the tab's recorded value;
- a dirty existing tab opens confirmation with No selected;
- No, Esc, and cancel keep the dirty buffer and undo history;
- Yes loads the current disk contents, clears Dirty, and resets undo history;
- a reload failure keeps the old buffer and reports an error;
- image and external existing-tab behavior remains unchanged;
- repeated open still reuses one tab.

Run focused app tests, `make test`, `make build`, Linux build, and Windows cross-compilation.

## 9. Non-goals

- Do not merge disk changes with unsaved edits.
- Do not create a second tab for the same path.
- Do not change the periodic disk-reconciliation policy.
- Do not reload image tabs as part of this fix.
- Do not change external-file confirmation or default-application behavior.
- Do not add a persistent preference for automatic reload decisions.

## 10. Documentation

This is a corrective behavior change and does not add a new user command. No README or website update is required. The regression tests and this design document define the behavior.
