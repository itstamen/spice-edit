//go:build darwin || linux

// =============================================================================
// File: internal/app/defaultopen_unix_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"errors"
	"testing"
)

// TestUnixOpenCommand covers macOS, Linux's preferred command, and Linux's
// gio fallback without launching a real desktop application.
func TestUnixOpenCommand(t *testing.T) {
	path := "/tmp/规格 draft $?.pdf"

	darwin, err := unixOpenCommand("darwin", path, func(string) (string, error) {
		return "", errors.New("unexpected lookup")
	})
	if err != nil || darwin.name != "open" || len(darwin.args) != 1 || darwin.args[0] != path {
		t.Fatalf("darwin command = %#v, err=%v", darwin, err)
	}

	linux, err := unixOpenCommand("linux", path, func(name string) (string, error) {
		if name == "xdg-open" {
			return "/usr/bin/xdg-open", nil
		}
		return "", errors.New("unexpected lookup")
	})
	if err != nil || linux.name != "/usr/bin/xdg-open" || len(linux.args) != 1 || linux.args[0] != path {
		t.Fatalf("linux command = %#v, err=%v", linux, err)
	}

	fallback, err := unixOpenCommand("linux", path, func(name string) (string, error) {
		switch name {
		case "xdg-open":
			return "", errors.New("missing")
		case "gio":
			return "/usr/bin/gio", nil
		default:
			return "", errors.New("unexpected lookup")
		}
	})
	if err != nil || fallback.name != "/usr/bin/gio" || len(fallback.args) != 2 || fallback.args[0] != "open" || fallback.args[1] != path {
		t.Fatalf("fallback command = %#v, err=%v", fallback, err)
	}
}
