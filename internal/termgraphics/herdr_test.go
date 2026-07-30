// =============================================================================
// File: internal/termgraphics/herdr_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package termgraphics

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHerdrPresentCallsInfoThenSetWithPaneRelativePlacement verifies the
// static adapter requests authoritative pane metrics before fitting and setting
// a scaled PNG at a pane-relative cell placement.
func TestHerdrPresentCallsInfoThenSetWithPaneRelativePlacement(t *testing.T) {
	server := newJSONSocketServer(t, []string{
		`{"id":"graphics-info","result":{"type":"pane_graphics_info","cell_width_px":8,"cell_height_px":16}}`,
		`{"id":"graphics-set","result":{"type":"ok"}}`,
	})
	p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
	req := testRequest(1, 1, image.NewRGBA(image.Rect(0, 0, 40, 20)))
	req.Cells = CellPixels{Width: 1, Height: 1}
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}

	requests := server.Requests()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if got, want := requests[0].Method, "pane.graphics.info"; got != want {
		t.Fatalf("info method = %q, want %q", got, want)
	}
	if got, want := requests[0].Params, map[string]any{"pane_id": "w1:p1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("info params = %#v, want %#v", got, want)
	}
	if got, want := requests[1].Method, "pane.graphics.set"; got != want {
		t.Fatalf("set method = %q, want %q", got, want)
	}

	set := requests[1].Params
	if got, want := mapKeys(set), []string{"data_base64", "format", "image_height", "image_width", "pane_id", "placement"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("set fields = %v, want %v", got, want)
	}
	if got, want := set["pane_id"], "w1:p1"; got != want {
		t.Fatalf("set pane_id = %#v, want %#v", got, want)
	}
	if got, want := set["format"], "png"; got != want {
		t.Fatalf("set format = %#v, want %#v", got, want)
	}
	if got, want := set["image_width"], float64(32); got != want {
		t.Fatalf("set image_width = %#v, want %#v", got, want)
	}
	if got, want := set["image_height"], float64(16); got != want {
		t.Fatalf("set image_height = %#v, want %#v", got, want)
	}
	placement, ok := set["placement"].(map[string]any)
	if !ok {
		t.Fatalf("placement = %#v, want object", set["placement"])
	}
	if got, want := placement, map[string]any{
		"viewport_col": float64(6),
		"viewport_row": float64(4),
		"grid_cols":    float64(4),
		"grid_rows":    float64(1),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("placement = %#v, want %#v", got, want)
	}

	encoded, ok := set["data_base64"].(string)
	if !ok {
		t.Fatalf("data_base64 = %#v, want string", set["data_base64"])
	}
	pngData, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("data_base64 is not standard base64: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatalf("data_base64 is not a PNG: %v", err)
	}
	if got, want := decoded.Bounds().Size(), (image.Point{X: 32, Y: 16}); got != want {
		t.Fatalf("encoded PNG size = %v, want %v", got, want)
	}
}

// TestHerdrPresentCachesInfoMetrics verifies a later presentation uses cached
// pane metrics rather than querying the graphics capability again.
func TestHerdrPresentCachesInfoMetrics(t *testing.T) {
	server := newJSONSocketServer(t, []string{
		`{"id":"graphics-info","result":{"type":"pane_graphics_info","cell_width_px":8,"cell_height_px":16}}`,
		`{"id":"graphics-set","result":{"type":"ok"}}`,
		`{"id":"graphics-set","result":{"type":"ok"}}`,
	})
	p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
	if err := p.Present(testRequest(1, 1, onePixel())); err != nil {
		t.Fatal(err)
	}
	if err := p.Present(testRequest(2, 1, onePixel())); err != nil {
		t.Fatal(err)
	}

	requests := server.Requests()
	if len(requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(requests))
	}
	if requests[0].Method != "pane.graphics.info" || requests[1].Method != "pane.graphics.set" || requests[2].Method != "pane.graphics.set" {
		t.Fatalf("methods = %#v", []string{requests[0].Method, requests[1].Method, requests[2].Method})
	}
}

