// =============================================================================
// File: internal/termgraphics/kitty_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package termgraphics

import (
	"bytes"
	"errors"
	"image"
	"io"
	"strings"
	"testing"
)

// TestKittyPresentWritesQuietChunkedTransferThenPlacement verifies direct KGP
// uploads use quiet PNG chunks and a one-based cursor placement.
func TestKittyPresentWritesQuietChunkedTransferThenPlacement(t *testing.T) {
	var out bytes.Buffer
	p := NewKittyPresenter(&out)

	err := p.Present(testRequest(1, 1, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	if err != nil {
		t.Fatal(err)
	}

	got := out.String()
	if !strings.Contains(got, "\x1b_Ga=t,f=100,i=1,q=2,m=0;") {
		t.Fatalf("missing quiet PNG transfer: %q", got)
	}
	if !strings.Contains(got, "\x1b[5;7H\x1b_Ga=p,i=1,p=1,c=4,r=2,C=1,q=2;\x1b\\") {
		t.Fatalf("missing cursor-positioned placement: %q", got)
	}
}

// TestKittyPresentChunkCommandsStayWithinBase64Limit verifies all non-final
// KGP transfer payload chunks are complete base64 quartets no larger than the
// protocol's payload limit.
func TestKittyPresentChunkCommandsStayWithinBase64Limit(t *testing.T) {
	chunks := chunkBase64(bytes.Repeat([]byte{'A'}, 8196))
	for index, chunk := range chunks[:len(chunks)-1] {
		if len(chunk) > kittyChunkSize || len(chunk)%4 != 0 {
			t.Fatalf("chunk %d has invalid size %d", index, len(chunk))
		}
	}
}

// TestKittyPresenterClearKeepsUploadedImageData verifies cleanup deletes the
// active placement without discarding the cached prepared PNG.
func TestKittyPresenterClearKeepsUploadedImageData(t *testing.T) {
	var out bytes.Buffer
	p := NewKittyPresenter(&out)
	p.hasPrepared = true
	p.preparedPNG = []byte("cached")
	p.hasUploaded = true
	p.hasPlacement = true
	p.lastPlacement = Placement{ViewportCol: 6, ViewportRow: 4, GridCols: 4, GridRows: 2, PixelWidth: 32, PixelHeight: 32}

	if err := p.Clear(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "\x1b_Ga=d,d=i,i=1,p=1,q=2;\x1b\\" {
		t.Fatalf("clear = %q", got)
	}
	if !p.hasPrepared || string(p.preparedPNG) != "cached" {
		t.Fatal("clear discarded cached uploaded image data")
	}
	if !p.hasUploaded {
		t.Fatal("clear discarded uploaded image state")
	}
	if p.hasPlacement {
		t.Fatal("clear retained visible placement state")
	}
	if p.lastPlacement != (Placement{}) {
		t.Fatalf("clear retained placement geometry: %+v", p.lastPlacement)
	}
}

// TestKittyPresenterSameActiveViewIsNoop verifies a visible, already-uploaded
// view emits no transfer or placement on a repeat frame.
func TestKittyPresenterSameActiveViewIsNoop(t *testing.T) {
	var out bytes.Buffer
	p := NewKittyPresenter(&out)
	calls := 0
	p.encodePNG = func(image.Image) ([]byte, error) {
		calls++
		return []byte("png"), nil
	}
	req := testRequest(1, 1, onePixel())

	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("encode calls = %d, want 1", calls)
	}
	if got := out.String(); got != "" {
		t.Fatalf("same active view wrote %q", got)
	}
}

// TestKittyPresenterClearThenPresentReusesUpload verifies a clear removes only
// the placement, so presenting the same view restores it without uploading.
func TestKittyPresenterClearThenPresentReusesUpload(t *testing.T) {
	var out bytes.Buffer
	p := NewKittyPresenter(&out)
	calls := 0
	p.encodePNG = func(image.Image) ([]byte, error) {
		calls++
		return []byte("png"), nil
	}
	req := testRequest(1, 1, onePixel())

	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Clear(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("encode calls = %d, want 1", calls)
	}
	if got := out.String(); strings.Contains(got, "a=t,") {
		t.Fatalf("same uploaded view retransferred PNG: %q", got)
	}
	if got := out.String(); !strings.Contains(got, "a=p,i=1,p=1,") {
		t.Fatalf("same uploaded view did not restore placement: %q", got)
	}
}

// TestKittyPresenterShiftedRectReusesUploadAndUpdatesPlacement verifies a
// same-sized image moved within the terminal emits only a new placement and
// records its new geometry.
func TestKittyPresenterShiftedRectReusesUploadAndUpdatesPlacement(t *testing.T) {
	var out bytes.Buffer
	p := NewKittyPresenter(&out)
	req := testRequest(1, 1, onePixel())
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	req.Rect.X += 2
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}

	const want = "\x1b[5;9H\x1b_Ga=p,i=1,p=1,c=4,r=2,C=1,q=2;\x1b\\"
	if got := out.String(); got != want {
		t.Fatalf("shifted placement = %q, want %q", got, want)
	}
	if strings.Contains(out.String(), "a=t,") {
		t.Fatalf("shifted same-sized view retransferred PNG: %q", out.String())
	}
	wantPlacement := Placement{ViewportCol: 8, ViewportRow: 4, GridCols: 4, GridRows: 2, PixelWidth: 32, PixelHeight: 32}
	if p.lastPlacement != wantPlacement {
		t.Fatalf("stored placement = %+v, want %+v", p.lastPlacement, wantPlacement)
	}
}

// TestKittyPresenterChangedViewTransfersOnceAndReplaces verifies a new view
// key uploads once and creates a fresh placement with the stable KGP IDs.
func TestKittyPresenterChangedViewTransfersOnceAndReplaces(t *testing.T) {
	var out bytes.Buffer
	p := NewKittyPresenter(&out)
	calls := 0
	p.encodePNG = func(image.Image) ([]byte, error) {
		calls++
		return []byte("png"), nil
	}
	req := testRequest(1, 1, onePixel())

	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	req.Image.Revision++
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("encode calls = %d, want 2", calls)
	}
	if count := strings.Count(out.String(), "a=t,"); count != 1 {
		t.Fatalf("new view transfers = %d, want 1", count)
	}
	if count := strings.Count(out.String(), "a=p,i=1,p=1,"); count != 1 {
		t.Fatalf("new view placements = %d, want 1", count)
	}
	if !p.hasUploaded || !p.hasPlacement || p.uploadedKey.revision != req.Image.Revision {
		t.Fatalf("state after replacement = uploaded:%t placement:%t key:%+v", p.hasUploaded, p.hasPlacement, p.uploadedKey)
	}
}

