# Finder UI Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent Finder Accessibility UI readiness races from blocking `Esc e` tab reuse or new-tab creation.

**Architecture:** Add an AppleScript helper that fronts Finder and waits for an Accessibility window with a bounded retry. Call it before tab scanning and `Cmd+T`; retain the existing target matching, stable Finder window-ID traversal, and Go error propagation.

**Tech Stack:** Go 1.24, AppleScript, macOS Finder, System Events Accessibility scripting, Go tests.

## Global Constraints

- Keep `platformRevealPath(path string, selectFile bool) error` and all non-Darwin adapters unchanged.
- Preserve literal `osascript` path arguments, existing tab reuse, and stable Finder window-ID traversal.
- Retry at most ten times with a 50 ms delay for Finder's Accessibility window.
- Return a distinct Finder UI readiness error when no UI window becomes available.
- Do not modify or stage image-preview draft documents.

---

## File Structure

| Path | Responsibility |
|---|---|
| `internal/app/reveal_darwin.go` | Finder UI readiness helper and call sites. |
| `internal/app/reveal_darwin_test.go` | Deterministic readiness and ordering assertions. |
| `docs/superpowers/plans/2026-08-15-finder-ui-readiness.md` | Implementation and verification checklist. |

### Task 1: Wait For Finder Accessibility UI

**Files:**
- Modify: `internal/app/reveal_darwin.go`
- Modify: `internal/app/reveal_darwin_test.go`

**Consumes:** Existing `finderTabRevealScript` and its Finder target/tab helpers.

**Produces:** `waitForFinderWindow()` called before tab controls and before `Cmd+T`.

- [x] **Step 1: Extend the failing script-contract test**

Append the following block inside `TestFinderTabRevealScriptReusesExistingTabs`, after its existing `runFragments` loop and before its final closing brace:

```go
readinessFragments := []string{
	"on waitForFinderWindow()",
	"set frontmost to true",
	"repeat 10 times",
	"if exists window 1 then return true",
	"delay 0.05",
	"return false",
}
for _, fragment := range readinessFragments {
	if !strings.Contains(finderTabRevealScript, fragment) {
		t.Fatalf("script is missing readiness fragment %q", fragment)
	}
}

readinessCall := "if not my waitForFinderWindow() then error \"Finder UI did not become ready for tab automation\""
if strings.Count(finderTabRevealScript, readinessCall) != 2 {
	t.Fatalf("readiness calls = %d, want 2", strings.Count(finderTabRevealScript, readinessCall))
}
if strings.Index(finderTabRevealScript, readinessCall) > strings.Index(finderTabRevealScript, "set tabGroups to tab groups of front window") {
	t.Fatal("tab scan starts before Finder UI readiness check")
}
if strings.LastIndex(finderTabRevealScript, readinessCall) > strings.Index(finderTabRevealScript, "keystroke \"t\" using {command down}") {
	t.Fatal("new-tab shortcut starts before Finder UI readiness check")
}
```

- [x] **Step 2: Verify the test fails**

Run:

```bash
go test ./internal/app -run TestFinderTabRevealScriptReusesExistingTabs -count=1
```

Expected: FAIL because the script does not define `waitForFinderWindow`.

- [x] **Step 3: Add the readiness handler and both call sites**

In `internal/app/reveal_darwin.go`, insert this handler between `activeFinderTargetPath` and `activateMatchingFinderTab`:

```applescript
on waitForFinderWindow()
	tell application "System Events"
		tell process "Finder"
			set frontmost to true
			repeat 10 times
				if exists window 1 then return true
				delay 0.05
			end repeat
		end tell
	end tell
	return false
end waitForFinderWindow
```

Inside `activateMatchingFinderTab`, add this exact line immediately after the `delay 0.05` that follows the Finder window-ID fronting statement:

```applescript
if not my waitForFinderWindow() then error "Finder UI did not become ready for tab automation"
```

In `on run argv`, add the same line immediately after:

```applescript
tell application "Finder" to set index of originalFrontWindow to 1
```

It must be before the `System Events` block that sends `Cmd+T`. Leave the Go error helper unchanged because `platformRevealPath` already exposes `osascript` stderr.

- [x] **Step 4: Format and run focused validation**

Run:

```bash
gofmt -w internal/app/reveal_darwin.go internal/app/reveal_darwin_test.go
go test ./internal/app -run 'Test(DarwinReveal|FinderTabRevealScriptReusesExistingTabs|ShowInFileManager)' -count=1
sed -n '/^const finderTabRevealScript = `/,/^end run`$/p' internal/app/reveal_darwin.go | sed '1s/^.*`//' | sed '$s/`$//' | osacompile -o /tmp/spiceedit-finder-ui-readiness.scpt -
```

Expected: Go tests pass and `osacompile` exits successfully.

- [x] **Step 5: Commit Task 1**

Run:

```bash
git add internal/app/reveal_darwin.go internal/app/reveal_darwin_test.go
git commit -m "Wait for Finder UI before opening tabs"
```

### Task 2: Validate The Reported Regression

**Files:**
- Modify: `docs/superpowers/plans/2026-08-15-finder-ui-readiness.md`

**Consumes:** Task 1's readiness helper.

**Produces:** Full test/build results and a recorded manual regression check.

- [x] **Step 1: Run full automated validation**

Run:

```bash
git diff --check
make test
make build
```

Expected: no whitespace errors, all race-enabled tests pass, and `bin/spiceedit` builds.

- [ ] **Step 2: Manually verify the reported case**

With the terminal that starts SpiceEdit authorized in **System Settings > Privacy & Security > Accessibility**:

1. Open Finder at `/Users/itstamen/workspace/shop/shop-tools`.
2. Open `/Users/itstamen/workspace/ai/aiserver/gj.html` in SpiceEdit.
3. Press `Esc e` and verify an active new tab opens at `/Users/itstamen/workspace/ai/aiserver`.
4. Press `Esc e` again and verify the existing `aiserver` tab is activated without duplication.
5. Verify a matching tab in a background Finder window is fronted, and closing all Finder windows opens one window at the target.

- [x] **Step 3: Commit Task 2 and the plan**

After recording completed validation steps, run:

```bash
git add docs/superpowers/plans/2026-08-15-finder-ui-readiness.md
git commit -m "Document Finder UI readiness validation"
```

## Plan Review

- Spec coverage: Task 1 adds a bounded readiness wait at both UI access boundaries; Task 2 covers the exact reported paths, existing-tab reuse, background-window activation, and no-window behavior.
- Placeholder scan: timing, code, paths, commands, and expected outcomes are explicit.
- Type consistency: no Go signatures change; `darwinRevealArgs(path string, selectFile bool) []string` and `platformRevealPath(path string, selectFile bool) error` remain the execution boundary.