// TestHerdrPresentSameViewAndPlacementIsNoop verifies repeated frame flushes
// for an unchanged image do not re-encode or open another Herdr connection.
func TestHerdrPresentSameViewAndPlacementIsNoop(t *testing.T) {
	server := newJSONSocketServer(t, []string{
		`{"id":"graphics-info","result":{"type":"pane_graphics_info","cell_width_px":8,"cell_height_px":16}}`,
		`{"id":"graphics-set","result":{"type":"ok"}}`,
	})
	p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
	encodes := 0
	p.encodePNG = func(image.Image) ([]byte, error) {
		encodes++
		return []byte("png"), nil
	}
	req := testRequest(1, 1, onePixel())
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	if got, want := encodes, 1; got != want {
		t.Fatalf("encodes = %d, want %d", got, want)
	}
	if got, want := len(server.Requests()), 2; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
}

// TestHerdrPresentChangedPlacementReusesPreparedPNG verifies moving a visible
// image calls set with fresh pane geometry but does not repeat PNG preparation.
func TestHerdrPresentChangedPlacementReusesPreparedPNG(t *testing.T) {
	server := newJSONSocketServer(t, []string{
		`{"id":"graphics-info","result":{"type":"pane_graphics_info","cell_width_px":8,"cell_height_px":16}}`,
		`{"id":"graphics-set","result":{"type":"ok"}}`,
		`{"id":"graphics-set","result":{"type":"ok"}}`,
	})
	p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
	encodes := 0
	p.encodePNG = func(image.Image) ([]byte, error) {
		encodes++
		return []byte("png"), nil
	}
	req := testRequest(1, 1, onePixel())
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	req.Rect.X++
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	if got, want := encodes, 1; got != want {
		t.Fatalf("encodes = %d, want %d", got, want)
	}

	requests := server.Requests()
	if got, want := len(requests), 3; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
	placement := requests[2].Params["placement"].(map[string]any)
	if got, want := placement["viewport_col"], float64(7); got != want {
		t.Fatalf("changed viewport_col = %#v, want %#v", got, want)
	}
}

// TestHerdrClearThenPresentReusesPreparedPNG verifies clear removes only the
// active placement, allowing an unchanged image to be restored without encoding.
func TestHerdrClearThenPresentReusesPreparedPNG(t *testing.T) {
	server := newJSONSocketServer(t, []string{
		`{"id":"graphics-info","result":{"type":"pane_graphics_info","cell_width_px":8,"cell_height_px":16}}`,
		`{"id":"graphics-set","result":{"type":"ok"}}`,
		`{"id":"graphics-clear","result":{"type":"ok"}}`,
		`{"id":"graphics-set","result":{"type":"ok"}}`,
	})
	p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
	encodes := 0
	p.encodePNG = func(image.Image) ([]byte, error) {
		encodes++
		return []byte("png"), nil
	}
	req := testRequest(1, 1, onePixel())
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	preparedKey := p.preparedKey
	preparedPNG := append([]byte(nil), p.preparedPNG...)
	if err := p.Clear(); err != nil {
		t.Fatal(err)
	}
	if !p.hasPrepared || p.preparedKey != preparedKey || !bytes.Equal(p.preparedPNG, preparedPNG) {
		t.Fatal("clear discarded prepared image cache")
	}
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	if got, want := encodes, 1; got != want {
		t.Fatalf("encodes = %d, want %d", got, want)
	}
	if got, want := len(server.Requests()), 4; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
}

// TestHerdrPresentChangedViewOrTargetReencodes verifies every prepared-cache
// key component, including revision and target pixel dimensions, triggers PNG
// preparation for a distinct view.
func TestHerdrPresentChangedViewOrTargetReencodes(t *testing.T) {
	server := newJSONSocketServer(t, []string{
		`{"id":"graphics-info","result":{"type":"pane_graphics_info","cell_width_px":8,"cell_height_px":16}}`,
		`{"id":"graphics-set","result":{"type":"ok"}}`,
		`{"id":"graphics-set","result":{"type":"ok"}}`,
		`{"id":"graphics-set","result":{"type":"ok"}}`,
	})
	p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
	encodes := 0
	p.encodePNG = func(image.Image) ([]byte, error) {
		encodes++
		return []byte("png"), nil
	}
	req := testRequest(1, 1, onePixel())
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	req.Image.Revision++
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	req.Rect.Cols--
	if err := p.Present(req); err != nil {
		t.Fatal(err)
	}
	if got, want := encodes, 3; got != want {
		t.Fatalf("encodes = %d, want %d", got, want)
	}
	if got, want := len(server.Requests()), 4; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
}