// TestKittyPresenterOversizeReturnsTypedFallback verifies a direct-KGP PNG
// over the 16 MiB limit requests only a per-view fallback.
func TestKittyPresenterOversizeReturnsTypedFallback(t *testing.T) {
	p := NewKittyPresenter(io.Discard)
	p.encodePNG = func(image.Image) ([]byte, error) {
		return make([]byte, directPayloadLimit+1), nil
	}

	err := p.Present(testRequest(1, 1, onePixel()))
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("error = %v, want ErrPayloadTooLarge", err)
	}
	if p.hasPlacement {
		t.Fatal("oversize payload left placement marked visible")
	}
	if p.hasPrepared {
		t.Fatal("oversize payload was retained in the preparation cache")
	}
}

// TestKittyPresenterTransferFailureClearsPriorPlacementState verifies an
// unsuccessful replacement transfer cannot leave an old placement marked
// visible after reusing the stable KGP image ID.
func TestKittyPresenterTransferFailureClearsPriorPlacementState(t *testing.T) {
	p := NewKittyPresenter(io.Discard)
	req := testRequest(1, 1, onePixel())
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}

	p.writer = errWriter{err: errors.New("write failed")}
	req.Image.Revision++
	if err := p.Present(req); err == nil {
		t.Fatal("expected transfer error")
	}
	if p.hasPlacement || p.hasUploaded {
		t.Fatalf("transfer failure state = placement:%t uploaded:%t", p.hasPlacement, p.hasUploaded)
	}
}

