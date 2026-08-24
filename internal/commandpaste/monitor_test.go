// =============================================================================
// File: internal/commandpaste/monitor_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-24
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package commandpaste

import (
	"testing"
	"time"
)

// TestMonitorRecent verifies that Command+V authorization is bounded by the
// configured grace period.
func TestMonitorRecent(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	m := newMonitorForTest()
	if m.Recent(now) {
		t.Fatal("unrecorded monitor authorized terminal paste")
	}

	m.record(now)
	if !m.Recent(now.Add(commandPasteGracePeriod - time.Nanosecond)) {
		t.Fatal("fresh Command+V was not authorized")
	}
	if m.Recent(now.Add(commandPasteGracePeriod)) {
		t.Fatal("expired Command+V still authorized terminal paste")
	}
}

// TestMonitorRejectsFutureTimestamp verifies that clock rollback cannot
// authorize a terminal paste.
func TestMonitorRejectsFutureTimestamp(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	m := newMonitorForTest()
	m.record(now)

	if m.Recent(now.Add(-time.Nanosecond)) {
		t.Fatal("future Command+V timestamp authorized terminal paste")
	}
}

// TestMonitorCloseIsIdempotent verifies that native shutdown is invoked once.
func TestMonitorCloseIsIdempotent(t *testing.T) {
	closeCalls := 0
	m := newMonitor(func() {
		closeCalls++
	})

	m.Close()
	m.Close()
	if closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", closeCalls)
	}
}
