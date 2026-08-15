# Finder Existing Tab Activation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `Esc e` activate an existing Finder tab for the requested directory before creating a new tab.

**Architecture:** Preserve the shared reveal API and update only the Darwin `osascript` adapter. Canonicalize the target as a Finder alias, scan Finder windows in front-to-back order using System Events tab controls, compare each active tab's real Finder target, and use the current `Cmd+T` fallback only after no match.

**Tech Stack:** Go 1.24, AppleScript, macOS Finder, System Events Accessibility scripting, Go tests.

## Global Constraints

- Keep `platformRevealPath(path string, selectFile bool) error`, Windows behavior, and unsupported-platform behavior unchanged.
- Pass paths as literal `osascript` arguments; never interpolate them into AppleScript source.
- Active files use their parent directory; root fallback directories remain direct targets.
- Compare canonical Finder alias paths, not UI tab titles.
- Search all windows, first matching the frontmost window, and create a tab only when no match exists.
- Continue to return actionable Accessibility-permission errors.
- Do not stage or modify image-preview draft documents.

---

## File Structure

| Path | Responsibility |
|---|---|
| `internal/app/reveal_darwin.go` | Canonical target resolution, existing-tab activation, and new-tab fallback. |
| `internal/app/reveal_darwin_test.go` | Deterministic script-contract and existing helper tests. |
| `README.md` | Explain Finder existing-tab activation. |

### Task 1: Add Finder Tab Reuse

**Files:**
- Modify: `internal/app/reveal_darwin.go`
- Modify: `internal/app/reveal_darwin_test.go`

**Consumes:** `platformRevealPath(path string, selectFile bool) error`.

**Produces:** Updated `finderTabRevealScript`; all existing Go helper signatures remain unchanged.

- [x] **Step 1: Add a failing script-contract test**

Add the following test to `internal/app/reveal_darwin_test.go`. `strings` is already imported.

```go
// TestFinderTabRevealScriptReusesExistingTabs requires a full-path scan before
// the new-tab fallback.
func TestFinderTabRevealScriptReusesExistingTabs(t *testing.T) {
	fragments := []string{
		"set targetFolderPath to POSIX path of targetFolder",
		"set originalFrontWindow to front Finder window",
		"repeat with finderWindow in finderWindows",
		"set index of finderWindow to 1",
		"set tabButtons to radio buttons of tab group 1 of front window",
		"click tabButton",
		"if my activeFinderTargetPath() is targetFolderPath then return true",
		"if my activateMatchingFinderTab(targetFolderPath) then return",
		"set index of originalFrontWindow to 1",
		"keystroke \"t\" using {command down}",
	}
	last := -1
	for _, fragment := range fragments {
		index := strings.Index(finderTabRevealScript, fragment)
		if index == -1 {
			t.Fatalf("script is missing %q", fragment)
		}
		if index < last {
			t.Fatalf("script orders %q before a required prior step", fragment)
		}
		last = index
	}
}
```

- [x] **Step 2: Verify the new test fails**

Run:

```bash
go test ./internal/app -run TestFinderTabRevealScriptReusesExistingTabs -count=1
```

Expected: FAIL because the current script always creates a tab when Finder has a window.

- [x] **Step 3: Implement the AppleScript helpers and scan**

Replace only `finderTabRevealScript` in `internal/app/reveal_darwin.go` with this implementation. Retain `darwinRevealTarget`, `darwinRevealArgs`, `darwinRevealError`, and existing `CombinedOutput` error handling.

```go
const finderTabRevealScript = `on activeFinderTargetPath()
	tell application "Finder"
		return POSIX path of (target of front Finder window as alias)
	end tell
end activeFinderTargetPath

on activateMatchingFinderTab(targetFolderPath)
	tell application "Finder" to set finderWindows to Finder windows
	repeat with finderWindow in finderWindows
		tell application "Finder" to set index of finderWindow to 1
		delay 0.05
		tell application "System Events"
			tell process "Finder"
				set tabGroups to tab groups of front window
				if (count of tabGroups) is 0 then
					if my activeFinderTargetPath() is targetFolderPath then return true
				else
					set tabButtons to radio buttons of tab group 1 of front window
					repeat with tabButton in tabButtons
						click tabButton
						delay 0.05
						if my activeFinderTargetPath() is targetFolderPath then return true
					end repeat
				end if
			end tell
		end tell
	end repeat
	return false
