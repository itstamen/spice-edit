// =============================================================================
// File: internal/editor/wrap.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-01
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

// wrapSegment is one visual row produced from a logical buffer line. Rune
// ranges are half-open and remain indexed into the original line so rendering,
// selection, and find highlighting can keep using the editor's logical model.
type wrapSegment struct {
	startRune   int
	endRune     int
	startVisual int
	width       int
}

// wrapLine splits runes into grapheme-safe visual rows no wider than contentW
// when possible. A single grapheme or tab wider than contentW is kept whole in
// its own segment so rendering never slices a cluster across terminal rows.
func wrapLine(runes []rune, contentW int) []wrapSegment {
	if contentW < 1 {
		contentW = 1
	}
	if len(runes) == 0 {
		return []wrapSegment{{}}
	}

	var segments []wrapSegment
	startRune, startVisual, segWidth, visualCol := 0, 0, 0, 0
	for _, cluster := range lineClusters(runes) {
		width := clusterVisualWidth(cluster, visualCol)
		if segWidth > 0 && width > 0 && segWidth+width > contentW {
			segments = append(segments, wrapSegment{
				startRune:   startRune,
				endRune:     cluster.StartRune,
				startVisual: startVisual,
				width:       segWidth,
			})
			startRune = cluster.StartRune
			startVisual = visualCol
			segWidth = 0
		}
		segWidth += width
		visualCol += width
	}
	segments = append(segments, wrapSegment{
		startRune:   startRune,
		endRune:     len(runes),
		startVisual: startVisual,
		width:       segWidth,
	})
	return segments
}
