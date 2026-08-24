//go:build darwin && cgo

// =============================================================================
// File: internal/commandpaste/monitor_darwin.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-24
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package commandpaste

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>
#include <pthread.h>
#include <stdbool.h>
#include <stdlib.h>

extern void spiceCommandPasteRecorded(void);

enum {
	SpiceCommandPasteStartOK = 0,
	SpiceCommandPasteStartTapFailed = 1,
	SpiceCommandPasteStartSourceFailed = 2,
	SpiceCommandPasteStartThreadFailed = 3,
	SpiceCommandPasteStartAlreadyRunning = 4,
	SpiceKeyCodeANSI_V = 9
};

typedef struct {
	CFMachPortRef tap;
	CFRunLoopSourceRef source;
	CFRunLoopRef runLoop;
	pthread_t thread;
	pthread_mutex_t mutex;
	pthread_cond_t readyCondition;
	bool ready;
	bool stopRequested;
} SpiceCommandPasteContext;

static pthread_mutex_t spiceCommandPasteGlobalMutex = PTHREAD_MUTEX_INITIALIZER;
static SpiceCommandPasteContext *spiceCommandPasteContext = NULL;

static int spiceCommandPasteHasPermission(void) {
	return CGPreflightListenEventAccess() ? 1 : 0;
}

static void spiceCommandPasteRequestPermission(void) {
	CGRequestListenEventAccess();
}

static CGEventRef spiceCommandPasteCallback(
	CGEventTapProxy proxy,
	CGEventType type,
	CGEventRef event,
	void *userInfo
) {
	(void)proxy;
	(void)userInfo;

	if (type == kCGEventKeyDown && event != NULL) {
		CGEventFlags flags = CGEventGetFlags(event);
		CGKeyCode keyCode = (CGKeyCode)CGEventGetIntegerValueField(
			event,
			kCGKeyboardEventKeycode
		);
		if ((flags & kCGEventFlagMaskCommand) != 0 && keyCode == SpiceKeyCodeANSI_V) {
			spiceCommandPasteRecorded();
		}
	}

	return event;
}

static void *spiceCommandPasteRunLoop(void *argument) {
	SpiceCommandPasteContext *context = (SpiceCommandPasteContext *)argument;
	CFRunLoopRef runLoop = CFRunLoopGetCurrent();
	CFRetain(runLoop);
	CFRunLoopAddSource(runLoop, context->source, kCFRunLoopCommonModes);
	CGEventTapEnable(context->tap, true);

	pthread_mutex_lock(&context->mutex);
	context->runLoop = runLoop;
	context->ready = true;
	pthread_cond_signal(&context->readyCondition);
	pthread_mutex_unlock(&context->mutex);

	for (;;) {
		pthread_mutex_lock(&context->mutex);
		bool stopRequested = context->stopRequested;
		pthread_mutex_unlock(&context->mutex);
		if (stopRequested) {
			break;
		}

		CFRunLoopRunInMode(kCFRunLoopDefaultMode, 60.0, false);
	}

	CFRunLoopRemoveSource(runLoop, context->source, kCFRunLoopCommonModes);
	return NULL;
}

static void spiceCommandPasteDestroy(SpiceCommandPasteContext *context) {
	if (context->source != NULL) {
		CFRunLoopSourceInvalidate(context->source);
		CFRelease(context->source);
	}
	if (context->tap != NULL) {
		CFMachPortInvalidate(context->tap);
		CFRelease(context->tap);
	}
	if (context->runLoop != NULL) {
		CFRelease(context->runLoop);
	}
	pthread_cond_destroy(&context->readyCondition);
	pthread_mutex_destroy(&context->mutex);
	free(context);
}

