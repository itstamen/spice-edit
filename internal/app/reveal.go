// =============================================================================
// File: internal/app/reveal.go
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

// pathRevealer shows a file or directory through the host file manager.
type pathRevealer func(path string, selectFile bool) error

// revealDoneEvent returns an asynchronous file-manager result to the main
// tcell event loop, which exclusively owns visible status state.
type revealDoneEvent struct {
	when       time.Time
	path       string
	selectFile bool
	err        error
}

// When satisfies the tcell.Event interface.
func (e *revealDoneEvent) When() time.Time { return e.when }

// showInFileManager selects the active file or opens the project root when no
// file-backed tab is active.
func (a *App) showInFileManager() {
	path := a.rootDir
	selectFile := false
	if tab := a.activeTabPtr(); tab != nil && tab.Path != "" {
		path = tab.Path
		selectFile = true
	}
	a.revealPathInFileManager(path, selectFile)
}

// revealPathInFileManager validates the target and starts the platform call
// away from the UI goroutine so the editor remains responsive.
func (a *App) revealPathInFileManager(path string, selectFile bool) {
	if path == "" {
		a.flash("No path to show in file manager")
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		a.flash(fmt.Sprintf("Could not show %s in file manager: %v", filepath.Base(path), err))
		return
	}
	if _, err := os.Stat(abs); err != nil {
		a.flash(fmt.Sprintf("Could not show %s in file manager: %v", filepath.Base(abs), err))
		return
	}

	revealer := a.revealPath
	if revealer == nil {
		revealer = platformRevealPath
	}
	scr := a.screen
	go func() {
		runErr := revealer(abs, selectFile)
		_ = scr.PostEvent(&revealDoneEvent{
			when:       time.Now(),
			path:       abs,
			selectFile: selectFile,
			err:        runErr,
		})
	}()
}

// handleRevealDone translates one platform result into concise status copy.
func (a *App) handleRevealDone(e *revealDoneEvent) {
	name := filepath.Base(e.path)
	if e.err != nil {
		a.flash(fmt.Sprintf("Could not show %s in file manager: %v", name, e.err))
		return
	}
	if e.selectFile {
		a.flash(fmt.Sprintf("Revealed %s in file manager", name))
		return
	}
	a.flash(fmt.Sprintf("Opened %s in file manager", name))
}
