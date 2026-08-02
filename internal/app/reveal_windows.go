//go:build windows

// =============================================================================
// File: internal/app/reveal_windows.go
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

	"golang.org/x/sys/windows"
)

var (
	revealShell32              = windows.NewLazySystemDLL("shell32.dll")
	shParseDisplayName         = revealShell32.NewProc("SHParseDisplayName")
	shOpenFolderAndSelectItems = revealShell32.NewProc("SHOpenFolderAndSelectItems")
)

// platformRevealPath asks Explorer to select a file or opens a directory with
// the existing ShellExecuteW adapter.
func platformRevealPath(path string, selectFile bool) error {
	if !selectFile {
		return platformOpenPath(path)
	}

	// COM initialization and every dependent Shell call must stay on the same
	// Windows thread even though this function already runs in a goroutine.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	uninitialize, err := initializeRevealCOM()
	if err != nil {
		return err
	}
	if uninitialize {
		defer windows.CoUninitialize()
	}

	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var pidl uintptr
	hr, _, _ := shParseDisplayName.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&pidl)),
		0,
		0,
	)
	runtime.KeepAlive(pathPtr)
	if hresultFailed(hr) {
		return hresultError("SHParseDisplayName", hr)
	}
	if pidl == 0 {
		return fmt.Errorf("SHParseDisplayName returned an empty item identifier")
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(pidl))

	hr, _, _ = shOpenFolderAndSelectItems.Call(pidl, 0, 0, 0)
	if hresultFailed(hr) {
		return hresultError("SHOpenFolderAndSelectItems", hr)
	}
	return nil
}

// initializeRevealCOM initializes COM for the current locked OS thread.
// The bool reports whether the caller must balance it with CoUninitialize.
func initializeRevealCOM() (bool, error) {
	err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	if err == nil {
		return true, nil
	}
	errno, ok := err.(syscall.Errno)
	if !ok {
		return false, fmt.Errorf("CoInitializeEx failed: %w", err)
	}
	switch uint32(errno) {
	case uint32(windows.S_FALSE):
		return true, nil
	case uint32(windows.RPC_E_CHANGED_MODE):
		return false, nil
	default:
		return false, fmt.Errorf("CoInitializeEx failed: HRESULT 0x%08x", uint32(errno))
	}
}

// hresultFailed reports whether a Windows HRESULT has its failure bit set.
func hresultFailed(hr uintptr) bool {
	return int32(uint32(hr)) < 0
}

// hresultError formats a stable error without relying on thread-local last
// error state, which HRESULT-returning Shell APIs do not define.
func hresultError(name string, hr uintptr) error {
	return fmt.Errorf("%s failed: HRESULT 0x%08x", name, uint32(hr))
}
