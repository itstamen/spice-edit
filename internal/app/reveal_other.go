//go:build !darwin && !windows

// =============================================================================
// File: internal/app/reveal_other.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"fmt"
	"runtime"
)

// platformRevealPath reports platforms without supported file selection.
func platformRevealPath(string, bool) error {
	return fmt.Errorf("show in file manager is not supported on %s", runtime.GOOS)
}
