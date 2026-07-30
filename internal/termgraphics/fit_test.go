// =============================================================================
// File: internal/termgraphics/fit_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package termgraphics

import (
	"image"
	"testing"
)

// TestFitCentresWideImageInsidePhysicalCellRectangle verifies a wide image
// fills the physical width, preserves its aspect ratio, and centres by cells.
func TestFitCentresWideImageInsidePhysicalCellRectangle(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 400, 100))

	got, ok := Fit(img, CellRect{X: 10, Y: 4, Cols: 40, Rows: 20}, CellPixels{Width: 8, Height: 16})
	if !ok {
		t.Fatal("Fit returned false")
	}
	if got.GridCols != 40 || got.GridRows != 5 {
		t.Fatalf("grid = %dx%d, want 40x5", got.GridCols, got.GridRows)
	}
	if got.ViewportCol != 10 || got.ViewportRow != 11 {
		t.Fatalf("origin = %d,%d, want 10,11", got.ViewportCol, got.ViewportRow)
	}
	if got.PixelWidth != 320 || got.PixelHeight != 80 {
		t.Fatalf("pixels = %dx%d, want 320x80", got.PixelWidth, got.PixelHeight)
	}
}

// TestFitCentresTallImageInsidePhysicalCellRectangle verifies a tall image
// fills the physical height and centres horizontally in whole cells.
func TestFitCentresTallImageInsidePhysicalCellRectangle(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 400))

	got, ok := Fit(img, CellRect{X: 10, Y: 4, Cols: 40, Rows: 20}, CellPixels{Width: 8, Height: 16})
	if !ok {
		t.Fatal("Fit returned false")
	}
	if got.GridCols != 10 || got.GridRows != 20 {
		t.Fatalf("grid = %dx%d, want 10x20", got.GridCols, got.GridRows)
	}
	if got.ViewportCol != 25 || got.ViewportRow != 4 {
		t.Fatalf("origin = %d,%d, want 25,4", got.ViewportCol, got.ViewportRow)
	}
	if got.PixelWidth != 80 || got.PixelHeight != 320 {
		t.Fatalf("pixels = %dx%d, want 80x320", got.PixelWidth, got.PixelHeight)
	}
}

// TestFitRejectsInvalidGeometry keeps callers from preparing a zero-area
// image for a terminal graphics presenter.
func TestFitRejectsInvalidGeometry(t *testing.T) {
	validImage := image.NewRGBA(image.Rect(0, 0, 1, 1))
	emptyImage := image.NewRGBA(image.Rect(0, 0, 0, 1))
	validRect := CellRect{Cols: 1, Rows: 1}
	validCells := CellPixels{Width: 1, Height: 1}

	for _, tc := range []struct {
		name  string
		image image.Image
		rect  CellRect
		cells CellPixels
	}{
		{name: "nil image", rect: validRect, cells: validCells},
		{name: "empty image", image: emptyImage, rect: validRect, cells: validCells},
		{name: "zero columns", image: validImage, rect: CellRect{Rows: 1}, cells: validCells},
		{name: "zero rows", image: validImage, rect: CellRect{Cols: 1}, cells: validCells},
		{name: "zero cell width", image: validImage, rect: validRect, cells: CellPixels{Height: 1}},
		{name: "zero cell height", image: validImage, rect: validRect, cells: CellPixels{Width: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := Fit(tc.image, tc.rect, tc.cells); ok || got != (Placement{}) {
				t.Fatalf("Fit() = (%+v, %t), want zero placement and false", got, ok)
			}
		})
	}
}
