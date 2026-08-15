# Finder Tab Reveal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `Esc e` open an active file's parent directory in a new tab of an existing macOS Finder window, or create one Finder window if none exists.

**Architecture:** Preserve the shared `pathRevealer` contract and change only the Darwin adapter. It passes an absolute folder as a literal `osascript` argument, creates a Finder window when there are no windows, or uses System Events to send `Cmd+T` before setting the new tab's target.

**Tech Stack:** Go 1.24 standard library, macOS Finder AppleScript, System Events, same-package Go tests.

## Global Constraints

- Keep Windows and unsupported-platform adapters unchanged.
- Never interpolate a filesystem path into AppleScript source.
- Active-file requests open the parent directory; root fallback requests use the directory unchanged.
- Map accessibility authorization failures to an actionable error.
- Run `gofmt`, `make test`, `make build`, and macOS manual validation.

---

## File Structure

| Path | Responsibility |
|---|---|
| `internal/app/reveal_darwin.go` | Target-folder conversion, AppleScript arguments, authorization error normalization, and execution. |
| `internal/app/reveal_darwin_test.go` | Deterministic target, argument, and error tests. |
| `README.md` | Finder tab behavior and Accessibility prerequisite. |

### Task 1: Add the Testable Darwin Adapter

**Files:**
- Modify: `internal/app/reveal_darwin.go`
- Modify: `internal/app/reveal_darwin_test.go`

**Consumes:** `platformRevealPath(path string, selectFile bool) error`.

**Produces:** `darwinRevealTarget`, `darwinRevealArgs`, `darwinRevealError`, and the `finderTabRevealScript` constant.

- [x] **Step 1: Write failing helper tests**

Add to `internal/app/reveal_darwin_test.go`:

```go
func TestDarwinRevealTarget(t *testing.T) {
	file := "/tmp/Project Notes/plan.md"
	folder := "/tmp/Project Notes"
	if got := darwinRevealTarget(file, true); got != folder {
		t.Fatalf("file target = %q, want %q", got, folder)
	}
	if got := darwinRevealTarget(folder, false); got != folder {
		t.Fatalf("directory target = %q, want %q", got, folder)
	}
}

func TestDarwinRevealArgsUsesLiteralFolderArgument(t *testing.T) {
	path := "/tmp/规格 draft $?.pdf"
	want := []string{"-e", finderTabRevealScript, "/tmp"}
	if got := darwinRevealArgs(path, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestDarwinRevealErrorAddsAccessibilityHint(t *testing.T) {
	err := darwinRevealError(errors.New("Not authorized to send Apple events"))
	if !strings.Contains(err.Error(), "Accessibility") {
		t.Fatalf("error = %q, want Accessibility hint", err)
	}
}
```

Add `errors` and `strings` to the test imports.

- [x] **Step 2: Run the focused tests and verify failure**

Run: `go test ./internal/app -run 'TestDarwinReveal(Target|ArgsUsesLiteralFolderArgument|ErrorAddsAccessibilityHint)' -count=1`

Expected: FAIL because the helper functions and script constant do not exist.

- [x] **Step 3: Implement Finder tab creation through literal osascript arguments**

Replace `internal/app/reveal_darwin.go` implementation with these declarations, retaining the build tag and header:

```go
const finderTabRevealScript = `on run argv
	set targetFolder to POSIX file (item 1 of argv) as alias
	tell application "Finder"
		activate
		if (count of Finder windows) is 0 then
			make new Finder window to targetFolder
			return
		end if
	end tell
	tell application "System Events"
		tell process "Finder"
			set frontmost to true
			keystroke "t" using {command down}
		end tell
	end tell
	delay 0.1
	tell application "Finder"
		set target of front Finder window to targetFolder
	end tell
end run`

func darwinRevealTarget(path string, selectFile bool) string {
	if selectFile { return filepath.Dir(path) }
	return path
}

func darwinRevealArgs(path string, selectFile bool) []string {
	return []string{"-e", finderTabRevealScript, darwinRevealTarget(path, selectFile)}
}

func darwinRevealError(err error) error {
	if err == nil { return nil }
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "not authorized") || strings.Contains(message, "not allowed assistive access") {
		return fmt.Errorf("Finder tab opening requires Accessibility permission for the terminal running SpiceEdit: %w", err)
	}
	return err
}

func platformRevealPath(path string, selectFile bool) error {
	return darwinRevealError(exec.Command("osascript", darwinRevealArgs(path, selectFile)...).Run())
}
```

Use `fmt`, `os/exec`, `path/filepath`, and `strings` imports. Add concise doc comments to every new helper and the script constant.

- [x] **Step 4: Format and run focused tests**

Run: `gofmt -w internal/app/reveal_darwin.go internal/app/reveal_darwin_test.go && go test ./internal/app -run 'Test(DarwinReveal|ShowInFileManager)' -count=1`

Expected: PASS.

### Task 2: Document and Validate Finder Tab Behavior

**Files:**
- Modify: `README.md`

**Consumes:** Task 1's Darwin behavior.

**Produces:** Clear macOS setup guidance without altering other platform documentation.

- [x] **Step 1: Update the `Esc e` README paragraph**

Add this text after the sentence that describes macOS Finder and Windows Explorer:

```markdown
On macOS, an active file opens its containing folder in a new tab of an
existing Finder window; when Finder has no open window, SpiceEdit opens one.
This requires Accessibility permission for the terminal application running
SpiceEdit. Windows continues to select the active file in Explorer.
```

- [x] **Step 2: Run complete validation**

Run: `git diff --check && make test && make build`

Expected: no whitespace errors, race-enabled tests pass, and `bin/spiceedit` builds.

- [ ] **Step 3: Manually validate on macOS**

1. Grant the terminal application that launches SpiceEdit Accessibility permission in **System Settings → Privacy & Security → Accessibility**.
2. With no Finder windows, press `Esc e` on an active file and verify one Finder window opens at the parent folder.
3. Keep that window open, select a file from another folder in SpiceEdit, press `Esc e`, and verify Finder creates an active new tab at that folder without replacing the existing tab.
4. Revoke Accessibility permission and repeat step 3; verify the editor remains usable and its status message names Accessibility permission.

- [ ] **Step 4: Commit only this change**

Run: `git add internal/app/reveal_darwin.go internal/app/reveal_darwin_test.go README.md docs/superpowers/plans/2026-08-15-finder-tab-reveal.md && git commit -m "Open Finder folders in tabs"`

Expected: existing image-preview draft documents remain uncommitted.

## Plan Review

- Spec coverage: the plan covers no-window and existing-window paths, literal path handling, permission failure, documentation, and macOS manual validation.
- Placeholder scan: all implementation and validation steps are concrete.
- Type consistency: the existing `platformRevealPath(path string, selectFile bool) error` contract remains unchanged.
