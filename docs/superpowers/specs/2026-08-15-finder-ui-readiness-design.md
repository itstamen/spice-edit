# Finder UI Readiness Design

## Status

- Date: 2026-08-15
- Status: Approved for implementation planning

## Goal

Ensure macOS `Esc e` can scan existing Finder tabs and create a new tab when
no match exists, even when Finder's Accessibility UI is not immediately ready
after activation.

## Problem

The existing Finder-tab scan asks System Events for `window 1 of process
Finder` immediately after Finder is activated. On the reported macOS setup,
that UI window is not yet available and System Events returns error `519`.
The script exits before it can compare folders or execute its new-tab fallback.

## Decision

Add an AppleScript readiness helper before every operation that reads Finder UI
tab controls. The helper will:

1. Set the Finder process to frontmost through System Events.
2. Retry a bounded number of times for a Finder accessibility window to become
   available, with a short delay between attempts.
3. Return success only after `window 1` exists.

The existing-tab scan will call this helper before obtaining tab groups. The
no-match branch will call it again before sending `Cmd+T`, so the key event is
not issued against an unavailable UI process.

If Finder does not expose an Accessibility window before the retry limit, the
script returns a specific error that identifies Finder UI readiness. The Go
adapter preserves that output for the editor status bar. This is distinct from
the existing Accessibility-permission error guidance.

## Scope

The change is limited to the Darwin AppleScript constant, deterministic
script-contract tests, the Finder behavior documentation, and macOS manual
verification. It does not alter the shared reveal interface, Windows behavior,
Linux behavior, or Finder preferences.

## Testing

- Extend the Darwin script-contract test to require frontmost activation,
  bounded window readiness checks, and use of the helper before tab scanning
  and `Cmd+T`.
- Keep target-folder and Accessibility-error tests intact.
- Run `gofmt`, focused Darwin tests, `make test`, and `make build`.
- Manually reproduce the reported case: keep Finder open at
  `/Users/itstamen/workspace/shop/shop-tools`, reveal
  `/Users/itstamen/workspace/ai/aiserver/gj.html`, and verify a new active tab
  opens at `/Users/itstamen/workspace/ai/aiserver`.
- Also verify reuse of an existing target tab and the no-Finder-window path.
