//go:build darwin || linux

// =============================================================================
// File: internal/app/foldercreate_unix.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-22
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// createFolder creates path using the nearest existing directory as the
// descriptor anchor. The menu action calls createFolderAt with its selected
// parent directly, which is the containment-sensitive path.
func createFolder(path string) error {
	parent, name, err := folderCreateBase(path)
	if err != nil {
		return err
	}
	return createFolderAt(parent, name)
}

// createFolderAt creates a nested relative folder path without following any
// child symlink. Every open and mkdir is relative to an already-open parent
// descriptor, closing the Lstat/MkdirAll time-of-check/time-of-use gap.
func createFolderAt(parent, name string) error {
	fd, err := openUnixDirectoryAnchor(parent)
	if err != nil {
		return err
	}
	defer closeUnixDirectory(&fd)

	components := strings.Split(name, string(filepath.Separator))
	for i, component := range components {
		for {
			next, openErr := unix.Openat(fd, component,
				unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if openErr == nil {
				if i == len(components)-1 {
					unix.Close(next)
					return fmt.Errorf("a folder named %q already exists", filepath.Base(name))
				}
				replaceUnixDirectory(&fd, next)
				break
			}
			if !errors.Is(openErr, unix.ENOENT) {
				return unixNoFollowDirectoryError(fd, component, openErr)
			}
			if mkdirErr := unix.Mkdirat(fd, component, 0o755); mkdirErr != nil {
				if errors.Is(mkdirErr, unix.EEXIST) {
					continue
				}
				return mkdirErr
			}
			if i == len(components)-1 {
				return nil
			}
		}
	}
	return nil
}

// closeUnixDirectory closes the currently owned descriptor at most once.
func closeUnixDirectory(fd *int) {
	if *fd < 0 {
		return
	}
	_ = unix.Close(*fd)
	*fd = -1
}

// replaceUnixDirectory transfers ownership after closing the prior descriptor.
func replaceUnixDirectory(fd *int, next int) {
	closeUnixDirectory(fd)
	*fd = next
}

// openUnixDirectoryAnchor resolves an absolute directory from the trusted
// root descriptor without following any component symlink.
func openUnixDirectoryAnchor(path string) (int, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return -1, err
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	if abs == string(filepath.Separator) {
		return fd, nil
	}
	for _, component := range strings.Split(strings.TrimPrefix(abs, string(filepath.Separator)), string(filepath.Separator)) {
		next, openErr := unix.Openat(fd, component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			err = unixNoFollowDirectoryError(fd, component, openErr)
			unix.Close(fd)
			return -1, err
		}
		unix.Close(fd)
		fd = next
	}
	return fd, nil
}

// unixNoFollowDirectoryError preserves a clear symlink error when Darwin's
// O_DIRECTORY|O_NOFOLLOW reports ENOTDIR for a refused symlink.
func unixNoFollowDirectoryError(parentFD int, component string, openErr error) error {
	if errors.Is(openErr, unix.ELOOP) {
		return fmt.Errorf("folder path cannot traverse symlink %q", component)
	}
	if errors.Is(openErr, unix.ENOTDIR) {
		var stat unix.Stat_t
		if err := unix.Fstatat(parentFD, component, &stat, unix.AT_SYMLINK_NOFOLLOW); err == nil && stat.Mode&unix.S_IFMT == unix.S_IFLNK {
			return fmt.Errorf("folder path cannot traverse symlink %q", component)
		}
	}
	return openErr
}
