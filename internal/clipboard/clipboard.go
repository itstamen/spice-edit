// =============================================================================
// File: internal/clipboard/clipboard.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package clipboard pushes text onto the host system clipboard via the
// OSC 52 terminal escape sequence. OSC 52 is the right primitive for an
// SSH-first editor: it travels over the SSH channel, tmux forwards it
// (with set-clipboard on, the default since tmux 3.2), and every modern
// terminal we care about — iTerm2, WezTerm, Kitty, Alacritty, Ghostty,
// gnome-terminal, the macOS default — honors it.
//
// ReadSystem supports direct system-clipboard reads only on macOS, where
// pbpaste is available. Other platforms return an unsupported-platform error.
package clipboard

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// runCommand executes a command and returns its standard output. It is a
// package variable so clipboard command behavior can be tested without
// depending on a host clipboard utility.
var runCommand = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// CopyToSystem pushes text onto the host system clipboard via OSC 52.
//
// We open /dev/tty directly rather than writing to stdout so we don't
// race tcell's screen rendering. When TMUX is set we wrap the OSC 52
// sequence in tmux's escape passthrough so it reaches the outer terminal
// even if tmux is configured with set-clipboard off.
func CopyToSystem(text string) error {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	seq := fmt.Sprintf("\x1b]52;c;%s\x07", encoded)
	if os.Getenv("TMUX") != "" {
		// tmux passthrough: wrap inner escape sequences so tmux forwards
		// them verbatim to the host terminal.
		seq = fmt.Sprintf("\x1bPtmux;\x1b%s\x1b\\", seq)
	}
	_, err = f.WriteString(seq)
	return err
}

// ReadSystem reads the host system clipboard on macOS via pbpaste.
func ReadSystem() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("system clipboard read unsupported on %s", runtime.GOOS)
	}

	out, err := runCommand("pbpaste")
	if err != nil {
		return "", fmt.Errorf("read system clipboard via pbpaste: %w", err)
	}
	return string(out), nil
}
