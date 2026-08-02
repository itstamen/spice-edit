# Tree External Open Confirmation Design

## Document information

- **Date:** 2026-08-02
- **Status:** Proposed
- **Related design:** `docs/superpowers/specs/2026-08-02-default-application-open-design.md`
- **Selected approach:** Add a source-aware external-open policy and reuse the existing Yes/No confirmation modal.

## 1. Goal

Prevent accidental desktop application launches when a user clicks an unsupported file in the left file tree. SpiceEdit must create or activate the file's read-only placeholder tab, ask for confirmation, and launch the host operating system's default application only after the user chooses Yes.

## 2. Scope

The confirmation applies only to left file-tree clicks on files classified as external. It does not change these deliberate or non-tree entry points:

- `Esc o`;
- **Open with default app** in the main menu;
- project finder results;
- command-line file arguments;
- files opened by existing internal workflows.

Text files and supported PNG, JPEG, and GIF images continue to open inline without confirmation.

## 3. User interaction

When the user clicks an external file in the tree:

1. SpiceEdit creates or activates the existing read-only external tab.
2. The editor displays `编辑器只支持文本及图片文件在线编辑.` behind the modal.
3. SpiceEdit opens the existing Yes/No confirmation modal.
4. The modal title is `Open with default app?`.
5. The message names the file and asks whether to open it with the system default application.
6. The default focus remains **No**.
7. Choosing **Yes** closes the modal and starts the existing asynchronous opener.
8. Choosing **No**, pressing Esc, or clicking outside the modal closes it without starting an application.

Canceling keeps the external tab open and active. Every later tree click on the same file opens a new confirmation modal; no tree click launches the application without confirmation.

## 4. Architecture

`App.openFile` currently combines tab activation with automatic external opening and serves several callers. The implementation will separate the shared file-open lifecycle from the external-launch policy.

The app layer will define two policies:

- **Immediate:** preserve current behavior for existing non-tree callers.
- **Confirm:** use the confirmation modal for left file-tree clicks.

`sidebarClick` selects the confirm policy. The existing `openFile` and exported `OpenFile` paths select the immediate policy. Both policies use the same classification, tab creation, tab reuse, active-folder update, tree reveal, and git-gutter logic.

This design avoids pre-classifying files in `sidebarClick`. `editor.NewTab` remains the single authority for text, image, corrupt-image, and external classification.

## 5. Data flow

```text
left file-tree click
        |
        v
open file with Confirm policy
        |
        v
create or reuse tab
        |
        +---- text/image ----> display inline
        |
        +---- external ------> display placeholder
                                  |
                                  v
                           open Yes/No modal
                             /          \
                           No            Yes
                           |              |
                    keep active tab   async opener
```

For an already-open external file, the flow starts at tab reuse and still opens the confirmation modal.

## 6. Confirmation behavior

The implementation will reuse `App.openConfirm`. This preserves the editor's established interaction rules:

- No is selected by default;
- Enter activates the selected button;
- Left and Right change the selected button;
- Esc cancels;
- clicking outside cancels;
- only the Yes callback can launch the application.

The callback captures the canonical file path selected by the tree click. It calls `openPathWithDefaultApp` after the modal closes. The callback does not save buffers, change tabs, or reclassify the file.

## 7. Error handling

- A classification or file-read error uses the existing status-bar error and does not show confirmation.
- Canceling is not an error and does not flash a warning.
- A launch failure uses the existing asynchronous `Could not open <name>: <reason>` status message.
- If the file disappears between confirmation and launch, the existing path validation reports the failure.
- Opening or canceling never creates duplicate tabs.

## 8. Behavior matrix

| Entry point | Text | Supported image | External file |
|---|---|---|---|
| Left file tree | Open inline | Preview inline | Activate placeholder, then confirm |
| Project finder | Open inline | Preview inline | Preserve immediate external open |
| Command-line path | Open inline | Preview inline | Preserve immediate external open |
| `Esc o` / main menu | Open externally | Open externally | Open externally |

## 9. Testing

App tests will cover:

- first tree click on an external file creates one external tab and opens confirmation without invoking the opener;
- choosing Yes invokes the opener once with the absolute path;
- choosing No, pressing Esc, and canceling through the modal do not invoke the opener;
- cancellation leaves the external tab active;
- repeated tree clicks on an already-open external file open confirmation each time without duplicating the tab;
- tree clicks on text and supported images do not open confirmation or invoke the opener;
- finder, command-line/shared `openFile`, `Esc o`, and menu behavior remain immediate;
- launcher success and failure continue to return through `defaultOpenDoneEvent`.

Run `make test`, `make build`, Linux build, and Windows cross-compilation after implementation.

## 10. Non-goals

- Do not add a remembered choice such as "always open this file type."
- Do not add an application picker.
- Do not change file classification or supported inline formats.
- Do not prompt for explicit `Esc o` or menu actions.
- Do not remove the placeholder tab after cancellation.
- Do not change SSH host-local opening behavior.

## 11. Documentation

Update README and website file-opening documentation to state that unsupported files selected from the left tree require confirmation. Keep the existing explanation that `Esc o` and the main menu explicitly open the active file with the host's default application.
