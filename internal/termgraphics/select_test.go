// =============================================================================
// File: internal/termgraphics/select_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package termgraphics

import "testing"

// mapEnv is a test environment implementation with only the values a case
// needs to exercise terminal selection.
type mapEnv map[string]string

// Getenv returns a configured test environment value.
func (e mapEnv) Getenv(key string) string {
	return e[key]
}

// TestSupportsKittyAcceptsOnlyNativeEligibleTerminals verifies direct KGP
// selection stays on the documented environment allowlist.
func TestSupportsKittyAcceptsOnlyNativeEligibleTerminals(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  mapEnv
		want bool
	}{
		{name: "native kitty", env: mapEnv{"TERM": "xterm-kitty"}, want: true},
		{name: "wezterm", env: mapEnv{"TERM_PROGRAM": "WezTerm"}, want: true},
		{name: "ghostty", env: mapEnv{"TERM_PROGRAM": "ghostty"}, want: true},
		{name: "unknown terminal", env: mapEnv{"TERM": "xterm-256color"}, want: false},
		{name: "tmux", env: mapEnv{"TERM": "xterm-kitty", "TMUX": "/tmp/tmux"}, want: false},
		{name: "zellij", env: mapEnv{"TERM": "xterm-kitty", "ZELLIJ": "0"}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SupportsKitty(tc.env); got != tc.want {
				t.Fatalf("SupportsKitty() = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestSupportsKittyRejectsMultiplexers keeps KGP passthrough explicitly out
// of scope even when the underlying terminal identifies as Kitty.
func TestSupportsKittyRejectsMultiplexers(t *testing.T) {
	env := mapEnv{"TERM": "xterm-kitty", "TMUX": "/tmp/tmux"}
	if SupportsKitty(env) {
		t.Fatal("tmux must force fallback")
	}
}

// TestIsHerdrRequiresAllInjectedVariables verifies selection requires all of
// the local-session variables injected by Herdr.
func TestIsHerdrRequiresAllInjectedVariables(t *testing.T) {
	if IsHerdr(mapEnv{"HERDR_ENV": "1", "HERDR_SOCKET_PATH": "/tmp/herdr"}) {
		t.Fatal("missing pane ID must reject Herdr")
	}
}

// TestIsHerdrRequiresCompleteInjectedEnvironment checks each required value
// and accepts the local Herdr environment only once all are present.
func TestIsHerdrRequiresCompleteInjectedEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  mapEnv
		want bool
	}{
		{name: "wrong marker", env: mapEnv{"HERDR_ENV": "0", "HERDR_SOCKET_PATH": "/tmp/herdr", "HERDR_PANE_ID": "pane"}},
		{name: "missing socket", env: mapEnv{"HERDR_ENV": "1", "HERDR_PANE_ID": "pane"}},
		{name: "missing pane", env: mapEnv{"HERDR_ENV": "1", "HERDR_SOCKET_PATH": "/tmp/herdr"}},
		{name: "complete", env: mapEnv{"HERDR_ENV": "1", "HERDR_SOCKET_PATH": "/tmp/herdr", "HERDR_PANE_ID": "pane"}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsHerdr(tc.env); got != tc.want {
				t.Fatalf("IsHerdr() = %t, want %t", got, tc.want)
			}
		})
	}
}
