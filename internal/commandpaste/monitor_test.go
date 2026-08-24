// =============================================================================
// File: internal/commandpaste/monitor_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-24
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package commandpaste

import (
	"sync"
	"testing"
	"time"
)

// TestMonitorRecordPreservesTimeValue verifies that recording publishes the
// complete time.Time value, including its monotonic clock reading.
func TestMonitorRecordPreservesTimeValue(t *testing.T) {
	now := time.Now()
	m := newMonitorForTest()
	m.record(now)

	recordedAt := m.recordedAt.Load()
	if recordedAt == nil {
		t.Fatal("recorded time was not published")
	}
	if *recordedAt != now {
		t.Fatalf("recorded time = %v, want exact time value %v", *recordedAt, now)
	}
}

// TestMonitorRecent verifies that Command+V authorization is bounded by the
// configured grace period.
func TestMonitorRecent(t *testing.T) {
	now := time.Now()
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
	now := time.Now()
	m := newMonitorForTest()
	m.record(now)

	if m.Recent(now.Add(-time.Nanosecond)) {
		t.Fatal("future Command+V timestamp authorized terminal paste")
	}
}

// TestMonitorConcurrentRecordAndRecent verifies that the callback and app
// event-loop access paths can safely update and query the monitor together.
func TestMonitorConcurrentRecordAndRecent(t *testing.T) {
	start := time.Now()
	m := newMonitorForTest()

	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for i := 0; i < 1000; i++ {
			m.record(start.Add(time.Duration(i) * time.Nanosecond))
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 1000; i++ {
			m.Recent(start.Add(time.Duration(i) * time.Nanosecond))
		}
	}()
	workers.Wait()
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
