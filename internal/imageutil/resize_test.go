// =============================================================================
// File: internal/imageutil/resize_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for nearest-neighbour scaling shared by editor fallback rendering and
// terminal graphics preparation.

package imageutil

import (
	"image"
	"image/color"
	"testing"
)

// TestResizeNearestSamplesTopLeftOfEachSourceCell verifies that upsampling
// repeats the source pixel selected from each nearest-neighbour source cell.
func TestResizeNearestSamplesTopLeftOfEachSourceCell(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, color.RGBA{R: 1, A: 255})
	src.SetRGBA(1, 0, color.RGBA{R: 2, A: 255})
	src.SetRGBA(0, 1, color.RGBA{R: 3, A: 255})
	src.SetRGBA(1, 1, color.RGBA{R: 4, A: 255})

	got := ResizeNearest(src, 4, 4)
	if got == nil {
		t.Fatal("ResizeNearest returned nil")
	}
	if got.RGBAAt(3, 3).R != 4 {
		t.Fatalf("bottom-right = %d, want 4", got.RGBAAt(3, 3).R)
	}
}

// TestResizeNearestReturnsNilForInvalidInput confirms callers can pass an
// absent image or zero-area target without handling a separate error.
func TestResizeNearestReturnsNilForInvalidInput(t *testing.T) {
	if got := ResizeNearest(nil, 1, 1); got != nil {
		t.Fatal("nil source should return nil")
	}
	if got := ResizeNearest(image.NewRGBA(image.Rect(0, 0, 1, 1)), 0, 1); got != nil {
		t.Fatal("zero width should return nil")
	}
	if got := ResizeNearest(image.NewRGBA(image.Rect(0, 0, 1, 1)), 1, -1); got != nil {
		t.Fatal("negative height should return nil")
	}
}
