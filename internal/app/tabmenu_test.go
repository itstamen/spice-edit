// =============================================================================
// File: internal/app/tabmenu_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-03
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import "testing"

// TestBuiltinMenuGroups_TabCloseActions pins the main-menu fallback used when
// a terminal or outer pane manager does not forward right-click events.
func TestBuiltinMenuGroups_TabCloseActions(t *testing.T) {
	want := []string{
		"Save",
		"Save & close tab",
		"Close tab",
		"Close other tabs",
		"Close all tabs",
	}
	group := builtinMenuGroups()[0]
	if len(group) != len(want) {
		t.Fatalf("tab action count = %d, want %d", len(group), len(want))
	}
	for i, label := range want {
		if group[i].label != label {
			t.Fatalf("tab action %d = %q, want %q", i, group[i].label, label)
		}
	}
}

// TestMenuLayout_TabCloseEnabledStates verifies batch-close actions cannot be
// selected when their target set would be empty.
func TestMenuLayout_TabCloseEnabledStates(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	if menuItemByLabel(t, a, "Close other tabs").enabled(a) {
		t.Fatal("close-other should be disabled without tabs")
	}
	if menuItemByLabel(t, a, "Close all tabs").enabled(a) {
		t.Fatal("close-all should be disabled without tabs")
	}

	one, _ := seedCloseTabs(t, "a.txt")
	if menuItemByLabel(t, one, "Close other tabs").enabled(one) {
		t.Fatal("close-other should be disabled with one tab")
	}
	if !menuItemByLabel(t, one, "Close all tabs").enabled(one) {
		t.Fatal("close-all should be enabled with one tab")
	}

	two, _ := seedCloseTabs(t, "a.txt", "b.txt")
	if !menuItemByLabel(t, two, "Close other tabs").enabled(two) {
		t.Fatal("close-other should be enabled with two tabs")
	}
	if !menuItemByLabel(t, two, "Close all tabs").enabled(two) {
		t.Fatal("close-all should be enabled with two tabs")
	}
}

// TestMenuCloseOtherTabs_KeepsActiveTab verifies the menu wrapper closes its
// own modal and delegates the captured non-active target set.
func TestMenuCloseOtherTabs_KeepsActiveTab(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt", "c.txt")
	a.activeTab = 1
	a.menuOpen = true

	a.menuCloseOtherTabs()

	if a.menuOpen || len(a.tabs) != 1 || a.tabs[0] != tabs[1] {
		t.Fatalf("menuOpen=%v tabs=%v, want only active b", a.menuOpen, a.tabs)
	}
}

// TestMenuCloseAllTabs_ClosesCleanTabs verifies the main-menu action routes
// clean tabs through the shared batch-close implementation.
func TestMenuCloseAllTabs_ClosesCleanTabs(t *testing.T) {
	a, _ := seedCloseTabs(t, "a.txt", "b.txt")
	a.menuOpen = true

	a.menuCloseAllTabs()

	if a.menuOpen || len(a.tabs) != 0 {
		t.Fatalf("menuOpen=%v tabs=%d, want closed", a.menuOpen, len(a.tabs))
	}
}

// TestMenuCloseAllTabs_DirtyCancelKeepsTabs verifies the menu action reuses
// the existing one-modal Save / Discard / Cancel behavior for dirty targets.
func TestMenuCloseAllTabs_DirtyCancelKeepsTabs(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt")
	for _, tab := range tabs {
		tab.InsertString(" edit")
	}
	a.menuOpen = true

	a.menuCloseAllTabs()

	if a.menuOpen || !a.dirtyOpen || a.dirtyHover != 0 {
		t.Fatalf("menuOpen=%v dirtyOpen=%v hover=%d, want batch prompt on Cancel", a.menuOpen, a.dirtyOpen, a.dirtyHover)
	}
	if a.dirtyMessage != "2 files have unsaved changes." {
		t.Fatalf("dirty message = %q", a.dirtyMessage)
	}
	a.dirtyActivate()
	if len(a.tabs) != 2 || !tabs[0].Dirty || !tabs[1].Dirty {
		t.Fatalf("cancel changed targets: tabs=%d dirty=%v/%v", len(a.tabs), tabs[0].Dirty, tabs[1].Dirty)
	}
}
