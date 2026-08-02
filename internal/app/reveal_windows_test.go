//go:build windows

// =============================================================================
// File: internal/app/reveal_windows_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import "testing"

// TestHRESULTFailed covers success, S_FALSE, and a representative failure.
func TestHRESULTFailed(t *testing.T) {
	if hresultFailed(0) || hresultFailed(1) {
		t.Fatal("successful HRESULT reported as failure")
	}
	if !hresultFailed(uintptr(0x80004005)) {
		t.Fatal("E_FAIL did not report failure")
	}
}
