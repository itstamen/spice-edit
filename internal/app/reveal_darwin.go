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
)

// finderTabRevealScript activates or opens the requested Finder folder.
const finderTabRevealScript = `on activeFinderTargetPath()
	tell application "Finder"
		return POSIX path of (target of front Finder window as alias)
	end tell
end activeFinderTargetPath

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
	tell application "Finder" to set finderWindowIDs to id of every Finder window
	repeat with finderWindowID in finderWindowIDs
		tell application "Finder" to set index of Finder window id (contents of finderWindowID) to 1
		delay 0.05
		if not my waitForFinderWindow() then error "Finder UI did not become ready for tab automation"
		tell application "System Events"
			tell process "Finder"
				set tabGroups to tab groups of front window
				if (count of tabGroups) is 0 then
					if my activeFinderTargetPath() is targetFolderPath then return true
				else
					set tabButtons to radio buttons of tab group 1 of front window
					repeat with tabButton in tabButtons
						click tabButton
						delay 0.05
						if my activeFinderTargetPath() is targetFolderPath then return true
					end repeat
				end if
			end tell
		end tell
	end repeat
	return false
end activateMatchingFinderTab

on run argv
	set targetFolder to POSIX file (item 1 of argv) as alias
	set targetFolderPath to POSIX path of targetFolder
	tell application "Finder"
		activate
		if (count of Finder windows) is 0 then
			make new Finder window to targetFolder
			return
		end if
		set originalFrontWindow to front Finder window
	end tell
	if my activateMatchingFinderTab(targetFolderPath) then return
	tell application "Finder" to set index of originalFrontWindow to 1
	if not my waitForFinderWindow() then error "Finder UI did not become ready for tab automation"
	tell application "System Events"
		tell process "Finder"
			set frontmost to true
			keystroke "t" using {command down}
		end tell
	end tell
	delay 0.1
	tell application "Finder" to set target of front Finder window to targetFolder
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
