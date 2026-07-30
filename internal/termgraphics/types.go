// =============================================================================
// File: internal/termgraphics/types.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package termgraphics provides terminal-specific image presentation
// primitives shared by Kitty Graphics Protocol and Herdr adapters.
package termgraphics

import (
	"errors"
	"image"
)

// ErrPayloadTooLarge marks a prepared graphics image that must use the
// per-view half-block fallback instead of disabling its presenter.
var ErrPayloadTooLarge = errors.New("terminal graphics payload too large")

// CellRect identifies an image viewport in terminal-cell coordinates.
type CellRect struct {
	X    int
	Y    int
	Cols int
	Rows int
}

// CellPixels describes the physical dimensions of one terminal cell.
type CellPixels struct {
	Width  int
	Height int
}

// Image is an immutable image identity, revision, and decoded source for a
// terminal graphics request.
type Image struct {
	ID       uint64
	Revision uint64
	Source   image.Image
}

// Request supplies one source image and the viewport in which to present it.
type Request struct {
	Image Image
	Rect  CellRect
	Cells CellPixels
}

// Presenter presents graphics after tcell has flushed a frame and clears its
// active placement before the image is hidden.
type Presenter interface {
	Present(Request) error
	Clear() error
}

// Placement describes a fitted image in both terminal-cell and pixel units.
type Placement struct {
	ViewportCol int
	ViewportRow int
	GridCols    int
	GridRows    int
	PixelWidth  int
	PixelHeight int
}
