# Mouse Selection Auto-Copy Design

## Status

- Date: 2026-08-15
- Status: Approved for implementation planning

## Goal

Automatically copy text selected with the mouse and show a temporary overlay
toast confirming successful system-clipboard publication.

## Decision

Use the existing editor mouse gesture lifecycle. On a left-button release,
inspect the drag mode before clearing it. When the completed gesture belongs to
the editor and the active editable text tab has a non-empty selection, invoke
the shared copy path exactly once.

This covers both click-drag selection and double-click word selection because
both finish with the same left-button release event. It also covers selections
extended by drag auto-scroll. Single clicks collapse the selection and do not
copy; sidebar drags, tabs, menus, read-only images, and placeholders do not
copy.

## Clipboard And Errors

The shared copy operation continues to set SpiceEdit's internal clipboard and
write through OSC 52. Display the success toast only when the OSC 52 write
succeeds. When it fails, retain the current status-bar fallback explaining that
the system clipboard is unavailable, so the UI never claims an unavailable
system copy succeeded.

## Toast

Add transient App toast state and draw a bottom-centered overlay above the
editor content. The style follows the approved Herdr reference:

- dark solid background;
- thin green border;
- green circular status marker;
- `Copied to clipboard` message;
- three-second lifetime.

The toast does not change buffer content, selection state, undo history, or
the existing status-bar message. A later successful copy refreshes the timeout
rather than stacking overlays.

## Scope

The change is limited to App mouse release handling, the shared copy path,
overlay drawing, same-package tests, and README mouse-selection documentation.
It does not add host clipboard reads, alter keyboard copy/cut/paste semantics,
or change image rendering behavior.

## Testing

- Simulate editor drag release and assert one internal-copy update plus an
  active toast.
- Simulate double-click selection followed by release and assert the selected
  word is copied once.
- Assert empty selections, single clicks, sidebar drags, read-only tabs, and
  placeholder/image tabs leave clipboard and toast unchanged.
- Assert clipboard-write failure suppresses the success toast and preserves the
  fallback status message.
- Assert the toast expires after its lifetime and draws within the editor area.
- Run focused App tests, `make test`, and `make build`.
