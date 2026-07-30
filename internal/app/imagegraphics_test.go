// =============================================================================
// File: internal/app/imagegraphics_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/cloudmanic/spice-edit/internal/termgraphics"
)

// fakePresenter records frame lifecycle calls without writing terminal escape
// sequences, letting app tests verify presentation ordering deterministically.
type fakePresenter struct {
	events     []string
	requests   []termgraphics.Request
	presentErr error
	clearErr   error
}

// Present records a graphics request and returns the configured test error.
func (p *fakePresenter) Present(request termgraphics.Request) error {
	p.events = append(p.events, "present")
	p.requests = append(p.requests, request)
	return p.presentErr
}

// Clear records placement cleanup and returns the configured test error.
func (p *fakePresenter) Clear() error {
	p.events = append(p.events, "clear")
	return p.clearErr
}

// writeImageFixture creates the small PNG used by app graphics lifecycle
// tests, returning its path for App.openFile.
func writeImageFixture(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// imageTestApp opens one image tab in a simulation-backed App.
func imageTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	a := newTestApp(t, dir)
	a.openFile(writeImageFixture(t, dir, "image.png"))
	return a
}

// screenContainsHalfBlock reports whether the simulation's front buffer
// includes the fallback image glyph.
func screenContainsHalfBlock(screen tcell.Screen) bool {
	simulation := screen.(tcell.SimulationScreen)
	simulation.Show()
	cells, _, _ := simulation.GetContents()
	for _, cell := range cells {
		if len(cell.Runes) > 0 && cell.Runes[0] == '▀' {
			return true
		}
	}
	return false
}

// TestFlushFrameShowsBeforeGraphicsPresent pins down the tcell-first frame
// boundary that prevents graphics output from racing the cell buffer.
func TestFlushFrameShowsBeforeGraphicsPresent(t *testing.T) {
	a := imageTestApp(t)
	presenter := &fakePresenter{}
	a.graphics = presenter
	a.showFrame = func() { presenter.events = append(presenter.events, "show") }

	a.flushFrame()

	if got, want := strings.Join(presenter.events, ","), "show,present"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
}

// TestInitializeImageGraphicsPrefersHerdr verifies a complete Herdr pane
// environment wins before direct KGP selection, even when a Kitty identity is
// also present.
func TestInitializeImageGraphicsPrefersHerdr(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/herdr.sock")
	t.Setenv("HERDR_PANE_ID", "window:pane")
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TMUX", "")
	t.Setenv("ZELLIJ", "")

	a := newTestApp(t, t.TempDir())
	a.initializeImageGraphics()

	if a.graphics == nil {
		t.Fatal("Herdr environment did not select a presenter")
	}
	if a.graphicsUsesTTY || a.graphicsTTY != nil {
		t.Fatal("Herdr selection unexpectedly used direct KGP TTY state")
	}
}

// TestInitializeImageGraphicsRequiresTTYForKitty ensures a simulated or
// otherwise non-terminal tcell screen never receives direct KGP writes.
func TestInitializeImageGraphicsRequiresTTYForKitty(t *testing.T) {
	t.Setenv("HERDR_ENV", "")
	t.Setenv("HERDR_SOCKET_PATH", "")
	t.Setenv("HERDR_PANE_ID", "")
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TMUX", "")
	t.Setenv("ZELLIJ", "")

	a := newTestApp(t, t.TempDir())
	a.initializeImageGraphics()

	if a.graphics != nil {
		t.Fatal("simulation screen selected direct KGP without a tcell TTY")
	}
}

