//go:build !darwin || !cgo

// =============================================================================
// File: internal/commandpaste/monitor_other_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-24
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package commandpaste

import (
	"testing"
	"time"
)

// TestStartReturnsNoopMonitor verifies the unsupported-platform contract.
func TestStartReturnsNoopMonitor(t *testing.T) {
	m, err := Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if m.Recent(time.Now()) {
		t.Fatal("no-op monitor authorized terminal paste")
	}

	m.Close()
	m.Close()
}
