//go:build windows

// =============================================================================
// File: internal/app/defaultopen_windows.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

const shellExecuteShowNormal = 1

var shellExecuteW = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

// platformOpenPath asks Windows ShellExecuteW to apply the registered file
// association without routing the path through cmd.exe parsing.
func platformOpenPath(path string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	result, _, callErr := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0,
		0,
		shellExecuteShowNormal,
	)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(file)
	if result > 32 {
		return nil
	}
	if callErr != syscall.Errno(0) {
		return fmt.Errorf("ShellExecuteW failed: %w", callErr)
	}
	return fmt.Errorf("ShellExecuteW failed with code %d", result)
}