static int spiceCommandPasteStart(void) {
	pthread_mutex_lock(&spiceCommandPasteGlobalMutex);
	if (spiceCommandPasteContext != NULL) {
		pthread_mutex_unlock(&spiceCommandPasteGlobalMutex);
		return SpiceCommandPasteStartAlreadyRunning;
	}

	SpiceCommandPasteContext *context = calloc(1, sizeof(*context));
	if (context == NULL) {
		pthread_mutex_unlock(&spiceCommandPasteGlobalMutex);
		return SpiceCommandPasteStartThreadFailed;
	}
	pthread_mutex_init(&context->mutex, NULL);
	pthread_cond_init(&context->readyCondition, NULL);
	spiceCommandPasteContext = context;
	pthread_mutex_unlock(&spiceCommandPasteGlobalMutex);

	context->tap = CGEventTapCreate(
		kCGSessionEventTap,
		kCGHeadInsertEventTap,
		kCGEventTapOptionListenOnly,
		CGEventMaskBit(kCGEventKeyDown),
		spiceCommandPasteCallback,
		NULL
	);
	if (context->tap == NULL) {
		pthread_mutex_lock(&spiceCommandPasteGlobalMutex);
		spiceCommandPasteContext = NULL;
		pthread_mutex_unlock(&spiceCommandPasteGlobalMutex);
		spiceCommandPasteDestroy(context);
		return SpiceCommandPasteStartTapFailed;
	}

	context->source = CFMachPortCreateRunLoopSource(NULL, context->tap, 0);
	if (context->source == NULL) {
		pthread_mutex_lock(&spiceCommandPasteGlobalMutex);
		spiceCommandPasteContext = NULL;
		pthread_mutex_unlock(&spiceCommandPasteGlobalMutex);
		spiceCommandPasteDestroy(context);
		return SpiceCommandPasteStartSourceFailed;
	}

	pthread_mutex_lock(&context->mutex);
	int threadResult = pthread_create(
		&context->thread,
		NULL,
		spiceCommandPasteRunLoop,
		context
	);
	if (threadResult != 0) {
		pthread_mutex_unlock(&context->mutex);
		pthread_mutex_lock(&spiceCommandPasteGlobalMutex);
		spiceCommandPasteContext = NULL;
		pthread_mutex_unlock(&spiceCommandPasteGlobalMutex);
		spiceCommandPasteDestroy(context);
		return SpiceCommandPasteStartThreadFailed;
	}
	while (!context->ready) {
		pthread_cond_wait(&context->readyCondition, &context->mutex);
	}
	pthread_mutex_unlock(&context->mutex);

	return SpiceCommandPasteStartOK;
}

static void spiceCommandPasteStop(void) {
	pthread_mutex_lock(&spiceCommandPasteGlobalMutex);
	SpiceCommandPasteContext *context = spiceCommandPasteContext;
	spiceCommandPasteContext = NULL;
	pthread_mutex_unlock(&spiceCommandPasteGlobalMutex);
	if (context == NULL) {
		return;
	}

	pthread_mutex_lock(&context->mutex);
	context->stopRequested = true;
	CFRunLoopRef runLoop = context->runLoop;
	if (runLoop != NULL) {
		CFRunLoopStop(runLoop);
		CFRunLoopWakeUp(runLoop);
	}
	pthread_mutex_unlock(&context->mutex);

	pthread_join(context->thread, NULL);
	CGEventTapEnable(context->tap, false);
	spiceCommandPasteDestroy(context);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	activeMonitor         atomic.Pointer[timestampMonitor]
	nativeMonitorMu       sync.Mutex
	requestPermissionOnce sync.Once
)

// Start begins listening for physical Command+V key-down events on macOS.
func Start() (Monitor, error) {
	nativeMonitorMu.Lock()
	defer nativeMonitorMu.Unlock()

	if activeMonitor.Load() != nil {
		return noopMonitor{}, fmt.Errorf("start Command+V listener: already running")
	}

	if C.spiceCommandPasteHasPermission() == 0 {
		requestPermissionOnce.Do(func() {
			C.spiceCommandPasteRequestPermission()
		})
		if C.spiceCommandPasteHasPermission() == 0 {
			return noopMonitor{}, ErrPermissionDenied
		}
	}

	var monitor *timestampMonitor
	monitor = newMonitor(func() {
		nativeMonitorMu.Lock()
		activeMonitor.CompareAndSwap(monitor, nil)
		C.spiceCommandPasteStop()
		nativeMonitorMu.Unlock()
	})
	activeMonitor.Store(monitor)

	startResult := C.spiceCommandPasteStart()
	if startResult != C.SpiceCommandPasteStartOK {
		activeMonitor.CompareAndSwap(monitor, nil)
		return noopMonitor{}, fmt.Errorf("start Command+V listener: %w", nativeStartError(startResult))
	}

	return monitor, nil
}

// nativeStartError translates the C bridge's startup result into an error.
func nativeStartError(result C.int) error {
	switch result {
	case C.SpiceCommandPasteStartTapFailed:
		return errors.New("create event tap")
	case C.SpiceCommandPasteStartSourceFailed:
		return errors.New("create event tap run-loop source")
	case C.SpiceCommandPasteStartThreadFailed:
		return errors.New("create event tap thread")
	case C.SpiceCommandPasteStartAlreadyRunning:
		return errors.New("event tap already running")
	default:
		return fmt.Errorf("unknown native startup result %d", int(result))
	}
}

//export spiceCommandPasteRecorded
func spiceCommandPasteRecorded() {
	monitor := activeMonitor.Load()
	if monitor != nil {
		monitor.record(time.Now())
	}
}
