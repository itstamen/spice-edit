//go:build !darwin && !linux && !windows

// =============================================================================
// File: internal/app/defaultopen_other.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"fmt"
	"runtime"
)

// platformOpenPath reports platforms without a supported desktop handoff.
func platformOpenPath(string) error {
	return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
}
