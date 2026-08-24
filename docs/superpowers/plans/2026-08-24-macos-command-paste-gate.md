# macOS Command Paste Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Accept terminal paste payloads only when a physical macOS `Command+V`
was just observed, while retaining system-clipboard Paste through the menu and
`Esc+v`.

**Architecture:** Add `internal/commandpaste`, a narrow platform abstraction
that records the time of physical Command+V through a macOS CoreGraphics
listen-only event tap. The app snapshots that signal at bracketed-paste start,
then accepts or discards the captured terminal payload at its end; it never
reads `pbpaste` from that terminal path. Explicit app paste actions remain on
the existing injected system-reader path.

**Tech Stack:** Go 1.24, tcell v2, macOS ApplicationServices/CoreGraphics via
cgo, `sync/atomic`, existing Go test suite and Make targets.

## Global Constraints

- The three and only three valid paste requests are physical `Command+V`,
  `Esc+v`, and the Paste menu action.
- Menu Paste and `Esc+v` read the macOS system clipboard through the existing
  injectable reader and must not fall back to `clipBuf`.
- Bracketed terminal paste must never call the system-clipboard reader; when
  accepted it inserts only its captured terminal payload.
- A terminal paste with no recent physical Command+V signal must leave every
  text target and modal input unchanged.
- The CoreGraphics listener must be listen-only: it cannot consume or alter
  keyboard input for SpiceEdit or any other application.
- macOS permission denial must leave `Esc+v` and the Paste menu usable, while
  disabling terminal `Command+V` paste with one clear status-line message.
- Non-macOS builds remain cgo-free and provide a no-op monitor that never
  authorizes terminal paste.
- Preserve existing LF normalization, modal routing, and one-step undo for an
  accepted terminal payload.
- Follow the repository's Go header and private-function comment conventions;
  run `gofmt` on all touched Go files.

---

### Task 1: Platform Command+V Monitor

**Files:**
- Create: `internal/commandpaste/monitor.go`
- Create: `internal/commandpaste/monitor_darwin.go`
- Create: `internal/commandpaste/monitor_other.go`
- Create: `internal/commandpaste/monitor_test.go`

**Interfaces:**
- Produces: `commandpaste.Monitor`, with `Recent(now time.Time) bool` and
  `Close()` methods.
- Produces: `commandpaste.Start() (Monitor, error)` and exported sentinel
  `commandpaste.ErrPermissionDenied` for a denied macOS event-listening
  permission.
- Consumes: no app package symbols; `internal/app` will own this monitor in
  Task 2.

- [ ] **Step 1: Write the failing, platform-neutral time-window tests**

Create `internal/commandpaste/monitor_test.go` with a deterministic clock and
an unstarted monitor constructor used only inside the package tests. Cover an
unrecorded monitor, a freshly recorded Command+V timestamp, an exactly expired
timestamp, and an idempotent close:

```go
func TestMonitorRecent(t *testing.T) {
    now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
    m := newMonitorForTest()
    if m.Recent(now) {
        t.Fatal("unrecorded monitor authorized terminal paste")
    }
    m.record(now)
    if !m.Recent(now.Add(commandPasteGracePeriod - time.Nanosecond)) {
        t.Fatal("fresh Command+V was not authorized")
    }
    if m.Recent(now.Add(commandPasteGracePeriod)) {
        t.Fatal("expired Command+V still authorized terminal paste")
    }
}
```

Add a non-darwin build-tagged test or compile-safe assertion that `Start()`
returns a monitor whose `Recent(time.Now())` is false and whose `Close()` can
be called twice.

- [ ] **Step 2: Run the focused test to verify it fails**

Run: `go test ./internal/commandpaste -run 'TestMonitorRecent|TestStart' -count=1`

Expected: FAIL because `internal/commandpaste` and its monitor API do not yet
exist.

- [ ] **Step 3: Implement the shared monitor contract and no-op platform stub**

Create `monitor.go` with the repository header, the `Monitor` interface,
`ErrPermissionDenied`, a named `commandPasteGracePeriod`, and a private
timestamp tracker using `atomic.Int64`. Its `Recent` implementation must use
strictly less than the grace period so the boundary test above expires.

