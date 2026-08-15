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
	"reflect"
	"strings"
	"testing"
)

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
		"set index of Finder window id (contents of finderWindowID) to 1",
		"set tabButtons to radio buttons of tab group 1 of front window",
		"click tabButton",
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
	matchedTabIndex := strings.Index(finderTabRevealScript[last:], "if my activeFinderTargetPath() is targetFolderPath then return true")
	if matchedTabIndex == -1 {
		t.Fatal("script does not compare a clicked tab's folder with the target")
	}

	runFragments := []string{
		"set targetFolderPath to POSIX path of targetFolder",
		"set originalFrontWindow to front Finder window",
		"if my activateMatchingFinderTab(targetFolderPath) then return",
		"set index of originalFrontWindow to 1",
		"keystroke \"t\" using {command down}",
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
}