// TestKittyPresenterPlacementFailureClearsPriorPlacementState verifies a
// successful replacement transfer followed by a failed placement leaves no
// placement state while retaining the known uploaded image.
func TestKittyPresenterPlacementFailureClearsPriorPlacementState(t *testing.T) {
	p := NewKittyPresenter(io.Discard)
	req := testRequest(1, 1, onePixel())
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}

	p.writer = &failAfterWriter{successfulWrites: 1, err: errors.New("placement write failed")}
	req.Image.Revision++
	if err := p.Present(req); err == nil {
		t.Fatal("expected placement error")
	}
	if p.hasPlacement {
		t.Fatal("failed placement left placement marked visible")
	}
	if !p.hasUploaded || p.uploadedKey.revision != req.Image.Revision {
		t.Fatalf("failed placement upload state = uploaded:%t key:%+v", p.hasUploaded, p.uploadedKey)
	}
}

// TestWriteAllRetriesPartialWrites verifies terminal writers that make
// progress with short successful writes still receive each complete command.
func TestWriteAllRetriesPartialWrites(t *testing.T) {
	writer := &partialWriter{maximum: 2}
	if err := writeAll(writer, []byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if got := writer.buffer.String(); got != "abcdef" {
		t.Fatalf("written = %q, want %q", got, "abcdef")
	}
}

// TestWriteAllRejectsZeroByteSuccess verifies a writer cannot silently drop a
// graphics command by reporting no error and no progress.
func TestWriteAllRejectsZeroByteSuccess(t *testing.T) {
	if err := writeAll(zeroWriter{}, []byte("command")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want io.ErrShortWrite", err)
	}
}

// testRequest supplies the shared valid geometry used by graphics presenter
// tests, including the later Herdr adapter tests in this package.
func testRequest(id, revision uint64, src image.Image) Request {
	return Request{
		Image: Image{ID: id, Revision: revision, Source: src},
		Rect:  CellRect{X: 6, Y: 4, Cols: 4, Rows: 2},
		Cells: CellPixels{Width: 8, Height: 16},
	}
}

// onePixel returns a valid minimal source image for presenter tests.
func onePixel() image.Image {
	return image.NewRGBA(image.Rect(0, 0, 1, 1))
}

// errWriter always returns its configured terminal write failure.
type errWriter struct {
	err error
}

// Write implements io.Writer for errWriter.
func (w errWriter) Write([]byte) (int, error) {
	return 0, w.err
}

// partialWriter writes at most maximum bytes per call while retaining each
// byte that it accepts.
type partialWriter struct {
	maximum int
	buffer  bytes.Buffer
}

// failAfterWriter accepts a fixed number of complete command writes before
// returning its configured terminal write failure.
type failAfterWriter struct {
	successfulWrites int
	writes           int
	err              error
}

// Write implements io.Writer for failAfterWriter.
func (w *failAfterWriter) Write(data []byte) (int, error) {
	if w.writes >= w.successfulWrites {
		return 0, w.err
	}
	w.writes++
	return len(data), nil
}

// Write implements io.Writer for partialWriter.
func (w *partialWriter) Write(data []byte) (int, error) {
	count := w.maximum
	if count > len(data) {
		count = len(data)
	}
	_, _ = w.buffer.Write(data[:count])
	return count, nil
}

// zeroWriter reports a successful write without accepting any bytes.
type zeroWriter struct{}

// Write implements io.Writer for zeroWriter.
func (zeroWriter) Write([]byte) (int, error) {
	return 0, nil
}
