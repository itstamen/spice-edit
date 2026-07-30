// =============================================================================
// File: internal/termgraphics/select.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package termgraphics

// Environment reads process environment values needed for presenter
// selection. Keeping it narrow makes selection deterministic in tests.
type Environment interface {
	Getenv(string) string
}

// IsHerdr reports whether the complete local Herdr environment was injected
// for the current pane.
func IsHerdr(env Environment) bool {
	return env != nil &&
		env.Getenv("HERDR_ENV") == "1" &&
		env.Getenv("HERDR_SOCKET_PATH") != "" &&
		env.Getenv("HERDR_PANE_ID") != ""
}

// SupportsKitty reports whether direct Kitty Graphics Protocol output is
// eligible in a native allowed terminal, without protocol probing.
func SupportsKitty(env Environment) bool {
	if env == nil || env.Getenv("TMUX") != "" || env.Getenv("ZELLIJ") != "" {
		return false
	}
	return env.Getenv("TERM") == "xterm-kitty" ||
		env.Getenv("TERM_PROGRAM") == "WezTerm" ||
		env.Getenv("TERM_PROGRAM") == "ghostty"
}