// TestHerdrClearNoopsUntilSetSucceeds verifies inactive graphics need no socket
// request and a successful set is the only operation that creates placement state.
func TestHerdrClearNoopsUntilSetSucceeds(t *testing.T) {
	p := NewHerdrPresenter("ignored", "w1:p1", func(string, string) (net.Conn, error) {
		return nil, errors.New("unexpected dial")
	})
	if err := p.Clear(); err != nil {
		t.Fatal(err)
	}
	if p.hasPlacement {
		t.Fatal("clear no-op created placement state")
	}
}

// TestHerdrClearUsesExactRequestAndClearsState verifies clear uses a fresh
// connection and removes placement state only after Herdr acknowledges it.
func TestHerdrClearUsesExactRequestAndClearsState(t *testing.T) {
	server := newJSONSocketServer(t, []string{`{"id":"graphics-clear","result":{"type":"ok"}}`})
	p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
	p.hasPlacement = true
	if err := p.Clear(); err != nil {
		t.Fatal(err)
	}
	if p.hasPlacement {
		t.Fatal("successful clear retained placement state")
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if got, want := requests[0].Method, "pane.graphics.clear"; got != want {
		t.Fatalf("clear method = %q, want %q", got, want)
	}
	if got, want := requests[0].Params, map[string]any{"pane_id": "w1:p1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("clear params = %#v, want %#v", got, want)
	}
}

// TestHerdrFeatureDisabledIsReturned verifies the configuration-specific
// feature error retains its message while remaining discoverable with errors.Is.
func TestHerdrFeatureDisabledIsReturned(t *testing.T) {
	server := newJSONSocketServer(t, []string{`{"id":"graphics-info","error":{"code":"feature_disabled","message":"pane graphics require experimental.kitty_graphics"}}`})
	err := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial).Present(testRequest(1, 1, onePixel()))
	if !errors.Is(err, ErrFeatureDisabled) {
		t.Fatalf("error = %v, want ErrFeatureDisabled", err)
	}
	if got, want := err.Error(), "pane graphics require experimental.kitty_graphics"; !strings.Contains(got, want) {
		t.Fatalf("error = %q, want message %q", got, want)
	}
}

// TestHerdrSetPayloadAbove512KiBStreams verifies a large PNG switches from the
// static RPC to a persistent stream and clears by closing that stream.
func TestHerdrSetPayloadAbove512KiBStreams(t *testing.T) {
	server := newGraphicsStreamServer(t)
	p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
	p.cellPixels = CellPixels{Width: 8, Height: 16}
	p.checkedInfo = true
	payload := bytes.Repeat([]byte{0x5a}, herdrStaticPayloadLimit+1)
	encodes := 0
	p.encodePNG = func(image.Image) ([]byte, error) {
		encodes++
		return payload, nil
	}

	req := testRequest(1, 1, onePixel())
	if err := p.Present(req); err != nil {
		t.Fatalf("512 KiB-plus stream payload: %v", err)
	}
	server.WaitForFrame(t)
	request, frame := server.Frame()
	if got, want := request.Method, "pane.graphics.stream"; got != want {
		t.Fatalf("stream method = %q, want %q", got, want)
	}
	if got, want := request.Params, map[string]any{"pane_id": "w1:p1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stream params = %#v, want %#v", got, want)
	}
	if got, want := frame.Format, "png"; got != want {
		t.Fatalf("stream frame format = %q, want %q", got, want)
	}
	if got, want := frame.DataLength, len(payload); got != want {
		t.Fatalf("stream frame data_length = %d, want %d", got, want)
	}
	if !bytes.Equal(frame.Data, payload) {
		t.Fatal("stream frame data differs from prepared PNG")
	}
	if p.streamConn == nil || !p.hasPlacement {
		t.Fatal("stream payload did not retain active placement state")
	}
	if err := p.Present(req); err != nil {
		t.Fatalf("same stream view: %v", err)
	}
	if got, want := encodes, 1; got != want {
		t.Fatalf("encodes = %d, want %d", got, want)
	}
	if err := p.Clear(); err != nil {
		t.Fatalf("clear stream: %v", err)
	}
	server.WaitForClose(t)
	if p.streamConn != nil || p.hasPlacement {
		t.Fatal("clear retained stream placement state")
	}
}

