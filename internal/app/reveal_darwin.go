//go:build darwin

// =============================================================================
// File: internal/app/reveal_darwin.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import "os/exec"

// darwinRevealArgs returns Finder's literal argument list for selecting a file
// or opening a directory.
func darwinRevealArgs(path string, selectFile bool) []string {
	if selectFile {
		return []string{"-R", path}
	}
	return []string{path}
}

// platformRevealPath asks Finder to select a file or open a directory.
func platformRevealPath(path string, selectFile bool) error {
	return exec.Command("open", darwinRevealArgs(path, selectFile)...).Run()
}
