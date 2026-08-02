//go:build darwin || linux

// =============================================================================
// File: internal/app/defaultopen_unix.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openCommand is an executable plus literal arguments for a Unix desktop
// handoff. Keeping construction pure makes Linux fallback behavior testable.
type openCommand struct {
	name string
	args []string
}

// unixOpenCommand selects macOS open or Linux's xdg-open/gio mechanism.
func unixOpenCommand(goos, path string, lookPath func(string) (string, error)) (openCommand, error) {
	switch goos {
	case "darwin":
		return openCommand{name: "open", args: []string{path}}, nil
	case "linux":
		if name, err := lookPath("xdg-open"); err == nil {
			return openCommand{name: name, args: []string{path}}, nil
		}
		if name, err := lookPath("gio"); err == nil {
			return openCommand{name: name, args: []string{"open", path}}, nil
		}
		return openCommand{}, fmt.Errorf("neither xdg-open nor gio is available")
	default:
		return openCommand{}, fmt.Errorf("unsupported operating system: %s", goos)
	}
}

// platformOpenPath runs the host's standard Unix desktop opener.
func platformOpenPath(path string) error {
	command, err := unixOpenCommand(runtime.GOOS, path, exec.LookPath)
	if err != nil {
		return err
	}
	return exec.Command(command.name, command.args...).Run()
}
