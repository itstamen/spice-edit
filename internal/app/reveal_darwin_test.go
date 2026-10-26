//go:build darwin

// =============================================================================
// File: internal/app/reveal_darwin_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestDarwinRevealSerializesConcurrentRequests prevents overlapping scripts
// from selecting or retargeting each other's Finder tabs.
func TestDarwinRevealSerializesConcurrentRequests(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
if ! /bin/mkdir "$SPICEEDIT_FINDER_TEST_DIR/active" 2>/dev/null; then
	echo "Finder automation overlapped" >&2
	exit 1
fi
trap '/bin/rmdir "$SPICEEDIT_FINDER_TEST_DIR/active"' EXIT
/bin/sleep 0.05
`
	if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("SPICEEDIT_FINDER_TEST_DIR", dir)
	start := make(chan struct{})
	results := make(chan error, 8)
	for range cap(results) {
		go func() {
			<-start
			results <- platformRevealPath(dir, false)
		}()
	}
	close(start)
	for range cap(results) {
		if err := <-results; err != nil {
			t.Errorf("concurrent reveal: %v", err)
		}
	}
}

// TestDarwinRevealTarget maps files to their Finder parent directory.
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

// TestDarwinRevealArgsUsesLiteralFolderArgument keeps special path characters
// out of the AppleScript source and passes the target as one argument.
func TestDarwinRevealArgsUsesLiteralFolderArgument(t *testing.T) {
	path := "/tmp/规格 draft $?.pdf"
	want := []string{"-e", finderTabRevealScript, "/tmp"}
	if got := darwinRevealArgs(path, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

// TestDarwinRevealErrorAddsAccessibilityHint identifies the permission failure
// users must resolve before Finder tab automation can run.
func TestDarwinRevealErrorAddsAccessibilityHint(t *testing.T) {
	tests := []string{
		"Not authorized to send Apple events",
		"not allowed assistive access",
		"not permitted to send keystrokes",
		"\u201cSystem Events\u201d\u9047\u5230\u4e00\u4e2a\u9519\u8bef\uff1a\u201cosascript\u201d\u4e0d\u5141\u8bb8\u53d1\u9001\u6309\u952e\u3002 (1002)",
	}
	for _, message := range tests {
		t.Run(message, func(t *testing.T) {
			err := darwinRevealError(errors.New(message))
			if !strings.Contains(err.Error(), "Accessibility") {
				t.Fatalf("error = %q, want Accessibility hint", err)
			}
		})
	}
}

// TestFinderTabRevealScriptReusesExistingTabs requires a full-path scan before
// the new-tab fallback.
func TestFinderTabRevealScriptReusesExistingTabs(t *testing.T) {
	scanFragments := []string{
		"on activateMatchingFinderTab(targetFolderPath)",
		"set finderWindowIDs to id of every Finder window",
		"repeat with finderWindowID in finderWindowIDs",
		"if my finderWindowTargetPath(candidateID) is targetFolderPath then",
		"set index of Finder window id candidateID to 1",
		"if activeWindowID is candidateID and my finderWindowTargetPath(candidateID) is targetFolderPath then return true",
	}
	last := -1
	for _, fragment := range scanFragments {
		index := strings.Index(finderTabRevealScript, fragment)
		if index == -1 {
			t.Fatalf("script is missing %q", fragment)
		}
		if index < last {
			t.Fatalf("script orders %q before a required prior scan step", fragment)
		}
		last = index
	}
	if strings.Contains(finderTabRevealScript, "click tabButton") {
		t.Fatal("existing tabs should be matched by stable Finder IDs, not stale UI references")
	}

	runFragments := []string{
		"set targetFolderPath to POSIX path of targetFolder",
		"set originalFrontWindowID to id of front Finder window",
		"if my activateMatchingFinderTab(targetFolderPath) then return",
		"set index of Finder window id originalFrontWindowID to 1",
		"set previousWindowIDs to id of every Finder window",
		"keystroke \"t\" using {command down}",
		"set newWindowID to my waitForNewFinderTab(previousWindowIDs)",
		"my openFinderTabFolder(newWindowID, targetFolder, targetFolderPath)",
	}
	last = -1
	for _, fragment := range runFragments {
		index := strings.Index(finderTabRevealScript, fragment)
		if index == -1 {
			t.Fatalf("script is missing %q", fragment)
		}
		if index < last {
			t.Fatalf("script orders %q before a required prior run step", fragment)
		}
		last = index
	}

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
	if strings.Count(finderTabRevealScript, readinessCall) != 1 {
		t.Fatalf("readiness calls = %d, want 1", strings.Count(finderTabRevealScript, readinessCall))
	}
	if strings.LastIndex(finderTabRevealScript, readinessCall) > strings.Index(finderTabRevealScript, "keystroke \"t\" using {command down}") {
		t.Fatal("new-tab shortcut starts before Finder UI readiness check")
	}
}
