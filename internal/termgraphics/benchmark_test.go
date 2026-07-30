// =============================================================================
// File: internal/termgraphics/benchmark_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package termgraphics

import (
	"image"
	"io"
	"testing"
)

// BenchmarkKittyPresenterPrepare1920x1080 measures full direct-KGP preparation
// and presentation for a full HD source image.
func BenchmarkKittyPresenterPrepare1920x1080(b *testing.B) {
	benchmarkKittyPresenterPrepare(b, 1920, 1080)
}

// BenchmarkKittyPresenterPrepare3840x2160 measures full direct-KGP preparation
// and presentation for a 4K source image.
func BenchmarkKittyPresenterPrepare3840x2160(b *testing.B) {
	benchmarkKittyPresenterPrepare(b, 3840, 2160)
}

// benchmarkKittyPresenterPrepare creates a presenter per iteration so the
// measurement includes target-pixel scaling, PNG encoding, and protocol output.
func benchmarkKittyPresenterPrepare(b *testing.B, width, height int) {
	b.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	req := Request{
		Image: Image{ID: 1, Revision: 1, Source: img},
		Rect:  CellRect{Cols: 160, Rows: 48},
		Cells: CellPixels{Width: 8, Height: 16},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		presenter := NewKittyPresenter(io.Discard)
		if err := presenter.Present(req); err != nil {
			b.Fatal(err)
		}
	}
}
