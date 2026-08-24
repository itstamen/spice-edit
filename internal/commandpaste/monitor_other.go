//go:build !darwin || !cgo

// =============================================================================
// File: internal/commandpaste/monitor_other.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-24
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package commandpaste

// Start returns a monitor that never authorizes terminal paste on platforms
// without the macOS CoreGraphics listener.
func Start() (Monitor, error) {
	return noopMonitor{}, nil
}
