// =============================================================================
// File: internal/app/tabcontext_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-03
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// assertContextLabels compares the complete context menu in display order.
func assertContextLabels(t *testing.T, a *App, want []string) {
	t.Helper()
	if len(a.contextItems) != len(want) {
		t.Fatalf("context item count = %d, want %d", len(a.contextItems), len(want))
	}
	for i, label := range want {
		if a.contextItems[i].label != label {
			t.Fatalf("context item %d = %q, want %q", i, a.contextItems[i].label, label)
		}
	}
}

// TestHandleMouse_RightClickTabActivatesAndOpensContext verifies an inactive
// tab becomes active before its three close actions are exposed.
func TestHandleMouse_RightClickTabActivatesAndOpensContext(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt", "b.txt")
	a.draw()
	r := a.lastTabRects[0]

	a.handleMouse(tcell.NewEventMouse(r.X+1, 0, tcell.Button3, tcell.ModNone))

	if a.activeTab != 0 || a.activeTabPtr() != tabs[0] {
		t.Fatalf("activeTab=%d active=%v, want tab a", a.activeTab, a.activeTabPtr())
	}
	if a.tree.ActiveFile != tabs[0].Path {
		t.Fatalf("tree active file = %q, want %q", a.tree.ActiveFile, tabs[0].Path)
	}
	if !a.contextOpen || a.menuOpen {
		t.Fatalf("contextOpen=%v menuOpen=%v", a.contextOpen, a.menuOpen)
	}
	assertContextLabels(t, a, []string{"Close tab", "Close other tabs", "Close all tabs"})
}

// TestHandleMouse_RightClickTabCloseCell opens the context menu without
// treating Button3 on × as an immediate close request.
func TestHandleMouse_RightClickTabCloseCell(t *testing.T) {
	a, _ := seedCloseTabs(t, "a.txt", "b.txt")
	a.draw()
	r := a.lastTabRects[0]

	a.handleMouse(tcell.NewEventMouse(r.CloseX, 0, tcell.Button3, tcell.ModNone))

	if len(a.tabs) != 2 {
		t.Fatalf("right-click close cell removed a tab; %d remain", len(a.tabs))
	}
	if !a.contextOpen {
		t.Fatal("right-click close cell should open tab context")
	}
}

// TestHandleMouse_RightClickOutsideTabsPreservesExistingRouting verifies the
// three close rows never leak into the tree, menu button, whitespace, or body.
func TestHandleMouse_RightClickOutsideTabsPreservesExistingRouting(t *testing.T) {
	t.Run("menu button", func(t *testing.T) {
		a, _ := seedCloseTabs(t, "a.txt")
		a.draw()
		x, _, _, _ := a.menuButtonRect()
		a.handleMouse(tcell.NewEventMouse(x, 0, tcell.Button3, tcell.ModNone))
		if !a.menuOpen || a.contextOpen {
			t.Fatalf("menuOpen=%v contextOpen=%v", a.menuOpen, a.contextOpen)
		}
	})

	t.Run("tab whitespace", func(t *testing.T) {
		a, _ := seedCloseTabs(t, "a.txt")
		a.draw()
		r := a.lastTabRects[0]
		x := r.X + r.Width + 3
		a.handleMouse(tcell.NewEventMouse(x, 0, tcell.Button3, tcell.ModNone))
		if !a.menuOpen || a.contextOpen {
			t.Fatalf("menuOpen=%v contextOpen=%v", a.menuOpen, a.contextOpen)
		}
	})

	t.Run("editor body", func(t *testing.T) {
		a, _ := seedCloseTabs(t, "a.txt")
		a.draw()
		x, y, _, _ := a.editorRect()
		a.handleMouse(tcell.NewEventMouse(x+2, y+2, tcell.Button3, tcell.ModNone))
		if !a.menuOpen || a.contextOpen {
			t.Fatalf("menuOpen=%v contextOpen=%v", a.menuOpen, a.contextOpen)
		}
	})

	t.Run("file tree", func(t *testing.T) {
		a, _ := seedCloseTabs(t, "a.txt")
		a.draw()
		a.handleMouse(tcell.NewEventMouse(1, 1, tcell.Button3, tcell.ModNone))
		if !a.contextOpen || a.menuOpen {
			t.Fatalf("contextOpen=%v menuOpen=%v", a.contextOpen, a.menuOpen)
		}
		if len(a.contextItems) == 3 && a.contextItems[0].label == "Close tab" {
			t.Fatal("file-tree right-click exposed tab close actions")
		}
	})
}

// TestOpenTabContext_EnabledStates pins the one-tab Close other tabs rule.
func TestOpenTabContext_EnabledStates(t *testing.T) {
	a, tabs := seedCloseTabs(t, "a.txt")
	a.openTabContext(tabs[0], 20, 0)
	if !a.contextItems[0].isEnabled(a) {
		t.Fatal("Close tab should be enabled")
	}
	if a.contextItems[1].isEnabled(a) {
		t.Fatal("Close other tabs should be disabled with one tab")
	}
	if !a.contextItems[2].isEnabled(a) {
		t.Fatal("Close all tabs should be enabled")
	}

	b, more := seedCloseTabs(t, "b.txt", "c.txt")
	b.openTabContext(more[0], 20, 0)
	for i, item := range b.contextItems {
		if !item.isEnabled(b) {
			t.Fatalf("item %d should be enabled with two tabs", i)
		}
	}
}

// TestOpenTabContext_ActionsWireToCloseModel exercises all three callbacks
// against clean tabs so menu wiring cannot drift from the close helpers.
func TestOpenTabContext_ActionsWireToCloseModel(t *testing.T) {
	t.Run("close tab", func(t *testing.T) {
		a, tabs := seedCloseTabs(t, "a.txt", "b.txt")
		a.openTabContext(tabs[0], 20, 0)
		a.contextHover = 0
		a.contextActivate()
		if len(a.tabs) != 1 || a.tabs[0] != tabs[1] {
			t.Fatalf("remaining tabs = %v, want b", a.tabs)
		}
	})

	t.Run("close other tabs", func(t *testing.T) {
		a, tabs := seedCloseTabs(t, "a.txt", "b.txt", "c.txt")
		a.activeTab = 1
		a.openTabContext(tabs[1], 20, 0)
		a.contextHover = 1
		a.contextActivate()
		if len(a.tabs) != 1 || a.tabs[0] != tabs[1] {
			t.Fatalf("remaining tabs = %v, want b", a.tabs)
		}
	})

	t.Run("close all tabs", func(t *testing.T) {
		a, tabs := seedCloseTabs(t, "a.txt", "b.txt")
		a.openTabContext(tabs[0], 20, 0)
		a.contextHover = 2
		a.contextActivate()
		if len(a.tabs) != 0 {
			t.Fatalf("tabs left = %d, want 0", len(a.tabs))
		}
	})
}

// TestContextMenuWidth_FitsTabLabels prevents silent clipping of the longest
// tab action inside the shared fixed-width popup.
func TestContextMenuWidth_FitsTabLabels(t *testing.T) {
	if contextMenuWidth-5 < textWidth("Close other tabs") {
		t.Fatalf("context content width = %d, want at least %d", contextMenuWidth-5, textWidth("Close other tabs"))
	}
}