// TestHerdrSetPayloadAbove16MiBFallsBackWithoutSet verifies a stream payload
// above the bounded graphics ceiling requests a per-view fallback before dial.
func TestHerdrSetPayloadAbove16MiBFallsBackWithoutSet(t *testing.T) {
	p := NewHerdrPresenter("ignored", "w1:p1", func(string, string) (net.Conn, error) {
		return nil, errors.New("oversize payload must not dial")
	})
	p.cellPixels = CellPixels{Width: 8, Height: 16}
	p.checkedInfo = true
	encodes := 0
	p.encodePNG = func(image.Image) ([]byte, error) {
		encodes++
		if encodes == 1 {
			return make([]byte, herdrStreamPayloadLimit+1), nil
		}
		return []byte("png"), nil
	}

	req := testRequest(1, 1, onePixel())
	err := p.Present(req)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("error = %v, want ErrPayloadTooLarge", err)
	}
	if p.hasPlacement {
		t.Fatal("oversize payload created placement state")
	}
	if !p.checkedInfo {
		t.Fatal("oversize payload disabled cached info state")
	}
}

// TestHerdrStreamHandshakeFailureDoesNotRetainPlacement verifies an error from
// pane.graphics.stream remains a session-level presenter failure.
func TestHerdrStreamHandshakeFailureDoesNotRetainPlacement(t *testing.T) {
	server := newJSONSocketServer(t, []string{`{"id":"graphics-stream","error":{"code":"feature_disabled","message":"disabled"}}`})
	p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
	p.cellPixels = CellPixels{Width: 8, Height: 16}
	p.checkedInfo = true
	p.encodePNG = func(image.Image) ([]byte, error) {
		return make([]byte, herdrStaticPayloadLimit+1), nil
	}

	err := p.Present(testRequest(1, 1, onePixel()))
	if !errors.Is(err, ErrFeatureDisabled) {
		t.Fatalf("error = %v, want ErrFeatureDisabled", err)
	}
	if p.hasPlacement || p.streamConn != nil {
		t.Fatal("failed stream handshake retained placement state")
	}
}

// TestHerdrClearFailureRetainsPlacementState verifies a failed cleanup does
// not claim that Herdr removed a placement it may still be displaying.
func TestHerdrClearFailureRetainsPlacementState(t *testing.T) {
	p := NewHerdrPresenter("ignored", "w1:p1", func(string, string) (net.Conn, error) {
		return nil, errors.New("dial failed")
	})
	p.hasPlacement = true
	if err := p.Clear(); err == nil {
		t.Fatal("expected clear failure")
	}
	if !p.hasPlacement {
		t.Fatal("failed clear erased placement state")
	}
}

// TestHerdrSetAndClearUse250MillisecondDeadline verifies the deadline covers
// each request's socket I/O rather than only the response read.
func TestHerdrSetAndClearUse250MillisecondDeadline(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	conn := &deadlineConn{Conn: client}
	p := NewHerdrPresenter("ignored", "w1:p1", func(string, string) (net.Conn, error) {
		return conn, nil
	})
	_, err := p.rpc("graphics-clear", "pane.graphics.clear", herdrClearParams{PaneID: "w1:p1"})
	if err == nil {
		t.Fatal("expected I/O failure from test connection")
	}
	if conn.deadline.IsZero() || time.Until(conn.deadline) > herdrTimeout+50*time.Millisecond {
		t.Fatalf("deadline = %v", conn.deadline)
	}
}

// TestHerdrDialTimeoutUsesRPCDeadline verifies an injected dialer cannot make
// the total RPC exceed the Herdr timeout and that a late socket is closed.
func TestHerdrDialTimeoutUsesRPCDeadline(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	releaseDial := make(chan struct{})
	closed := make(chan struct{})
	conn := &closeSignalConn{Conn: client, closed: closed}
	p := NewHerdrPresenter("ignored", "w1:p1", func(string, string) (net.Conn, error) {
		<-releaseDial
		return conn, nil
	})

	started := time.Now()
	_, err := p.rpc("graphics-clear", "pane.graphics.clear", herdrClearParams{PaneID: "w1:p1"})
	if err == nil {
		t.Fatal("expected dial timeout")
	}
	if elapsed := time.Since(started); elapsed > herdrTimeout+150*time.Millisecond {
		t.Fatalf("dial timeout took %s", elapsed)
	}

	close(releaseDial)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("late dial connection was not closed")
	}
}

