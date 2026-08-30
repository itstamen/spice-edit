# Tree External Confirmation Without Placeholder Tabs

## Document information

- **Date:** 2026-08-30
- **Status:** Approved for implementation planning
- **Related design:** `docs/superpowers/specs/2026-08-02-tree-external-open-confirmation-design.md`

## Goal

When a user selects an unsupported non-text file from the left file tree,
SpiceEdit must ask whether to open it with the system default application
without creating or activating an editor tab. Text files and supported images
continue to open inside SpiceEdit.

## Scope

This behavior applies only to left file-tree clicks.

| Tree file type | Behavior |
|---|---|
| UTF-8 text | Create or activate an editable text tab |
| Supported PNG, JPEG, or GIF | Create or activate the existing image-preview tab |
| Other non-text or unsupported file | Keep the current tab unchanged and show the default-application confirmation |

Project finder results, command-line file arguments, exported `OpenFile`, and
other existing non-tree workflows retain their current behavior. Explicit
`Esc o` and **Open with default app** actions remain immediate and do not show
this confirmation.

## Interaction behavior

Selecting an unsupported file in the tree updates the tree selection and the
active folder to the file's parent directory. It does not add a placeholder
tab and does not change the active tab.

The existing confirmation modal is reused:

- title: `Open with default app?`;
- message: `Open <filename> with the system default application?`;
- default selection: **No**.

Choosing **Yes** starts the existing asynchronous default-application opener.
Choosing **No**, pressing Esc, or clicking outside the modal closes it without
launching another application. Every outcome leaves the current editor tab
unchanged. When no tab was open before the tree click, no tab is created.

Repeated clicks on the same unsupported file show a fresh confirmation each
time. If that file already has a placeholder tab because another entry point
opened it earlier, a tree click still does not activate that placeholder.

## Architecture

The app retains one shared file-open pipeline and extends its source-aware
external policy. The tree uses a policy that confirms external files without
adding or activating a tab. Non-tree callers continue using the immediate
policy.

For an already-open path, the pipeline first inspects the matching tab:

- text and image tabs are activated through the existing reuse behavior;
- external placeholder tabs are not activated for the tree confirmation
  policy.

For a path with no existing tab, `editor.NewTab` remains the single owner of
content classification and image decoding. The App creates a candidate tab,
then:

- appends it when it is text or a supported image;
- discards the candidate and opens confirmation when it is external.

This avoids duplicating file-extension and content-sniffing rules in the App
layer. Creating an unreferenced candidate object has no visible tab, graphics,
or editor-state side effects.

## State updates

Tree selection remains meaningful even without a tab:

- `activeFolder` becomes the file's parent directory;
- `tree.ActiveFolder` mirrors that parent;
- `tree.ActiveFile` records the selected file;
- tree reveal/scroll behavior remains unchanged.

The current active tab, tab order, cursor, selection, dirty state, and image
graphics state remain unchanged by an external tree selection and its modal.

## Error handling

- Classification or file-read failures use the existing status-bar error and
  do not show confirmation or create a tab.
- A launch failure returns through the existing asynchronous completion event
  and status message.
- Canceling is not an error and produces no warning.
- Repeated tree clicks never create duplicate text or image tabs and never
  create external placeholder tabs.

## Tests

App regression tests will verify:

- an unsupported tree file opens confirmation without creating a tab;
- with an existing text or image tab active, an unsupported tree file does not
  change the active tab;
- **Yes** opens the selected path once and leaves tabs unchanged;
- **No**, Esc, and outside-click cancellation leave tabs unchanged and do not
  call the opener;
- repeated unsupported-file clicks reopen confirmation without creating tabs;
- an existing external placeholder from a non-tree entry point is not
  activated by a tree click;
- text tree clicks retain editable-tab behavior;
- PNG, JPEG, and GIF tree clicks retain image-preview behavior;
- non-tree external opening retains its current immediate placeholder behavior;
- classification failures show the existing error without a modal or tab.

README documentation will describe the three tree-selection outcomes and
remove the statement that unsupported tree files open a read-only placeholder.

## Out of scope

- Removing external placeholder tabs from non-tree entry points.
- Removing or changing image preview support.
- Remembering a default-application choice by extension.
- Adding an application picker.
- Changing file classification rules or supported image formats.
- Changing explicit `Esc o` or menu behavior.
