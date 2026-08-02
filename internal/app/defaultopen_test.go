// =============================================================================
// File: internal/app/defaultopen_test.go
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
)

// TestDefaultOpen_PostsSuccessEvent verifies that the opener runs away from
// the caller and returns status changes through the tcell event queue.
func TestDefaultOpen_PostsSuccessEvent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "manual.pdf")
	if err := os.WriteFile(target, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	opened := make(chan string, 1)
	a.openPath = func(path string) error {
		opened <- path
		return nil
	}

	a.openPathWithDefaultApp(target)
	select {
	case got := <-opened:
		if got != target {
			t.Fatalf("opened path = %q, want %q", got, target)
		}
	case <-time.After(time.Second):
		t.Fatal("opener was not called")
	}

	done := waitForDefaultOpenDone(t, a)
	a.handleEvent(done)
	if got := a.statusMsg; got != "Opened manual.pdf with default app" {
		t.Fatalf("status = %q", got)
	}
}

// TestDefaultOpen_PostsFailureEvent keeps opener failures on the main event
// loop and preserves a concise, filename-specific status message.
func TestDefaultOpen_PostsFailureEvent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "manual.pdf")
	if err := os.WriteFile(target, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, dir)
	a.openPath = func(string) error { return errors.New("no desktop session") }

	a.openPathWithDefaultApp(target)
	done := waitForDefaultOpenDone(t, a)
	a.handleEvent(done)
	if got := a.statusMsg; got != "Could not open manual.pdf: no desktop session" {
		t.Fatalf("status = %q", got)
	}
}

// TestDefaultOpen_NoPathDoesNotLaunch covers direct Esc-o dispatch without an
// active file-backed tab.
func TestDefaultOpen_NoPathDoesNotLaunch(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	called := false
	a.openPath = func(string) error {
		called = true
		return nil
	}
	a.openPathWithDefaultApp("")
	if called {
		t.Fatal("empty path invoked opener")
	}
	if got := a.statusMsg; got != "No file to open with default app" {
		t.Fatalf("status = %q", got)
	}
}

// waitForDefaultOpenDone polls the simulation screen on a helper goroutine so
// a missing completion event fails with a timeout instead of hanging tests.
func waitForDefaultOpenDone(t *testing.T, a *App) *defaultOpenDoneEvent {
	t.Helper()
	result := make(chan *defaultOpenDoneEvent, 1)
	go func() {
		for {
			ev := a.screen.PollEvent()
			if done, ok := ev.(*defaultOpenDoneEvent); ok {
				result <- done
				return
			}
		}
	}()
	select {
	case done := <-result:
		return done
	case <-time.After(time.Second):
		t.Fatal("no defaultOpenDoneEvent received")
		return nil
	}
}