Create `monitor_other.go` with `//go:build !darwin || !cgo`, returning a no-op
monitor from `Start()` without an error. Its `Recent` always returns false and
its `Close` is idempotent. Tag the CoreGraphics file as `darwin && cgo` so a
macOS `CGO_ENABLED=0` build selects this same stub.

The shared test constructor must build the same tracker without starting native
resources, so tracker behavior is fully unit-testable on Linux and macOS.

- [ ] **Step 4: Implement the macOS listener**

Create `monitor_darwin.go` with `//go:build darwin && cgo` and a minimal cgo bridge:

```go
/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>
*/
import "C"
```

The C bridge must create a `CGEventTapCreate` listener with:

```c
kCGSessionEventTap,
kCGHeadInsertEventTap,
kCGEventTapOptionListenOnly,
CGEventMaskBit(kCGEventKeyDown)
```

The callback must return the original `CGEventRef` unchanged. It records only
`kCGEventKeyDown` events with `kCGEventFlagMaskCommand` and the hardware V
keycode (`kVK_ANSI_V`, value `9`). Run the tap on its own CoreFoundation run
loop/thread, expose a C stop function that stops and wakes that loop, and make
Go `Close()` safe to call multiple times.

`Start()` must call `CGPreflightListenEventAccess`; if permission is missing,
call `CGRequestListenEventAccess` once. If access remains unavailable, return
a no-op monitor plus `ErrPermissionDenied`. If native tap creation or startup
fails, return a no-op monitor plus a wrapped error. The C callback must invoke
one exported Go function that records `time.Now()` on the currently active
monitor; it must not pass Go pointers to C or access app state.

- [ ] **Step 5: Run focused tests and platform builds**

Run:

```sh
gofmt -w internal/commandpaste/*.go
go test ./internal/commandpaste -count=1
CGO_ENABLED=0 go test ./internal/commandpaste -count=1
go test ./internal/commandpaste -count=1
```

Expected: all tests pass. The `CGO_ENABLED=0` invocation exercises the
non-darwin/no-cgo-compatible API shape; on macOS the final command also
compiles the CoreGraphics implementation.

- [ ] **Step 6: Commit Task 1**

```sh
git add internal/commandpaste
git commit -m "Add macOS Command paste monitor"
```

### Task 2: Gate Terminal Paste in the App and Document It

**Files:**
- Modify: `internal/app/app.go:342-404, 545-670, 782-789, 1146-1164, 1581-1621`
- Modify: `internal/app/app_test.go:36-70, 945-1030`
- Modify: `internal/app/eventburst_test.go:162-245`
- Modify: `internal/app/leader_test.go`
- Modify: `README.md:71-78`

**Interfaces:**
- Consumes: `commandpaste.Start() (commandpaste.Monitor, error)` and
  `Monitor.Recent(time.Time) bool` from Task 1.
- Produces: terminal-paste acceptance based on a snapshot stored at its start
  marker; `finishPaste()` routes only captured payload text when accepted.
- Preserves: `pasteClipboard()` for menu Paste and `Esc+v`, including its
  existing system-reader failure behavior.

- [ ] **Step 1: Replace the obsolete terminal-paste expectation with failing regression tests**

In `internal/app/eventburst_test.go`, replace the test that expects bracketed
paste to prefer `readFromSystem`. Add these focused tests using the normal
`handleEvent` sequence:

```go
func TestHandleEvent_BracketedPasteWithoutCommandVIsDiscarded(t *testing.T) {
    a := newTestApp(t, t.TempDir())
    a.commandPasteRecent = func(time.Time) bool { return false }
    calls := 0
    a.readFromSystem = func() (string, error) { calls++; return "clipboard", nil }
    // Paste start, rune 'x', paste end.
    // Assert empty buffer and calls == 0.
}

func TestHandleEvent_BracketedPasteAfterCommandVUsesCapturedText(t *testing.T) {
    a := newTestApp(t, t.TempDir())
    a.commandPasteRecent = func(time.Time) bool { return true }
    a.readFromSystem = func() (string, error) {
        t.Fatal("terminal paste read system clipboard")
        return "", nil
    }
    // Paste start, "a\\r\\nb\\rc", paste end.
    // Assert buffer is "a\\nb\\nc" and line count is 3.
}
```

