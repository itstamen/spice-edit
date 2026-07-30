// =============================================================================
// File: internal/imageutil/resize.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package imageutil provides small shared image helpers.
package imageutil

import "image"

// ResizeNearest produces a w×h RGBA copy of src using nearest-neighbour
// sampling. It returns nil for an absent source or non-positive target size.
func ResizeNearest(src image.Image, w, h int) *image.RGBA {
	if src == nil || w <= 0 || h <= 0 {
		return nil
	}

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	bounds := src.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return dst
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, y, src.At(
				bounds.Min.X+x*bounds.Dx()/w,
				bounds.Min.Y+y*bounds.Dy()/h,
			))
		}
	}

	return dst
}