// TestFlushFramePresentationFailureRedrawsHalfBlocks verifies session errors
// immediately replace the blank graphics canvas with the universal fallback.
func TestFlushFramePresentationFailureRedrawsHalfBlocks(t *testing.T) {
	a := imageTestApp(t)
	presenter := &fakePresenter{presentErr: errors.New("write failed")}
	a.graphics = presenter
	a.showFrame = func() {}

	a.flushFrame()

	if !a.graphicsDisabled {
		t.Fatal("presentation failure did not disable graphics")
	}
	if !screenContainsHalfBlock(a.screen) {
		t.Fatal("failed presentation did not redraw half-block fallback")
	}
	if got, want := a.statusMsg, "Image graphics unavailable — using block preview."; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

// TestPayloadFallbackOnlyDisablesTheCurrentView keeps the presenter usable
// while marking just one oversize target for half-block rendering.
func TestPayloadFallbackOnlyDisablesTheCurrentView(t *testing.T) {
	a := imageTestApp(t)
	presenter := &fakePresenter{presentErr: termgraphics.ErrPayloadTooLarge}
	a.graphics = presenter
	a.showFrame = func() {}

	a.flushFrame()
	a.flushFrame()

	if a.graphicsDisabled {
		t.Fatal("payload fallback disabled the graphics session")
	}
	if got, want := strings.Join(presenter.events, ","), "present"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
	if !screenContainsHalfBlock(a.screen) {
		t.Fatal("oversize image did not use half-block fallback")
	}
	if got, want := a.statusMsg, "Image too large for graphics — using block preview."; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

// TestGraphicsClearOnTabSwitchAndClose clears an old placement before the
// next visible image is presented and when the active image disappears.
func TestGraphicsClearOnTabSwitchAndClose(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	first := writeImageFixture(t, dir, "first.png")
	second := writeImageFixture(t, dir, "second.png")
	a.openFile(first)
	presenter := &fakePresenter{}
	a.graphics = presenter
	a.showFrame = func() {}
	a.flushFrame()

	a.openFile(second)
	a.flushFrame()
	if got, want := strings.Join(presenter.events, ","), "present,clear,present"; got != want {
		t.Fatalf("switch events = %q, want %q", got, want)
	}

	a.closeTab(a.activeTab)
	a.flushFrame()
	if got, want := strings.Join(presenter.events, ","), "present,clear,present,clear,present"; got != want {
		t.Fatalf("close-and-restore events = %q, want %q", got, want)
	}

	a.closeTab(a.activeTab)
	a.flushFrame()
	if got, want := strings.Join(presenter.events, ","), "present,clear,present,clear,present,clear"; got != want {
		t.Fatalf("last-close events = %q, want %q", got, want)
	}
}

// TestGraphicsClearForOverlayAndRestore verifies overlays clear the terminal
// layer and restore it once the editor image becomes visible again.
func TestGraphicsClearForOverlayAndRestore(t *testing.T) {
	a := imageTestApp(t)
	presenter := &fakePresenter{}
	a.graphics = presenter
	a.showFrame = func() {}
	a.flushFrame()

	a.openMenu()
	a.flushFrame()
	a.closeMenu()
	a.flushFrame()

	if got, want := strings.Join(presenter.events, ","), "present,clear,present"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
}

// TestGraphicsClearAndRetransmitAfterResize verifies a geometry change clears
// the previous placement before presenting the resized image view.
func TestGraphicsClearAndRetransmitAfterResize(t *testing.T) {
	a := imageTestApp(t)
	presenter := &fakePresenter{}
	a.graphics = presenter
	a.showFrame = func() {}
	a.flushFrame()

	simulation := a.screen.(tcell.SimulationScreen)
	simulation.SetSize(120, 41)
	a.handleEvent(tcell.NewEventResize(120, 41))
	a.flushFrame()

	if got, want := strings.Join(presenter.events, ","), "present,clear,present"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
}

// TestGraphicsClearWhenResizeKeepsPlacementPixels verifies a terminal resize
// still clears before presenting when cell quantization leaves the fitted
// placement and payload fallback pixels unchanged.
func TestGraphicsClearWhenResizeKeepsPlacementPixels(t *testing.T) {
	a := imageTestApp(t)
	presenter := &fakePresenter{}
	a.graphics = presenter
	a.showFrame = func() {}
	a.flushFrame()

	simulation := a.screen.(tcell.SimulationScreen)
	simulation.SetSize(121, 40)
	a.handleEvent(tcell.NewEventResize(121, 40))
	a.flushFrame()

	if got, want := strings.Join(presenter.events, ","), "present,clear,present"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
	first, firstOK := termgraphics.Fit(
		presenter.requests[0].Image.Source,
		presenter.requests[0].Rect,
		presenter.requests[0].Cells,
	)
	second, secondOK := termgraphics.Fit(
		presenter.requests[1].Image.Source,
		presenter.requests[1].Rect,
		presenter.requests[1].Cells,
	)
	if !firstOK || !secondOK {
		t.Fatal("fixture request did not fit")
	}
	if first.PixelWidth != second.PixelWidth || first.PixelHeight != second.PixelHeight {
		t.Fatalf("pixel size changed from %dx%d to %dx%d", first.PixelWidth, first.PixelHeight, second.PixelWidth, second.PixelHeight)
	}
	if first != second {
		t.Fatalf("quantized placement changed from %+v to %+v", first, second)
	}
	if presenter.requests[0].Rect == presenter.requests[1].Rect {
		t.Fatal("resize did not change the request rectangle")
	}
}

// TestGraphicsReloadRetransmitsNewRevision verifies a successful image reload
// creates a fresh graphics view even when the image dimensions are unchanged.
func TestGraphicsReloadRetransmitsNewRevision(t *testing.T) {
	a := imageTestApp(t)
	presenter := &fakePresenter{}
	a.graphics = presenter
	a.showFrame = func() {}
	a.flushFrame()

	if err := a.activeTabPtr().Reload(); err != nil {
		t.Fatal(err)
	}
	a.flushFrame()

	if got, want := strings.Join(presenter.events, ","), "present,clear,present"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
	if got, want := presenter.requests[1].Image.Revision, presenter.requests[0].Image.Revision+1; got != want {
		t.Fatalf("revision after reload = %d, want %d", got, want)
	}
}

// TestClearFailureDisablesFurtherPresenterCalls verifies an uncertain cleanup
// state cannot lead to more terminal graphics writes later in the session.
func TestClearFailureDisablesFurtherPresenterCalls(t *testing.T) {
	a := imageTestApp(t)
	presenter := &fakePresenter{clearErr: errors.New("clear failed")}
	a.graphics = presenter
	a.showFrame = func() {}
	a.flushFrame()

	a.openMenu()
	a.flushFrame()
	if !a.graphicsDisabled {
		t.Fatal("clear failure did not disable graphics")
	}
	if got, want := strings.Join(presenter.events, ","), "present,clear"; got != want {
		t.Fatalf("events after failed clear = %q, want %q", got, want)
	}

	a.closeMenu()
	a.flushFrame()
	if got, want := strings.Join(presenter.events, ","), "present,clear"; got != want {
		t.Fatalf("disabled presenter received another call: %q", got)
	}
}

// TestCloseClearsActiveGraphicsBeforeFinalizingScreen verifies orderly app
// shutdown sends terminal placement cleanup while the tcell screen is live.
func TestCloseClearsActiveGraphicsBeforeFinalizingScreen(t *testing.T) {
	a := imageTestApp(t)
	presenter := &fakePresenter{}
	a.graphics = presenter
	a.showFrame = func() {}
	a.flushFrame()

	a.Close()

	if got, want := strings.Join(presenter.events, ","), "present,clear"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
}

// TestFeatureDisabledUsesHerdrConfigurationHint verifies the only special
// session error gives the exact non-invasive Herdr recovery instruction.
func TestFeatureDisabledUsesHerdrConfigurationHint(t *testing.T) {
	a := imageTestApp(t)
	a.graphics = &fakePresenter{presentErr: termgraphics.ErrFeatureDisabled}
	a.showFrame = func() {}

	a.flushFrame()

	const want = "Herdr graphics disabled — add [experimental] kitty_graphics = true, run herdr server reload-config, then restart Herdr if needed."
	if a.statusMsg != want {
		t.Fatalf("status = %q, want %q", a.statusMsg, want)
	}
}
