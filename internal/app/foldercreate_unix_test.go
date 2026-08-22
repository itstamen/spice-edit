//go:build darwin || linux

// =============================================================================
// File: internal/app/foldercreate_unix_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-22
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestUnixDirectoryOwnershipClosesCurrentDescriptor reuses the original file
// descriptor number after replacement. Deferred cleanup must close the final
// owned descriptor without closing the unrelated resource using the old ID.
func TestUnixDirectoryOwnershipClosesCurrentDescriptor(t *testing.T) {
	root := realTempDir(t)
	first, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open first descriptor: %v", err)
	}
	second, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		unix.Close(first)
		t.Fatalf("open second descriptor: %v", err)
	}
	t.Cleanup(func() {
		_ = unix.Close(first)
		_ = unix.Close(second)
	})

	owned := first
	func() {
		defer closeUnixDirectory(&owned)
		replaceUnixDirectory(&owned, second)
		if err := unix.Dup2(second, first); err != nil {
			t.Fatalf("reuse original descriptor: %v", err)
		}
	}()

	var stat unix.Stat_t
	if err := unix.Fstat(first, &stat); err != nil {
		t.Fatalf("reused descriptor was closed: %v", err)
	}
	if err := unix.Fstat(second, &stat); !errors.Is(err, unix.EBADF) {
		t.Fatalf("final owned descriptor remains open: %v", err)
	}
	if owned != -1 {
		t.Fatalf("owned descriptor = %d, want released sentinel", owned)
	}
}

// realTempDir resolves macOS's /var alias so these tests exercise the folder
// component under test rather than the operating system's own symlink.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return dir
}

// TestDoCreateFolder_RejectsSymlinkedComponent verifies the descriptor-based
// traversal rejects a child symlink before a directory can be created through
// it. It is Unix-only because Windows symlink creation requires privileges.
func TestDoCreateFolder_RejectsSymlinkedComponent(t *testing.T) {
	root := realTempDir(t)
	outside := realTempDir(t)
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

// TestDoCreateFolder_RejectsSymlinkedParentAncestor proves parent anchoring
// refuses an alias before resolving the selected folder through it.
func TestDoCreateFolder_RejectsSymlinkedParentAncestor(t *testing.T) {
	root := realTempDir(t)
	outside := realTempDir(t)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if err := os.Mkdir(filepath.Join(outside, "selected"), 0o755); err != nil {
		t.Fatalf("seed selected folder: %v", err)
	}
	selected := filepath.Join(alias, "selected")
	a := newTestApp(t, root)

	a.doCreateFolder(selected, "new")

	if _, err := os.Stat(filepath.Join(outside, "selected", "new")); !os.IsNotExist(err) {
		t.Fatalf("folder escaped through parent alias: err=%v", err)
	}
	if !strings.Contains(a.statusMsg, "symlink") {
		t.Fatalf("status message = %q, want symlink error", a.statusMsg)
	}
}

// TestDoCreateFolder_AllowsSymlinkedProjectRoot treats the startup root as a
// trusted alias while preserving no-follow traversal for children beneath it.
func TestDoCreateFolder_AllowsSymlinkedProjectRoot(t *testing.T) {
	realRoot := realTempDir(t)
	aliasParent := realTempDir(t)
	aliasRoot := filepath.Join(aliasParent, "project")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatalf("create project-root symlink: %v", err)
	}
	a := newTestApp(t, aliasRoot)

	a.doCreateFolder(aliasRoot, filepath.Join("nested", "new"))

	if info, err := os.Stat(filepath.Join(realRoot, "nested", "new")); err != nil || !info.IsDir() {
		t.Fatalf("folder through trusted root alias: info=%v err=%v", info, err)
	}

	outside := realTempDir(t)
	if err := os.Symlink(outside, filepath.Join(realRoot, "escape")); err != nil {
		t.Fatalf("create descendant symlink: %v", err)
	}
	a.doCreateFolder(aliasRoot, filepath.Join("escape", "blocked"))
	if _, err := os.Stat(filepath.Join(outside, "blocked")); !os.IsNotExist(err) {
		t.Fatalf("folder escaped through descendant symlink: err=%v", err)
	}
	if !strings.Contains(a.statusMsg, "symlink") {
		t.Fatalf("status message = %q, want symlink error", a.statusMsg)
	}
}

// TestDoCreateFolder_FIFOIntermediateReturns confirms a FIFO cannot block the
// no-follow directory traversal or become a parent for the requested folder.
func TestDoCreateFolder_FIFOIntermediateReturns(t *testing.T) {
	root := realTempDir(t)
	pipe := filepath.Join(root, "pipe")
	if err := unix.Mkfifo(pipe, 0o600); err != nil {
		t.Fatalf("create FIFO: %v", err)
	}
	a := newTestApp(t, root)
	done := make(chan struct{})
	go func() {
		a.doCreateFolder(root, filepath.Join("pipe", "child"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		// Release an older blocking FIFO open so the regression test can fail
		// cleanly instead of leaving a goroutine behind.
		writer, err := unix.Open(pipe, unix.O_WRONLY|unix.O_NONBLOCK, 0)
		if err == nil {
			unix.Close(writer)
		}
		<-done
		t.Fatal("FIFO intermediate blocked folder creation")
	}
	if _, err := os.Stat(filepath.Join(root, "pipe", "child")); err == nil {
		t.Fatal("folder created beneath FIFO")
	}
	if !strings.HasPrefix(a.statusMsg, "Create folder failed: ") {
		t.Fatalf("status message = %q, want create-folder error", a.statusMsg)
	}
}
