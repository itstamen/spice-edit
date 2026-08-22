//go:build windows

// =============================================================================
// File: internal/app/foldercreate_windows.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-22
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
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

// createFolderAt creates a nested relative path through directory handles.
// OBJ_DONT_REPARSE prevents child junctions and symlinks from redirecting the
// operation outside the selected parent while a path is being created.
func createFolderAt(parent, name string) error {
	parentPath, err := filepath.Abs(parent)
	if err != nil {
		return err
	}
	fd, err := openWindowsDirectory(`\??\`+parentPath, 0, windows.FILE_OPEN, false)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(fd)

	components := strings.Split(name, string(filepath.Separator))
	for i, component := range components {
		for {
			next, openErr := openWindowsDirectory(component, fd, windows.FILE_OPEN, true)
			if openErr == nil {
				if i == len(components)-1 {
					windows.CloseHandle(next)
					return fmt.Errorf("a folder named %q already exists", filepath.Base(name))
				}
				windows.CloseHandle(fd)
				fd = next
				break
			}
			if !windowsPathMissing(openErr) {
				if windowsPathReparse(openErr) {
					return fmt.Errorf("folder path cannot traverse symlink %q", component)
				}
				return openErr
			}
			next, createErr := openWindowsDirectory(component, fd, windows.FILE_CREATE, true)
			if createErr != nil {
				if windowsPathExists(createErr) {
					continue
				}
				if windowsPathReparse(createErr) {
					return fmt.Errorf("folder path cannot traverse symlink %q", component)
				}
				return createErr
			}
			if i == len(components)-1 {
				windows.CloseHandle(next)
				return nil
			}
			windows.CloseHandle(fd)
			fd = next
			break
		}
	}
	return nil
}

// openWindowsDirectory opens or creates name relative to root. A zero root
// accepts the NT absolute path used for the initially selected parent.
func openWindowsDirectory(name string, root windows.Handle, disposition uint32, noReparse bool) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, err
	}
	attributes := uint32(windows.OBJ_CASE_INSENSITIVE)
	if noReparse {
		attributes |= windows.OBJ_DONT_REPARSE
	}
	oa := &windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: root,
		ObjectName:    objectName,
		Attributes:    attributes,
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	var allocation int64
	err = windows.NtCreateFile(
		&handle,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE,
		oa,
		&status,
		&allocation,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		disposition,
		windows.FILE_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if err != nil {
		return 0, err
	}
	return handle, nil
}

// windowsPathMissing reports the two statuses returned when a relative child
// is absent below an already-open directory.
func windowsPathMissing(err error) bool {
	return err == windows.STATUS_OBJECT_NAME_NOT_FOUND || err == windows.STATUS_NO_SUCH_FILE
}

// windowsPathExists reports the collision status from a concurrent create.
func windowsPathExists(err error) bool {
	return err == windows.STATUS_OBJECT_NAME_COLLISION || err == windows.STATUS_OBJECT_NAME_EXISTS
}

// windowsPathReparse reports a reparse-point refusal from OBJ_DONT_REPARSE.
func windowsPathReparse(err error) bool {
	return err == windows.STATUS_STOPPED_ON_SYMLINK || err == windows.STATUS_REPARSE_POINT_ENCOUNTERED
}
