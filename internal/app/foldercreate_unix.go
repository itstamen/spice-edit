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
	fd, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	components := strings.Split(name, string(filepath.Separator))
	for i, component := range components {
		for {
			next, openErr := unix.Openat(fd, component,
				unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if openErr == nil {
				var stat unix.Stat_t
				if statErr := unix.Fstat(next, &stat); statErr != nil {
					unix.Close(next)
					return statErr
				}
				if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
					unix.Close(next)
					if i == len(components)-1 {
						return fmt.Errorf("a folder named %q already exists", filepath.Base(name))
					}
					return unix.ENOTDIR
				}
				if i == len(components)-1 {
					unix.Close(next)
					return fmt.Errorf("a folder named %q already exists", filepath.Base(name))
				}
				unix.Close(fd)
				fd = next
				break
			}
			if !errors.Is(openErr, unix.ENOENT) {
				if errors.Is(openErr, unix.ELOOP) {
					return fmt.Errorf("folder path cannot traverse symlink %q", component)
				}
				return openErr
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