// TestHerdrPhaseServerAndProtocolFailures verifies each adapter phase rejects
// server errors, malformed result types, and mismatched response IDs.
func TestHerdrPhaseServerAndProtocolFailures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
	}{
		{name: "server error", response: `{"id":"%s","error":{"code":"invalid_request","message":"bad request"}}`},
		{name: "wrong result", response: `{"id":"%s","result":{"type":"unexpected"}}`},
		{name: "mismatched ID", response: `{"id":"not-%s","result":{"type":"%s"}}`},
	} {
		for _, phase := range []string{"info", "set", "clear"} {
			t.Run(tc.name+"/"+phase, func(t *testing.T) {
				response := phaseResponse(tc.response, phase)
				server := newJSONSocketServer(t, []string{response})
				p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
				if err := invokeHerdrPhase(p, phase); err == nil {
					t.Fatalf("%s %s unexpectedly succeeded", phase, tc.name)
				}
				if phase == "clear" && !p.hasPlacement {
					t.Fatal("failed clear erased placement state")
				}
			})
		}
	}
}

// TestHerdrPhaseMalformedAndReadFailures verifies all three calls reject bad
// JSON and a response deadline rather than silently falling back to success.
func TestHerdrPhaseMalformedAndReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
	}{
		{name: "malformed JSON", response: "not-json"},
		{name: "missing result", response: `{"id":"%s"}`},
		{name: "read timeout", response: ""},
	} {
		for _, phase := range []string{"info", "set", "clear"} {
			t.Run(tc.name+"/"+phase, func(t *testing.T) {
				server := newJSONSocketServer(t, []string{phaseResponse(tc.response, phase)})
				p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
				started := time.Now()
				if err := invokeHerdrPhase(p, phase); err == nil {
					t.Fatalf("%s %s unexpectedly succeeded", phase, tc.name)
				}
				if tc.name == "read timeout" && time.Since(started) > herdrTimeout+150*time.Millisecond {
					t.Fatalf("read timeout took %s", time.Since(started))
				}
			})
		}
	}
}

// TestHerdrWriteFailureFailsEachPhase verifies failed socket writes are always
// returned to App so it can disable the selected graphics presenter.
func TestHerdrWriteFailureFailsEachPhase(t *testing.T) {
	for _, phase := range []string{"info", "set", "clear"} {
		t.Run(phase, func(t *testing.T) {
			p := NewHerdrPresenter("ignored", "w1:p1", func(string, string) (net.Conn, error) {
				return &writeFailConn{err: errors.New("write failed")}, nil
			})
			if err := invokeHerdrPhase(p, phase); err == nil {
				t.Fatal("expected write failure")
			}
			if phase == "clear" && !p.hasPlacement {
				t.Fatal("failed clear erased placement state")
			}
		})
	}
}

// TestHerdrInvalidInfoMetricsFail verifies Herdr must report positive integer
// cell dimensions before the adapter can fit or encode an image.
func TestHerdrInvalidInfoMetricsFail(t *testing.T) {
	for _, response := range []string{
		`{"id":"graphics-info","result":{"type":"pane_graphics_info","cell_width_px":0,"cell_height_px":16}}`,
		`{"id":"graphics-info","result":{"type":"pane_graphics_info","cell_width_px":8,"cell_height_px":0}}`,
		`{"id":"graphics-info","result":{"type":"pane_graphics_info","cell_width_px":"8","cell_height_px":16}}`,
	} {
		server := newJSONSocketServer(t, []string{response})
		p := NewHerdrPresenter(server.Path(), "w1:p1", net.Dial)
		if err := p.Present(testRequest(1, 1, onePixel())); err == nil {
			t.Fatal("expected invalid metrics error")
		}
		if p.checkedInfo {
			t.Fatal("invalid metrics were cached")
		}
	}
}

// capturedRequest is the decoded request observed by a local Unix socket test
// server.
type capturedRequest struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// jsonSocketServer serves one response for every short-lived Herdr connection
// and retains decoded requests for assertions.
type jsonSocketServer struct {
	listener  net.Listener
	responses []string
	mu        sync.Mutex
	requests  []capturedRequest
}

// capturedStreamFrame records the header and raw bytes accepted by a stream
// fixture after Herdr acknowledges pane.graphics.stream.
type capturedStreamFrame struct {
	herdrStreamFrame
	Data []byte
}

// graphicsStreamServer accepts one persistent stream, acknowledges its RPC,
// captures its first frame, and remains open until the presenter clears it.
type graphicsStreamServer struct {
	listener net.Listener
	mu       sync.Mutex
	request  capturedRequest
	frame    capturedStreamFrame
	err      error
	received chan struct{}
	done     chan struct{}
}