Add a snapshot test where `commandPasteRecent` returns true only once at the
start marker, proving a later callback/clock change cannot alter the decision
mid-payload. Preserve coverage that accepted empty payloads do not modify the
editor. In `app_test.go` and `leader_test.go`, retain or add direct
assertions that menu Paste and `Esc+v` insert injected system clipboard text
even when the native monitor is unavailable.

- [ ] **Step 2: Run focused tests to verify the old behavior fails**

Run:

```sh
go test ./internal/app -run 'TestHandleEvent_BracketedPaste|TestPasteClipboard|TestHandleKey_LeaderPaste' -count=1
```

Expected: the new no-Command+V test fails because the current terminal path
still calls `readFromSystem`; the accepted-payload test fails because it still
prefers the system reader.

- [ ] **Step 3: Wire monitor lifecycle and the paste gate**

Import `internal/commandpaste`. Add to `App`:

```go
commandPasteRecent func(time.Time) bool
closeCommandPaste  func()
pasteAuthorized    bool
```

Create one private initializer used by both `New` and `NewSingleFile` that
calls `commandpaste.Start()`, installs `monitor.Recent` and `monitor.Close`,
and flashes exactly one `Command+V paste requires Input Monitoring permission`
message only for `errors.Is(err, commandpaste.ErrPermissionDenied)`. For other
startup failures, flash `Command+V paste unavailable`. Initialize test apps
with a false `commandPasteRecent` and a no-op closer.

Call the initializer after `App` construction and before returning from both
constructors. Call the closer from `App.Close()` before `screen.Fini()`.

Update paste capture exactly as follows:

```go
func (a *App) beginPaste() {
    a.pasting = true
    a.pasteAuthorized = a.commandPasteRecent != nil && a.commandPasteRecent(a.eventTime())
    a.pasteBuf.Reset()
    a.lastEscape = time.Time{}
}

func (a *App) finishPaste() {
    captured := a.pasteBuf.String()
    accepted := a.pasteAuthorized
    a.pasting = false
    a.pasteAuthorized = false
    a.pasteBuf.Reset()
    if !accepted || captured == "" {
        return
    }
    a.routePastedText(captured)
}
```

Do not call `readFromSystem` or `clipboard.ReadSystem` from `finishPaste`.
Leave `pasteClipboard()` unchanged apart from any necessary shared helper
extraction; it remains the source used by the menu and `Esc+v` binding.

- [ ] **Step 4: Update README behavior and build claim**

Replace the stale clipboard feature statement with one that says macOS
`Command+V` inserts the terminal payload only after Input Monitoring approval,
while the Paste menu and `Esc v` read the current system clipboard. Explain
that terminal paste not initiated by Command+V is ignored. Update the
single-binary claim so it no longer promises no CGO on macOS; say that release
builds remain one binary and that Linux/Windows builds do not require cgo.

- [ ] **Step 5: Run focused and full validation**

Run:

```sh
gofmt -w internal/app/app.go internal/app/app_test.go internal/app/eventburst_test.go internal/app/leader_test.go
go test ./internal/app -run 'TestHandleEvent_BracketedPaste|TestPasteClipboard|TestHandleKey_LeaderPaste' -count=1
go test ./internal/app ./internal/commandpaste -count=1
make test
make build
git diff --check
```

Expected: all commands succeed, including the race-enabled suite behind
`make test` and the current-host build behind `make build`.

- [ ] **Step 6: Commit Task 2**

```sh
git add internal/app/app.go internal/app/app_test.go internal/app/eventburst_test.go internal/app/leader_test.go README.md
git commit -m "Gate terminal paste on Command V"
```

## Plan Self-Review

- **Spec coverage:** Task 1 provides the permission-aware native signal and
  non-macOS no-op behavior. Task 2 gates all terminal payloads, preserves
  explicit system clipboard commands, owns listener lifecycle, adds regression
  coverage, and documents the user-visible permission requirement.
- **Placeholder scan:** No task contains incomplete work, unspecified test
  cases, or unspecified commands.
- **Type consistency:** Task 1 defines `Monitor.Recent(time.Time) bool`,
  `Monitor.Close()`, and `Start() (Monitor, error)`; Task 2 consumes those
  exact signatures. The `App` seam uses the corresponding function types.


