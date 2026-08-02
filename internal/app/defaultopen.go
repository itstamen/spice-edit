// =============================================================================
// File: internal/app/defaultopen.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// pathOpener hands one local path to the host operating system.
type pathOpener func(string) error

// defaultOpenDoneEvent returns an asynchronous opener result to the main
// tcell event loop, which exclusively owns visible status state.
type defaultOpenDoneEvent struct {
	when time.Time
	path string
	err  error
}

// When satisfies the tcell.Event interface.
func (e *defaultOpenDoneEvent) When() time.Time { return e.when }

// openPathWithDefaultApp validates path and starts the selected opener away
// from the UI goroutine so a slow or broken desktop handoff cannot freeze it.
func (a *App) openPathWithDefaultApp(path string) {
	if path == "" {
		a.flash("No file to open with default app")
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		a.flash(fmt.Sprintf("Could not open %s: %v", filepath.Base(path), err))
		return
	}
	if _, err := os.Stat(abs); err != nil {
		a.flash(fmt.Sprintf("Could not open %s: %v", filepath.Base(abs), err))
		return
	}

	opener := a.openPath
	if opener == nil {
		opener = platformOpenPath
	}
	scr := a.screen
	go func() {
		runErr := opener(abs)
		_ = scr.PostEvent(&defaultOpenDoneEvent{
			when: time.Now(),
			path: abs,
			err:  runErr,
		})
	}()
}

// handleDefaultOpenDone translates one opener result into a concise status
// message without changing tab selection, dirty state, or undo history.
func (a *App) handleDefaultOpenDone(e *defaultOpenDoneEvent) {
	name := filepath.Base(e.path)
	if e.err != nil {
		a.flash(fmt.Sprintf("Could not open %s: %v", name, e.err))
		return
	}
	a.flash(fmt.Sprintf("Opened %s with default app", name))
}
