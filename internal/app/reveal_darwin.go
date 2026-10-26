//go:build darwin

// =============================================================================
// File: internal/app/reveal_darwin.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// finderRevealMu keeps asynchronous reveal requests from interleaving Finder
// activation, tab creation, and navigation within this process.
var finderRevealMu sync.Mutex

// finderTabRevealScript activates or opens the requested Finder folder.
const finderTabRevealScript = `on finderWindowTargetPath(finderWindowID)
	tell application "Finder"
		try
			return POSIX path of (target of Finder window id finderWindowID as alias)
		on error
			-- Virtual locations such as Recents have no filesystem alias.
			return ""
		end try
	end tell
end finderWindowTargetPath

on waitForFinderWindow()
	tell application "System Events"
		tell process "Finder"
			set frontmost to true
			repeat 10 times
				if exists window 1 then return true
				delay 0.05
			end repeat
		end tell
	end tell
	return false
end waitForFinderWindow

on activateMatchingFinderTab(targetFolderPath)
	-- Finder's window collection includes background tabs with stable IDs.
	tell application "Finder" to set finderWindowIDs to id of every Finder window
	repeat with finderWindowID in finderWindowIDs
		set candidateID to contents of finderWindowID
		if my finderWindowTargetPath(candidateID) is targetFolderPath then
			tell application "Finder" to set index of Finder window id candidateID to 1
			repeat 100 times
				tell application "Finder" to set activeWindowID to id of front Finder window
				if activeWindowID is candidateID and my finderWindowTargetPath(candidateID) is targetFolderPath then return true
				delay 0.05
			end repeat
			error "Finder did not activate the requested folder tab before the timeout"
		end if
	end repeat
	return false
end activateMatchingFinderTab

on waitForNewFinderTab(previousWindowIDs)
	repeat 100 times
		tell application "Finder"
			try
				set activeWindowID to id of front Finder window
				if previousWindowIDs does not contain activeWindowID then return activeWindowID
			end try
		end tell
		delay 0.05
	end repeat
	error "Finder did not open a new tab before the timeout"
end waitForNewFinderTab

on openFinderTabFolder(finderWindowID, targetFolder, targetFolderPath)
	repeat 100 times
		try
			tell application "Finder" to set target of Finder window id finderWindowID to targetFolder
			delay 0.05
			tell application "Finder" to set activeWindowID to id of front Finder window
			if activeWindowID is finderWindowID and my finderWindowTargetPath(finderWindowID) is targetFolderPath then return
		end try
		delay 0.05
	end repeat
	error "Finder did not navigate the new tab to the requested folder before the timeout"
end openFinderTabFolder

on run argv
	set targetFolder to POSIX file (item 1 of argv) as alias
	set targetFolderPath to POSIX path of targetFolder
	tell application "Finder"
		activate
		if (count of Finder windows) is 0 then
			make new Finder window to targetFolder
			return
		end if
		set originalFrontWindowID to id of front Finder window
	end tell
	if my activateMatchingFinderTab(targetFolderPath) then return
	tell application "Finder" to set index of Finder window id originalFrontWindowID to 1
	if not my waitForFinderWindow() then error "Finder UI did not become ready for tab automation"
	tell application "Finder" to set previousWindowIDs to id of every Finder window
	tell application "System Events"
		tell process "Finder"
			set frontmost to true
			keystroke "t" using {command down}
		end tell
	end tell
	-- Finder gives each tab an ID, but Cmd+T returns before that tab exists.
	set newWindowID to my waitForNewFinderTab(previousWindowIDs)
	my openFinderTabFolder(newWindowID, targetFolder, targetFolderPath)
end run`

// darwinRevealTarget returns the folder Finder should open for the request.
func darwinRevealTarget(path string, selectFile bool) string {
	if selectFile {
		return filepath.Dir(path)
	}

	return path
}

// darwinRevealArgs returns osascript arguments with the folder as a literal
// argument rather than embedded in the AppleScript source.
func darwinRevealArgs(path string, selectFile bool) []string {
	return []string{"-e", finderTabRevealScript, darwinRevealTarget(path, selectFile)}
}

// darwinRevealError adds setup guidance when accessibility access is denied.
func darwinRevealError(err error) error {
	if err == nil {
		return nil
	}

	message := strings.ToLower(err.Error())
	if strings.Contains(message, "not authorized") ||
		strings.Contains(message, "not allowed assistive access") ||
		strings.Contains(message, "not permitted to send keystrokes") ||
		(strings.Contains(message, "system events") && strings.Contains(message, "(1002)")) {
		return fmt.Errorf("Finder tab opening requires Accessibility permission for the terminal running SpiceEdit: %w", err)
	}

	return err
}

// platformRevealPath opens the requested folder in a Finder window or tab.
func platformRevealPath(path string, selectFile bool) error {
	finderRevealMu.Lock()
	defer finderRevealMu.Unlock()

	output, err := exec.Command("osascript", darwinRevealArgs(path, selectFile)...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			err = fmt.Errorf("%w: %s", err, message)
		}
		return darwinRevealError(err)
	}

	return nil
}
