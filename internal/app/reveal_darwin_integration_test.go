//go:build darwin

// =============================================================================
// File: internal/app/reveal_darwin_integration_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-10-26
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// runFinderTestScript bounds live AppleScript calls so a stalled Finder cannot
// hang the opt-in integration test indefinitely.
func runFinderTestScript(t *testing.T, script string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "osascript", append([]string{"-e", script}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("osascript: %v: %s", err, output)
	}
	return strings.TrimSpace(string(output))
}

// TestFinderTabRevealIntegration exercises the real Finder with delayed key
// delivery. It is opt-in because it brings Finder forward and creates tabs.
func TestFinderTabRevealIntegration(t *testing.T) {
	if os.Getenv("SPICEEDIT_TEST_FINDER") != "1" {
		t.Skip("set SPICEEDIT_TEST_FINDER=1 to exercise the macOS Finder UI")
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(root, "original folder")
	target := filepath.Join(root, "目标 folder $'\"")
	for _, path := range []string{seed, target} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	frontApp := runFinderTestScript(t, `tell application "System Events" to get bundle identifier of first application process whose frontmost is true`)
	baseline := runFinderTestScript(t, `tell application "Finder" to get id of every Finder window`)
	t.Cleanup(func() {
		// The fixture uses its own Finder window; preserve every pre-existing tab.
		runFinderTestScript(t, `on run argv
			tell application "Finder"
				repeat with w in (get every Finder window)
					if ("," & item 1 of argv & ",") does not contain ("," & (id of w as text) & ",") then close w
				end repeat
			end tell
		end run`, strings.ReplaceAll(baseline, " ", ""))
		runFinderTestScript(t, `on run argv
			tell application id (item 1 of argv) to activate
		end run`, frontApp)
	})
	seedID := runFinderTestScript(t, `on run argv
		tell application "Finder"
			set w to make new Finder window to (POSIX file (item 1 of argv) as alias)
			activate
			return id of w
		end tell
	end run`, seed)
	script := finderTabRevealScript

	t.Run("delayed_new_tab", func(t *testing.T) {
		delayedKey := filepath.Join(root, "delayed-key.applescript")
		if err := os.WriteFile(delayedKey, []byte(`delay 0.4
tell application "System Events" to tell process "Finder" to keystroke "t" using {command down}`), 0o600); err != nil {
			t.Fatal(err)
		}
		delayedScript := strings.Replace(script, `keystroke "t" using {command down}`,
			`do shell script "/usr/bin/osascript " & quoted form of `+strconv.Quote(delayedKey)+` & " >/dev/null 2>&1 &"`, 1)
		runFinderTestScript(t, delayedScript, target)
		// Read after the delayed key has arrived, even when the adapter returned
		// prematurely. This catches a successful exit followed by a Recents tab.
		runFinderTestScript(t, `delay 0.6`)
		assertFinderTestTarget(t, target)
		originalTarget := runFinderTestScript(t, `on run argv
			tell application "Finder" to return POSIX path of (target of Finder window id (item 1 of argv as integer) as alias)
		end run`, seedID)
		if originalTarget != seed+"/" {
			t.Fatalf("original tab changed to %q, want %q", originalTarget, seed+"/")
		}
	})
	if t.Failed() {
		return
	}

	t.Run("reuse_existing_tab", func(t *testing.T) {
		before := runFinderTestScript(t, `tell application "Finder" to count Finder windows`)
		runFinderTestScript(t, script, seed)
		assertFinderTestTarget(t, seed)
		runFinderTestScript(t, script, target)
		assertFinderTestTarget(t, target)
		after := runFinderTestScript(t, `tell application "Finder" to count Finder windows`)
		if after != before {
			t.Fatalf("existing-tab reuse changed tab count from %s to %s", before, after)
		}
	})

	t.Run("missing_new_tab", func(t *testing.T) {
		missingTarget := filepath.Join(root, "missing tab")
		if err := os.Mkdir(missingTarget, 0o755); err != nil {
			t.Fatal(err)
		}
		before := runFinderTestScript(t, `tell application "Finder" to count Finder windows`)
		// Simulate Finder ignoring Cmd+T and shorten the bounded wait. An old
		// tab must never be overwritten or reported as a successful new tab.
		ignoredKeyScript := strings.Replace(script, `keystroke "t" using {command down}`, `-- Key delivery was ignored.`, 1)
		ignoredKeyScript = strings.ReplaceAll(ignoredKeyScript, "repeat 100 times", "repeat 3 times")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "osascript", "-e", ignoredKeyScript, missingTarget).CombinedOutput()
		if err == nil || !strings.Contains(string(output), "Finder did not open a new tab") {
			t.Fatalf("ignored shortcut: error = %v, output = %s", err, output)
		}
		after := runFinderTestScript(t, `tell application "Finder" to count Finder windows`)
		if after != before {
			t.Fatalf("ignored shortcut changed tab count from %s to %s", before, after)
		}
		runFinderTestScript(t, script, target)
		assertFinderTestTarget(t, target)
	})

	t.Run("virtual_location", func(t *testing.T) {
		// Use the user's default new-tab location without changing preferences.
		// When that is Recents, scanning must skip its non-filesystem target.
		location := runFinderTestScript(t, `tell application "System Events" to tell process "Finder" to keystroke "t" using {command down}
			delay 0.5
			tell application "Finder"
				try
					return POSIX path of (target of front Finder window as alias)
				on error
					return "virtual"
				end try
			end tell`)
		if location != "virtual" {
			t.Skip("Finder's default new-tab location is a filesystem folder")
		}
		nextTarget := filepath.Join(root, "after virtual location")
		if err := os.Mkdir(nextTarget, 0o755); err != nil {
			t.Fatal(err)
		}
		runFinderTestScript(t, script, nextTarget)
		assertFinderTestTarget(t, nextTarget)
	})
}

// assertFinderTestTarget reports virtual locations such as Recents explicitly
// instead of failing with Finder's opaque alias-conversion error.
func assertFinderTestTarget(t *testing.T, target string) {
	t.Helper()
	actual := runFinderTestScript(t, `tell application "Finder"
		try
			return POSIX path of (target of front Finder window as alias)
		on error
			return "<virtual location: " & (name of front Finder window) & ">"
		end try
	end tell`)
	if actual != target+"/" {
		t.Fatalf("active Finder tab = %q, want %q", actual, target+"/")
	}
}