// newJSONSocketServer starts a local Unix socket server that replies in order.
// An empty response intentionally holds the connection open until the client's
// deadline expires, allowing timeout tests without a process-global clock.
func newJSONSocketServer(t *testing.T, responses []string) *jsonSocketServer {
	t.Helper()
	path := shortSocketPath(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}

	server := &jsonSocketServer{listener: listener, responses: responses}
	go server.serve()
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(path)
	})
	return server
}

// newGraphicsStreamServer starts a fixture for one Herdr stream connection.
func newGraphicsStreamServer(t *testing.T) *graphicsStreamServer {
	t.Helper()
	path := shortSocketPath(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}

	server := &graphicsStreamServer{
		listener: listener,
		received: make(chan struct{}),
		done:     make(chan struct{}),
	}
	go server.serve()
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(path)
	})
	return server
}

// shortSocketPath reserves a short /tmp pathname that fits macOS's Unix-domain
// socket path limit even when Go's per-test temporary directory is long.
func shortSocketPath(t *testing.T) string {
	t.Helper()
	file, err := os.CreateTemp("/tmp", "spiceedit-herdr-*")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(file.Name()), filepath.Base(file.Name())+".sock")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file.Name()); err != nil {
		t.Fatal(err)
	}
	return path
}

// Path returns the local Unix socket path exposed by the fixture server.
func (s *jsonSocketServer) Path() string {
	return s.listener.Addr().String()
}

// Requests returns a stable copy of fixture requests observed so far.
func (s *jsonSocketServer) Requests() []capturedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	requests := make([]capturedRequest, len(s.requests))
	copy(requests, s.requests)
	return requests
}

// Path returns the Unix socket path used by the one-stream fixture.
func (s *graphicsStreamServer) Path() string {
	return s.listener.Addr().String()
}

// WaitForFrame blocks until the fixture receives a frame or exposes a server
// parsing failure instead of leaving a test waiting indefinitely.
func (s *graphicsStreamServer) WaitForFrame(t *testing.T) {
	t.Helper()
	select {
	case <-s.received:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Herdr stream frame")
	}
	s.mu.Lock()
	err := s.err
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
}

// WaitForClose blocks until the presenter closes the persistent stream.
func (s *graphicsStreamServer) WaitForClose(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Herdr stream close")
	}
}

// Frame returns stable copies of the captured stream request and frame.
func (s *graphicsStreamServer) Frame() (capturedRequest, capturedStreamFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	request := s.request
	request.Params = mapsClone(s.request.Params)
	frame := s.frame
	frame.Data = append([]byte(nil), s.frame.Data...)
	return request, frame
}

// serve accepts one short-lived Unix socket per configured fixture response.
func (s *jsonSocketServer) serve() {
	for _, response := range s.responses {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}

		var request capturedRequest
		err = json.NewDecoder(conn).Decode(&request)
		if err == nil {
			s.mu.Lock()
			s.requests = append(s.requests, request)
			s.mu.Unlock()
		}
		if response != "" {
			_, _ = io.WriteString(conn, response+"\n")
		} else {
			_, _ = io.Copy(io.Discard, conn)
		}
		_ = conn.Close()
	}
}

// serve completes one stream handshake, captures the first raw frame, and
// keeps the connection open until the presenter closes its stream layer.
func (s *graphicsStreamServer) serve() {
	defer close(s.done)
	conn, err := s.listener.Accept()
	if err != nil {
		s.recordFailure(err)
		close(s.received)
		return
	}
	defer conn.Close()

	reader := bufio.NewReaderSize(conn, herdrMaxResponseBytes)
	line, err := readHerdrResponse(reader)
	if err != nil {
		s.recordFailure(fmt.Errorf("read stream request: %w", err))
		close(s.received)
		return
	}
	var request capturedRequest
	if err := json.Unmarshal(line, &request); err != nil {
		s.recordFailure(fmt.Errorf("decode stream request: %w", err))
		close(s.received)
		return
	}
	if _, err := io.WriteString(conn, `{"id":"graphics-stream","result":{"type":"ok"}}`+"\n"); err != nil {
		s.recordFailure(fmt.Errorf("ack stream request: %w", err))
		close(s.received)
		return
	}

	line, err = readHerdrResponse(reader)
	if err != nil {
		s.recordFailure(fmt.Errorf("read stream frame header: %w", err))
		close(s.received)
		return
	}
	var header herdrStreamFrame
	if err := json.Unmarshal(line, &header); err != nil {
		s.recordFailure(fmt.Errorf("decode stream frame header: %w", err))
		close(s.received)
		return
	}
	if header.DataLength < 0 || header.DataLength > herdrStreamPayloadLimit {
		s.recordFailure(fmt.Errorf("invalid stream data_length %d", header.DataLength))
		close(s.received)
		return
	}
	data := make([]byte, header.DataLength)
	if _, err := io.ReadFull(reader, data); err != nil {
		s.recordFailure(fmt.Errorf("read stream frame data: %w", err))
		close(s.received)
		return
	}

	s.mu.Lock()
	s.request = request
	s.frame = capturedStreamFrame{herdrStreamFrame: header, Data: data}
	s.mu.Unlock()
	close(s.received)
	_, _ = io.Copy(io.Discard, reader)
}

