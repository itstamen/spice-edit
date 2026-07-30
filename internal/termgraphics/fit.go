// =============================================================================
// File: internal/termgraphics/fit.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package termgraphics

import "image"

// Fit calculates the largest whole-cell placement that fits src inside rect's
// physical pixel bounds. It preserves the source aspect ratio until terminal
// cell quantization, rounds inward to stay inside the viewport, and centres
// the resulting grid in rect.
func Fit(src image.Image, rect CellRect, cells CellPixels) (Placement, bool) {
	if src == nil || rect.Cols <= 0 || rect.Rows <= 0 || cells.Width <= 0 || cells.Height <= 0 {
		return Placement{}, false
	}

	bounds := src.Bounds()
	sourceWidth := bounds.Dx()
	sourceHeight := bounds.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return Placement{}, false
	}

	maxPixelWidth := int64(rect.Cols) * int64(cells.Width)
	maxPixelHeight := int64(rect.Rows) * int64(cells.Height)

	gridCols, gridRows := fittedGrid(
		sourceWidth,
		sourceHeight,
		rect.Cols,
		rect.Rows,
		cells.Width,
		cells.Height,
		maxPixelWidth,
		maxPixelHeight,
	)

	return Placement{
		ViewportCol: rect.X + (rect.Cols-gridCols)/2,
		ViewportRow: rect.Y + (rect.Rows-gridRows)/2,
		GridCols:    gridCols,
		GridRows:    gridRows,
		PixelWidth:  gridCols * cells.Width,
		PixelHeight: gridRows * cells.Height,
	}, true
}

// fittedGrid converts a physical aspect fit into a whole-cell grid. The
// limiting dimension fills the viewport; the other rounds inward, except that
// a valid image retains a single cell for an otherwise sub-cell dimension.
func fittedGrid(sourceWidth, sourceHeight, maxCols, maxRows, cellWidth, cellHeight int, maxPixelWidth, maxPixelHeight int64) (int, int) {
	if int64(sourceWidth)*maxPixelHeight >= int64(sourceHeight)*maxPixelWidth {
		rows := int(maxPixelWidth * int64(sourceHeight) / (int64(sourceWidth) * int64(cellHeight)))
		return maxCols, clampGridDimension(rows, maxRows)
	}

	cols := int(maxPixelHeight * int64(sourceWidth) / (int64(sourceHeight) * int64(cellWidth)))
	return clampGridDimension(cols, maxCols), maxRows
}

// clampGridDimension keeps a quantized image dimension inside its viewport
// while preserving one visible cell for valid non-zero input geometry.
func clampGridDimension(value, maximum int) int {
	if value < 1 {
		return 1
	}
	if value > maximum {
		return maximum
	}
	return value
}
