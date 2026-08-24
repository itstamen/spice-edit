// =============================================================================
// File: internal/commandpaste/monitor.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-24
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package commandpaste reports whether macOS recently observed a physical
// Command+V key press. Unsupported platforms provide the same API as a no-op.
package commandpaste

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const commandPasteGracePeriod = 500 * time.Millisecond

// ErrPermissionDenied reports that macOS Input Monitoring permission is not
// available for the Command+V listener.
var ErrPermissionDenied = errors.New("Command+V listener permission denied")

// Monitor reports recent physical Command+V input and owns any native
// resources used to observe it.
type Monitor interface {
	Recent(now time.Time) bool
	Close()
}

var (
	_ Monitor = (*timestampMonitor)(nil)
	_ Monitor = noopMonitor{}
)

// timestampMonitor stores the most recent Command+V observation as Unix
// nanoseconds so callbacks and the app event loop do not need a mutex.
type timestampMonitor struct {
	recordedAt atomic.Int64
	closeOnce  sync.Once
	closeFn    func()
}

// newMonitor creates a timestamp monitor with an idempotent close hook.
func newMonitor(closeFn func()) *timestampMonitor {
	if closeFn == nil {
		closeFn = func() {}
	}
	return &timestampMonitor{closeFn: closeFn}
}

// newMonitorForTest creates a monitor without starting native resources.
func newMonitorForTest() *timestampMonitor {
	return newMonitor(nil)
}

// record stores the time of a physical Command+V observation.
func (m *timestampMonitor) record(now time.Time) {
	m.recordedAt.Store(now.UnixNano())
}

// Recent reports whether Command+V was observed within the grace period.
func (m *timestampMonitor) Recent(now time.Time) bool {
	recordedAt := m.recordedAt.Load()
	if recordedAt == 0 {
		return false
	}

	elapsed := now.Sub(time.Unix(0, recordedAt))
	return elapsed >= 0 && elapsed < commandPasteGracePeriod
}

// Close releases the monitor's resources once.
func (m *timestampMonitor) Close() {
	m.closeOnce.Do(m.closeFn)
}

// noopMonitor is returned when native Command+V observation is unavailable.
type noopMonitor struct{}

// Recent always denies terminal paste on a no-op monitor.
func (noopMonitor) Recent(time.Time) bool {
	return false
}

// Close is safe to call repeatedly on a no-op monitor.
func (noopMonitor) Close() {}