// recordFailure stores the fixture failure for the waiting test.
func (s *graphicsStreamServer) recordFailure(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

// mapsClone makes request-map assertions independent of fixture ownership.
func mapsClone(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	clone := make(map[string]any, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

// deadlineConn records the combined I/O deadline applied to an RPC connection.
type deadlineConn struct {
	net.Conn
	deadline time.Time
}

// closeSignalConn reports the late-dial cleanup performed by the adapter.
type closeSignalConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

// Close implements net.Conn and records only its first close operation.
func (c *closeSignalConn) Close() error {
	c.once.Do(func() {
		close(c.closed)
	})
	return c.Conn.Close()
}

// SetDeadline records and applies the connection-wide Herdr RPC deadline.
func (c *deadlineConn) SetDeadline(deadline time.Time) error {
	c.deadline = deadline
	return c.Conn.SetDeadline(deadline)
}

// writeFailConn fails every write after accepting the adapter's deadline call.
type writeFailConn struct {
	err error
}

// Read implements net.Conn for writeFailConn.
func (c *writeFailConn) Read([]byte) (int, error) {
	return 0, io.EOF
}

// Write implements net.Conn for writeFailConn.
func (c *writeFailConn) Write([]byte) (int, error) {
	return 0, c.err
}

// Close implements net.Conn for writeFailConn.
func (c *writeFailConn) Close() error {
	return nil
}

// LocalAddr implements net.Conn for writeFailConn.
func (c *writeFailConn) LocalAddr() net.Addr {
	return testAddr("local")
}

// RemoteAddr implements net.Conn for writeFailConn.
func (c *writeFailConn) RemoteAddr() net.Addr {
	return testAddr("remote")
}

// SetDeadline implements net.Conn for writeFailConn.
func (c *writeFailConn) SetDeadline(time.Time) error {
	return nil
}

// SetReadDeadline implements net.Conn for writeFailConn.
func (c *writeFailConn) SetReadDeadline(time.Time) error {
	return nil
}

// SetWriteDeadline implements net.Conn for writeFailConn.
func (c *writeFailConn) SetWriteDeadline(time.Time) error {
	return nil
}

// testAddr is a minimal net.Addr used by the write-failure connection.
type testAddr string

// Network implements net.Addr for testAddr.
func (a testAddr) Network() string {
	return "test"
}

// String implements net.Addr for testAddr.
func (a testAddr) String() string {
	return string(a)
}

// invokeHerdrPhase invokes one adapter lifecycle phase after installing any
// prerequisite state that normally comes from an earlier successful RPC.
func invokeHerdrPhase(p *herdrPresenter, phase string) error {
	switch phase {
	case "info":
		return p.Present(testRequest(1, 1, onePixel()))
	case "set":
		p.checkedInfo = true
		p.cellPixels = CellPixels{Width: 8, Height: 16}
		return p.Present(testRequest(1, 1, onePixel()))
	case "clear":
		p.hasPlacement = true
		return p.Clear()
	default:
		panic("unknown Herdr phase " + phase)
	}
}

// phaseResponse fills response placeholders with the expected identifier and
// expected success result type for one lifecycle phase.
func phaseResponse(template, phase string) string {
	id := "graphics-" + phase
	resultType := "ok"
	if phase == "info" {
		resultType = "pane_graphics_info"
	}
	return fmt.Sprintf(template, id, resultType)
}

// mapKeys returns sorted map keys for exact JSON-field assertions.
func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
