// =============================================================================
// File: internal/app/reveal_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudmanic/spice-edit/internal/editor"
)

type revealCall struct {
	path       string
	selectFile bool
}

// TestMenuLayout_ShowInFileManager checks the File actions row remains
// discoverable even when the project-root fallback is the only target.
func TestMenuLayout_ShowInFileManager(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	item := menuItemByLabel(t, a, "Show in file manager")
	if item.shortcut != "Esc e" {
		t.Fatalf("shortcut = %q, want Esc e", item.shortcut)
	}
	if !item.enabled(a) {
		t.Fatal("show-in-file-manager should be enabled for project-root fallback")
	}
}

// TestShowInFileManager_UsesActiveFilePath verifies that every file-backed
// tab mode asks the platform adapter to select the active file.
func TestShowInFileManager_UsesActiveFilePath(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	imagePath := writeImageFixture(t, dir, "photo.png")
	externalPath := filepath.Join(dir, "manual.pdf")
	if err := os.WriteFile(externalPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{textPath, imagePath, externalPath} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			a := newTestApp(t, dir)
			tab, err := editor.NewTab(path)
			if err != nil {
				t.Fatal(err)
			}
			a.tabs = []*editor.Tab{tab}
			a.activeTab = 0
			calls := make(chan revealCall, 1)
			a.revealPath = func(path string, selectFile bool) error {
				calls <- revealCall{path: path, selectFile: selectFile}
				return nil
			}

			a.showInFileManager()
			select {
			case got := <-calls:
				if got.path != path || !got.selectFile {
					t.Fatalf("reveal call = %#v, want path=%q select=true", got, path)
				}
			case <-time.After(time.Second):
				t.Fatal("revealer was not called")
			}
			a.handleEvent(waitForRevealDone(t, a))
		})
	}
}

// TestShowInFileManager_FallsBackToProjectRoot covers both no-tab and
// untitled-tab states without consulting the sidebar's active folder.
func TestShowInFileManager_FallsBackToProjectRoot(t *testing.T) {
	for _, withUntitled := range []bool{false, true} {
		name := "no tab"
		if withUntitled {
			name = "untitled tab"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			a := newTestApp(t, dir)
			if withUntitled {
				tab, err := editor.NewTab("")
				if err != nil {
					t.Fatal(err)
				}
				a.tabs = []*editor.Tab{tab}
				a.activeTab = 0
			}
			a.activeFolder = filepath.Join(dir, "not-the-target")
			calls := make(chan revealCall, 1)
			a.revealPath = func(path string, selectFile bool) error {
				calls <- revealCall{path: path, selectFile: selectFile}
				return nil
			}

			a.showInFileManager()
			select {
			case got := <-calls:
				if got.path != dir || got.selectFile {
					t.Fatalf("reveal call = %#v, want path=%q select=false", got, dir)
				}
			case <-time.After(time.Second):
				t.Fatal("revealer was not called")
			}
			a.handleEvent(waitForRevealDone(t, a))
		})
	}
}

// TestShowInFileManager_PreservesDirtyBuffer verifies the reveal action uses
// only the on-disk path and leaves unsaved editor state untouched.
func TestShowInFileManager_PreservesDirtyBuffer(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(target, []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.openFile(target)
	a.activeTabPtr().InsertString("dirty")
	calls := make(chan revealCall, 1)
	a.revealPath = func(path string, selectFile bool) error {
		calls <- revealCall{path: path, selectFile: selectFile}
		return nil
	}

	a.showInFileManager()
	<-calls
	a.handleEvent(waitForRevealDone(t, a))
	if !a.activeTabPtr().Dirty {
		t.Fatal("reveal cleared dirty state")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "disk" {
		t.Fatalf("disk contents = %q, want unchanged", got)
	}
}

// TestRevealPathInFileManager_PostsFailureStatus keeps platform errors on the
// main event loop and reports the selected file by name.
func TestRevealPathInFileManager_PostsFailureStatus(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.revealPath = func(string, bool) error { return errors.New("desktop unavailable") }

	a.revealPathInFileManager(target, true)
	a.handleEvent(waitForRevealDone(t, a))
	if got := a.statusMsg; got != "Could not show notes.txt in file manager: desktop unavailable" {
		t.Fatalf("status = %q", got)
	}
}

// TestRevealPathInFileManager_MissingPathDoesNotLaunch validates the target
// before any platform-specific call starts.
func TestRevealPathInFileManager_MissingPathDoesNotLaunch(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "missing.txt")
	a := newTestApp(t, dir)
	called := false
	a.revealPath = func(string, bool) error {
		called = true
		return nil
	}

	a.revealPathInFileManager(target, true)
	if called {
		t.Fatal("missing path invoked revealer")
	}
	if got := a.statusMsg; got == "" {
		t.Fatal("missing path did not report an error")
	}
}

// waitForRevealDone polls the simulation screen on a helper goroutine so a
// missing completion event fails with a timeout instead of hanging tests.
func waitForRevealDone(t *testing.T, a *App) *revealDoneEvent {
	t.Helper()
	result := make(chan *revealDoneEvent, 1)
	go func() {
		for {
			ev := a.screen.PollEvent()
			if done, ok := ev.(*revealDoneEvent); ok {
				result <- done
				return
			}
		}
	}()
	select {
	case done := <-result:
		return done
	case <-time.After(time.Second):
		t.Fatal("no revealDoneEvent received")
		return nil
	}
}
