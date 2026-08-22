//go:build darwin || linux

// =============================================================================
// File: internal/app/foldercreate_unix_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-22
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoCreateFolder_RejectsSymlinkedComponent verifies the descriptor-based
// traversal rejects a child symlink before a directory can be created through
// it. It is Unix-only because Windows symlink creation requires privileges.
func TestDoCreateFolder_RejectsSymlinkedComponent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	a := newTestApp(t, root)

	a.doCreateFolder(root, filepath.Join("link", "new"))

	if _, err := os.Stat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
		t.Fatalf("folder escaped through symlink: err=%v", err)
	}
	if !strings.Contains(a.statusMsg, "symlink") {
		t.Fatalf("status message = %q, want symlink error", a.statusMsg)
	}
}
