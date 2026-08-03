// =============================================================================
// File: internal/app/tabclose_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-03
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudmanic/spice-edit/internal/editor"
)

// seedCloseTabs opens one clean text tab per name and returns their stable
// pointers so tests can assert behavior across index changes.
func seedCloseTabs(t *testing.T, names ...string) (*App, []*editor.Tab) {
	t.Helper()
	dir := t.TempDir()
	a := newTestApp(t, dir)
	tabs := make([]*editor.Tab, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("seed"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		a.openFile(path)
		tabs = append(tabs, a.activeTabPtr())
	}
	return a, tabs
}

// TestRequestCloseTabs_CleanOtherTabsKeepsTarget covers the target set used
// by Close other tabs: every non-target tab closes and the target stays active.
func TestRequestCloseTabs_CleanOtherTabsKeepsTarget(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt", "c.txt")
	a.activeTab = 1
	a.syncActiveTreeFile()

	a.requestCloseTabs([]*editor.Tab{tabs[0], tabs[2]})

	if len(a.tabs) != 1 || a.tabs[0] != tabs[1] {
		t.Fatalf("remaining tabs = %v, want only b", a.tabs)
	}
	if a.activeTabPtr() != tabs[1] {
		t.Fatalf("active tab = %v, want b", a.activeTabPtr())
	}
	if a.tree.ActiveFile != tabs[1].Path {
		t.Fatalf("tree active file = %q, want %q", a.tree.ActiveFile, tabs[1].Path)
	}
}

// TestCloseTabs_PreservesSurvivingActivePointer ensures removing an earlier
// tab does not silently switch away from the user's active tab.
func TestCloseTabs_PreservesSurvivingActivePointer(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt", "c.txt")
	a.activeTab = 2

	a.closeTabs([]*editor.Tab{tabs[0]})

	if a.activeTabPtr() != tabs[2] {
		t.Fatalf("active tab = %v, want original c tab", a.activeTabPtr())
	}
	if a.activeTab != 1 {
		t.Fatalf("active index = %d, want 1", a.activeTab)
	}
}

// TestCloseTabs_ChoosesNearestSurvivor pins down the active-tab fallback:
// prefer the nearest tab to the right, then the nearest tab to the left.
func TestCloseTabs_ChoosesNearestSurvivor(t *testing.T) {
	t.Run("right", func(t *testing.T) {
		a, tabs := seedCloseTabs(t, "a.txt", "b.txt", "c.txt")
		a.activeTab = 1

		a.closeTabs([]*editor.Tab{tabs[1]})

		if a.activeTabPtr() != tabs[2] {
			t.Fatalf("active tab = %v, want c", a.activeTabPtr())
		}
	})

	t.Run("left", func(t *testing.T) {
		a, tabs := seedCloseTabs(t, "a.txt", "b.txt", "c.txt")
		a.activeTab = 2

		a.closeTabs([]*editor.Tab{tabs[2]})

		if a.activeTabPtr() != tabs[1] {
			t.Fatalf("active tab = %v, want b", a.activeTabPtr())
		}
	})
}

// TestCloseTabs_AllClearsTreeHighlight verifies the empty editor state does
// not leave a stale active-file marker in the sidebar.
func TestCloseTabs_AllClearsTreeHighlight(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt")

	a.closeTabs(tabs)

	if len(a.tabs) != 0 {
		t.Fatalf("tabs left = %d, want 0", len(a.tabs))
	}
	if a.activeTab != 0 {
		t.Fatalf("active index = %d, want empty-state 0", a.activeTab)
	}
	if a.tree.ActiveFile != "" {
		t.Fatalf("tree active file = %q, want empty", a.tree.ActiveFile)
	}
}

// TestRequestCloseTabs_DirtyCancelKeepsEverything verifies one batch prompt
// protects all target buffers when the user cancels.
func TestRequestCloseTabs_DirtyCancelKeepsEverything(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt")
	for _, tab := range tabs {
		tab.InsertString(" edit")
	}

	a.requestCloseTabs(tabs)
	if !a.dirtyOpen || a.dirtyHover != 0 {
		t.Fatalf("dirtyOpen=%v hover=%d, want open on Cancel", a.dirtyOpen, a.dirtyHover)
	}
	if a.dirtyMessage != "2 files have unsaved changes." {
		t.Fatalf("dirty message = %q", a.dirtyMessage)
	}
	a.dirtyActivate()

	if len(a.tabs) != 2 || !tabs[0].Dirty || !tabs[1].Dirty {
		t.Fatalf("cancel changed targets: tabs=%d dirty=%v/%v", len(a.tabs), tabs[0].Dirty, tabs[1].Dirty)
	}
}

// TestRequestCloseTabs_DirtyDiscardClosesWithoutSaving drops every target
// buffer while preserving the original disk contents.
func TestRequestCloseTabs_DirtyDiscardClosesWithoutSaving(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt")
	for _, tab := range tabs {
		tab.InsertString(" edit")
	}

	a.requestCloseTabs(tabs)
	a.dirtyHover = 1
	a.dirtyActivate()

	if len(a.tabs) != 0 {
		t.Fatalf("tabs left = %d, want 0", len(a.tabs))
	}
	for _, tab := range tabs {
		got, err := os.ReadFile(tab.Path)
		if err != nil {
			t.Fatalf("read %s: %v", tab.Path, err)
		}
		if string(got) != "seed" {
			t.Fatalf("discard wrote %s: %q", tab.Path, got)
		}
	}
}

// TestRequestCloseTabs_DirtySaveOnlyTargets leaves a dirty non-target tab
// untouched while saving and closing the requested target set.
func TestRequestCloseTabs_DirtySaveOnlyTargets(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt", "c.txt")
	for _, tab := range tabs {
		tab.InsertString(" edit")
	}
	targets := []*editor.Tab{tabs[0], tabs[2]}

	a.requestCloseTabs(targets)
	a.dirtyHover = 2
	a.dirtyActivate()

	if len(a.tabs) != 1 || a.tabs[0] != tabs[1] {
		t.Fatalf("remaining tabs = %v, want only b", a.tabs)
	}
	if !tabs[1].Dirty {
		t.Fatal("non-target dirty tab was saved")
	}
	for _, tab := range targets {
		got, err := os.ReadFile(tab.Path)
		if err != nil {
			t.Fatalf("read %s: %v", tab.Path, err)
		}
		if string(got) != " editseed" {
			t.Fatalf("target %s contents = %q", tab.Path, got)
		}
	}
	got, err := os.ReadFile(tabs[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "seed" {
		t.Fatalf("non-target disk contents = %q, want seed", got)
	}
}

// TestRequestCloseTabs_OneDirtyTargetNamesFile uses the single-file wording
// even when the close set also contains clean tabs.
func TestRequestCloseTabs_OneDirtyTargetNamesFile(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt")
	tabs[0].InsertString(" edit")

	a.requestCloseTabs(tabs)

	if a.dirtyMessage != "a.txt has unsaved changes." {
		t.Fatalf("dirty message = %q", a.dirtyMessage)
	}
}

// TestRequestCloseTabs_SaveFailureClosesNothing verifies close atomicity. A
// successful earlier save stays saved, but no target disappears after a later
// untitled-tab save failure.
func TestRequestCloseTabs_SaveFailureClosesNothing(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt")
	fileTab := tabs[0]
	fileTab.InsertString(" edit")
	untitled, err := editor.NewTab("")
	if err != nil {
		t.Fatal(err)
	}
	untitled.InsertString("cannot save")
	a.tabs = append(a.tabs, untitled)
	a.activeTab = 1

	a.requestCloseTabs([]*editor.Tab{fileTab, untitled})
	a.dirtyHover = 2
	a.dirtyActivate()

	if len(a.tabs) != 2 || a.tabs[0] != fileTab || a.tabs[1] != untitled {
		t.Fatalf("save failure closed targets: %v", a.tabs)
	}
	if fileTab.Dirty {
		t.Fatal("successful earlier save should leave the file tab clean")
	}
	if !untitled.Dirty {
		t.Fatal("failed untitled save should remain dirty")
	}
	got, err := os.ReadFile(fileTab.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != " editseed" {
		t.Fatalf("saved file contents = %q", got)
	}
	if a.statusMsg != "Saving untitled tabs is not supported yet" {
		t.Fatalf("status = %q", a.statusMsg)
	}
}