end activateMatchingFinderTab

on run argv
	set targetFolder to POSIX file (item 1 of argv) as alias
	set targetFolderPath to POSIX path of targetFolder
	tell application "Finder"
		activate
		if (count of Finder windows) is 0 then
			make new Finder window to targetFolder
			return
		end if
		set originalFrontWindow to front Finder window
	end tell
	if my activateMatchingFinderTab(targetFolderPath) then return
	tell application "Finder" to set index of originalFrontWindow to 1
	tell application "System Events"
		tell process "Finder"
			set frontmost to true
			keystroke "t" using {command down}
		end tell
	end tell
	delay 0.1
	tell application "Finder" to set target of front Finder window to targetFolder
end run`
```

The no-tab-group branch handles a Finder window whose single tab is not exposed as a tab control. Keeping `originalFrontWindow` ensures an unmatched request creates its new tab in the window that was frontmost before the scan.

- [x] **Step 4: Format and run focused tests**

Run:

```bash
gofmt -w internal/app/reveal_darwin.go internal/app/reveal_darwin_test.go
go test ./internal/app -run 'Test(DarwinReveal|FinderTabRevealScriptReusesExistingTabs|ShowInFileManager)' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit Task 1**

Run:

```bash
git add internal/app/reveal_darwin.go internal/app/reveal_darwin_test.go
git commit -m "Reuse existing Finder tabs"
```

### Task 2: Document And Validate Finder Tab Reuse

**Files:**
- Modify: `README.md`

**Consumes:** Task 1's existing-tab matching behavior.

**Produces:** Updated documentation, test results, a local binary, and a manual macOS verification record.

- [x] **Step 1: Update README behavior text**

In `README.md`, replace the macOS sentence beginning `On macOS, an active file opens` with:

```markdown
On macOS, SpiceEdit first activates an existing Finder tab for an active
file's containing folder. When no matching tab exists, it opens the folder in
a new tab of an existing Finder window; when Finder has no open window,
SpiceEdit opens one.
```

Keep the nearby Accessibility, Windows Explorer, root fallback, and Linux statements unchanged.

- [x] **Step 2: Run automated validation**

Run:

```bash
git diff --check
make test
make build
```

Expected: no whitespace errors, all race-enabled tests pass, and `bin/spiceedit` builds.

- [ ] **Step 3: Manually validate on macOS**

After granting the terminal application Accessibility permission in **System Settings > Privacy & Security > Accessibility**:

1. Place a target tab in a background Finder window. `Esc e` makes that window frontmost, leaves the matching tab active, and adds no tab.
2. Duplicate a target tab in two windows, make one window frontmost, then press `Esc e`. The matching tab in the frontmost window remains active.
3. Use a target absent from Finder. `Esc e` creates exactly one tab in the Finder window that was frontmost before scanning.
4. Close all Finder windows. `Esc e` creates one Finder window at the target.
5. Revoke Accessibility permission. `Esc e` leaves the editor usable and displays the permission guidance.

- [x] **Step 4: Commit Task 2 and the plan**

Run:

```bash
git add README.md docs/superpowers/plans/2026-08-15-finder-existing-tab-activation.md
git commit -m "Document Finder tab reuse"
```

## Plan Review

- Spec coverage: Task 1 covers canonical full-path matching, every-window traversal, frontmost-window preference, no-match new-tab fallback, and no-window creation. Task 2 covers docs, automated tests, build, and every required macOS outcome.
- Placeholder scan: all tasks have exact paths, commands, expected results, and implementation code.
- Type consistency: the plan preserves `platformRevealPath(path string, selectFile bool) error`, `darwinRevealTarget(path string, selectFile bool) string`, `darwinRevealArgs(path string, selectFile bool) []string`, and `darwinRevealError(err error) error`.
