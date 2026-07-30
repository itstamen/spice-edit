// =============================================================================
// File: internal/termgraphics/herdr.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package termgraphics

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net"
	"time"

	"github.com/cloudmanic/spice-edit/internal/imageutil"
)

const (
	herdrTimeout            = 250 * time.Millisecond
	herdrStaticPayloadLimit = 512 << 10
	herdrStreamPayloadLimit = 16 << 20
	herdrStreamWriteTimeout = 5 * time.Second
	herdrMaxResponseBytes   = 64 << 10
)

// ErrFeatureDisabled marks Herdr's opt-in pane graphics capability as
// unavailable for the current session.
var ErrFeatureDisabled = errors.New("Herdr graphics feature disabled")

// rpcRequest is the one-line JSON request sent for each Herdr socket call.
type rpcRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

// rpcResponse is the one-line JSON reply received from a Herdr socket call.
type rpcResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// rpcError is a failed Herdr RPC response returned by the server.
type rpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// herdrPresenter uses Herdr's static and streaming pane graphics RPC methods.
// Static calls own short-lived connections, while a large streamed image owns
// a connection until Clear closes it.
type herdrPresenter struct {
	socketPath string
	paneID     string
	dial       func(string, string) (net.Conn, error)

	checkedInfo   bool
	cellPixels    CellPixels
	preparedKey   cacheKey
	preparedPNG   []byte
	hasPrepared   bool
	hasPlacement  bool
	lastPlacement Placement
	streamConn    net.Conn
	encodePNG     func(image.Image) ([]byte, error)
}

// herdrInfoParams identifies the pane whose graphics capability is queried.
type herdrInfoParams struct {
	PaneID string `json:"pane_id"`
}

// herdrInfoResult is the required metrics response from pane.graphics.info.
type herdrInfoResult struct {
	Type         string `json:"type"`
	CellWidthPX  int    `json:"cell_width_px"`
	CellHeightPX int    `json:"cell_height_px"`
}

// herdrSetParams is the complete static image request accepted by Herdr 0.7.4.
type herdrSetParams struct {
	PaneID      string         `json:"pane_id"`
	Format      string         `json:"format"`
	ImageWidth  int            `json:"image_width"`
	ImageHeight int            `json:"image_height"`
	DataBase64  string         `json:"data_base64"`
	Placement   herdrPlacement `json:"placement"`
}

// herdrPlacement is a pane-relative whole-cell image placement.
type herdrPlacement struct {
	ViewportCol int `json:"viewport_col"`
	ViewportRow int `json:"viewport_row"`
	GridCols    int `json:"grid_cols"`
	GridRows    int `json:"grid_rows"`
}

// herdrClearParams identifies the pane whose static graphics layer is removed.
type herdrClearParams struct {
	PaneID string `json:"pane_id"`
}

// herdrStreamParams starts a persistent stream owned by one pane. Individual
// frame geometry and data follow on the same socket after the RPC reply.
type herdrStreamParams struct {
	PaneID string `json:"pane_id"`
}

// herdrStreamFrame is the newline-delimited JSON header preceding one raw PNG
// frame on a Herdr pane.graphics.stream connection.
type herdrStreamFrame struct {
	Format      string         `json:"format"`
	ImageWidth  int            `json:"image_width"`
	ImageHeight int            `json:"image_height"`
	DataLength  int            `json:"data_length"`
	Placement   herdrPlacement `json:"placement"`
}

// herdrOKResult is the required successful result for set, clear, and stream
// handshake calls.
type herdrOKResult struct {
	Type string `json:"type"`
}

// NewHerdrPresenter creates a Herdr pane graphics presenter. Small images use
// static requests while larger images retain a stream connection. dial is
// injectable for socket tests and defaults to net.Dial when omitted.
func NewHerdrPresenter(socketPath, paneID string, dial func(string, string) (net.Conn, error)) *herdrPresenter {
	if dial == nil {
		dial = net.Dial
	}

	return &herdrPresenter{
		socketPath: socketPath,
		paneID:     paneID,
		dial:       dial,
		encodePNG:  encodePNG,
	}
}

// Present verifies pane graphics metrics once, prepares a fitted PNG, and
// uses static or streamed Herdr graphics according to its encoded size.
func (p *herdrPresenter) Present(req Request) error {
	if p == nil {
		return errors.New("Herdr graphics presenter is nil")
	}
	if err := p.ensureInfo(); err != nil {
		return err
	}

	placement, ok := Fit(req.Image.Source, req.Rect, p.cellPixels)
	if !ok {
		return errors.New("invalid Herdr graphics image placement")
	}

	key := cacheKey{
		imageID:     req.Image.ID,
		revision:    req.Image.Revision,
		pixelWidth:  placement.PixelWidth,
		pixelHeight: placement.PixelHeight,
	}
	if p.hasPlacement && p.hasPrepared && p.preparedKey == key && p.lastPlacement == placement {
		return nil
	}

	if !p.hasPrepared || p.preparedKey != key {
		pngData, err := prepareHerdrPNG(req.Image.Source, placement.PixelWidth, placement.PixelHeight, p.encodePNG)
		if err != nil {
			return err
		}
		if len(pngData) > herdrStreamPayloadLimit {
			return ErrPayloadTooLarge
		}

		p.preparedKey = key
		p.preparedPNG = pngData
		p.hasPrepared = true
	}
	if len(p.preparedPNG) > herdrStaticPayloadLimit {
		if p.streamConn != nil {
			if err := p.Clear(); err != nil {
				return err
			}
		}
		if p.hasPlacement {
			if err := p.Clear(); err != nil {
				return err
			}
		}
		return p.presentStream(placement)
	}
	if p.streamConn != nil {
		if err := p.Clear(); err != nil {
			return err
		}
	}

	result, err := p.rpc("graphics-set", "pane.graphics.set", herdrSetParams{
		PaneID:      p.paneID,
		Format:      "png",
		ImageWidth:  placement.PixelWidth,
		ImageHeight: placement.PixelHeight,
		DataBase64:  base64.StdEncoding.EncodeToString(p.preparedPNG),
		Placement: herdrPlacement{
			ViewportCol: placement.ViewportCol,
			ViewportRow: placement.ViewportRow,
			GridCols:    placement.GridCols,
			GridRows:    placement.GridRows,
		},
	})
	if err != nil {
		return err
	}
	if err := expectHerdrOK(result); err != nil {
		return fmt.Errorf("Herdr pane.graphics.set: %w", err)
	}

	p.hasPlacement = true
	p.lastPlacement = placement
	return nil
}

// Clear removes an active static layer or closes an active stream layer.
func (p *herdrPresenter) Clear() error {
	if p == nil || !p.hasPlacement {
		return nil
	}
	if p.streamConn != nil {
		if err := p.streamConn.Close(); err != nil {
			return fmt.Errorf("close Herdr pane.graphics.stream: %w", err)
		}
		p.streamConn = nil
		p.hasPlacement = false
		p.lastPlacement = Placement{}
		return nil
	}

	result, err := p.rpc("graphics-clear", "pane.graphics.clear", herdrClearParams{PaneID: p.paneID})
	if err != nil {
		return err
	}
	if err := expectHerdrOK(result); err != nil {
		return fmt.Errorf("Herdr pane.graphics.clear: %w", err)
	}

	p.hasPlacement = false
	p.lastPlacement = Placement{}
	return nil
}

// presentStream opens Herdr's persistent graphics stream, writes one PNG frame,
// and retains the connection because Herdr removes the layer when it closes.
func (p *herdrPresenter) presentStream(placement Placement) error {
	request, err := json.Marshal(rpcRequest{
		ID:     "graphics-stream",
		Method: "pane.graphics.stream",
		Params: herdrStreamParams{PaneID: p.paneID},
	})
	if err != nil {
		return fmt.Errorf("encode Herdr pane.graphics.stream request: %w", err)
	}
	request = append(request, '\n')

	deadline := time.Now().Add(herdrTimeout)
	conn, err := p.dialUntil(deadline)
	if err != nil {
		return err
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return fmt.Errorf("set Herdr pane.graphics.stream deadline: %w", err)
	}
	if err := writeAll(conn, request); err != nil {
		_ = conn.Close()
		return fmt.Errorf("write Herdr pane.graphics.stream request: %w", err)
	}

	line, err := readHerdrResponse(bufio.NewReaderSize(conn, herdrMaxResponseBytes))
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("read Herdr pane.graphics.stream response: %w", err)
	}
	result, err := parseHerdrResponse("graphics-stream", "pane.graphics.stream", line)
	if err != nil {
		_ = conn.Close()
		return err
	}
	if err := expectHerdrOK(result); err != nil {
		_ = conn.Close()
		return fmt.Errorf("Herdr pane.graphics.stream: %w", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return fmt.Errorf("clear Herdr pane.graphics.stream deadline: %w", err)
	}

	frame, err := json.Marshal(herdrStreamFrame{
		Format:      "png",
		ImageWidth:  placement.PixelWidth,
		ImageHeight: placement.PixelHeight,
		DataLength:  len(p.preparedPNG),
		Placement: herdrPlacement{
			ViewportCol: placement.ViewportCol,
			ViewportRow: placement.ViewportRow,
			GridCols:    placement.GridCols,
			GridRows:    placement.GridRows,
		},
	})
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("encode Herdr pane.graphics.stream frame: %w", err)
	}
	frame = append(frame, '\n')
	if err := conn.SetWriteDeadline(time.Now().Add(herdrStreamWriteTimeout)); err != nil {
		_ = conn.Close()
		return fmt.Errorf("set Herdr pane.graphics.stream frame deadline: %w", err)
	}
	if err := writeAll(conn, frame); err != nil {
		_ = conn.Close()
		return fmt.Errorf("write Herdr pane.graphics.stream frame header: %w", err)
	}
	if err := writeAll(conn, p.preparedPNG); err != nil {
		_ = conn.Close()
		return fmt.Errorf("write Herdr pane.graphics.stream frame: %w", err)
	}
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return fmt.Errorf("clear Herdr pane.graphics.stream frame deadline: %w", err)
	}

	p.streamConn = conn
	p.hasPlacement = true
	p.lastPlacement = placement
	return nil
}

// ensureInfo fetches and caches the authoritative cell-pixel dimensions for
// this Herdr pane before its first graphics placement.
func (p *herdrPresenter) ensureInfo() error {
	if p.checkedInfo {
		return nil
	}

	result, err := p.rpc("graphics-info", "pane.graphics.info", herdrInfoParams{PaneID: p.paneID})
	if err != nil {
		return err
	}

	var info herdrInfoResult
	if err := json.Unmarshal(result, &info); err != nil {
		return fmt.Errorf("invalid Herdr pane.graphics.info result: %w", err)
	}
	if info.Type != "pane_graphics_info" {
		return fmt.Errorf("unexpected Herdr pane.graphics.info result type %q", info.Type)
	}
	if info.CellWidthPX <= 0 || info.CellHeightPX <= 0 {
		return fmt.Errorf("invalid Herdr pane graphics cell dimensions %dx%d", info.CellWidthPX, info.CellHeightPX)
	}

	p.cellPixels = CellPixels{Width: info.CellWidthPX, Height: info.CellHeightPX}
	p.checkedInfo = true
	return nil
}

// rpc exchanges exactly one newline-delimited JSON request and response on a
// fresh Unix-socket connection within the shared Herdr deadline.
func (p *herdrPresenter) rpc(id, method string, params any) (json.RawMessage, error) {
	if p == nil {
		return nil, errors.New("Herdr graphics presenter is nil")
	}
	if p.dial == nil {
		return nil, errors.New("Herdr graphics presenter has no dialer")
	}

	request, err := json.Marshal(rpcRequest{ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("encode Herdr %s request: %w", method, err)
	}
	request = append(request, '\n')

	deadline := time.Now().Add(herdrTimeout)
	conn, err := p.dialUntil(deadline)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set Herdr %s deadline: %w", method, err)
	}
	if err := writeAll(conn, request); err != nil {
		return nil, fmt.Errorf("write Herdr %s request: %w", method, err)
	}

	line, err := readHerdrResponse(bufio.NewReaderSize(conn, herdrMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read Herdr %s response: %w", method, err)
	}
	return parseHerdrResponse(id, method, line)
}

// parseHerdrResponse validates one Herdr NDJSON reply and returns its required
// result payload so short-lived RPCs and stream handshakes share one contract.
func parseHerdrResponse(id, method string, line []byte) (json.RawMessage, error) {
	var response rpcResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return nil, fmt.Errorf("decode Herdr %s response: %w", method, err)
	}
	if response.ID == "" {
		return nil, fmt.Errorf("Herdr %s response has no ID", method)
	}
	if response.ID != id {
		return nil, fmt.Errorf("Herdr %s response ID %q does not match %q", method, response.ID, id)
	}
	if response.Error != nil {
		if response.Error.Code == "feature_disabled" {
			return nil, fmt.Errorf("%w: %s", ErrFeatureDisabled, response.Error.Message)
		}
		return nil, fmt.Errorf("Herdr %s failed with %q: %s", method, response.Error.Code, response.Error.Message)
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return nil, fmt.Errorf("Herdr %s response has no result", method)
	}

	return response.Result, nil
}

// dialUntil opens a Unix socket before deadline and closes a late connection
// if an injected dialer outlives the RPC deadline.
func (p *herdrPresenter) dialUntil(deadline time.Time) (net.Conn, error) {
	timeout := time.Until(deadline)
	if timeout <= 0 {
		return nil, fmt.Errorf("Herdr socket dial timed out after %s", herdrTimeout)
	}

	type dialResult struct {
		conn net.Conn
		err  error
	}

	result := make(chan dialResult)
	canceled := make(chan struct{})
	go func() {
		conn, err := p.dial("unix", p.socketPath)
		select {
		case result <- dialResult{conn: conn, err: err}:
		case <-canceled:
			if conn != nil {
				_ = conn.Close()
			}
		}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case dialed := <-result:
		close(canceled)
		if dialed.err != nil {
			return nil, fmt.Errorf("dial Herdr socket: %w", dialed.err)
		}
		if dialed.conn == nil {
			return nil, errors.New("dial Herdr socket returned no connection")
		}
		return dialed.conn, nil
	case <-timer.C:
		close(canceled)
		return nil, fmt.Errorf("Herdr socket dial timed out after %s", herdrTimeout)
	}
}

// prepareHerdrPNG scales src to its fitted physical dimensions and PNG-encodes
// the result for Herdr's static and streaming graphics APIs.
func prepareHerdrPNG(src image.Image, width, height int, encode func(image.Image) ([]byte, error)) ([]byte, error) {
	if encode == nil {
		return nil, errors.New("Herdr graphics presenter has no PNG encoder")
	}

	resized := imageutil.ResizeNearest(src, width, height)
	if resized == nil {
		return nil, errors.New("invalid Herdr graphics target size")
	}

	return encode(resized)
}

// expectHerdrOK validates the success result expected from Herdr graphics
// set, clear, and stream-handshake requests.
func expectHerdrOK(result json.RawMessage) error {
	var ok herdrOKResult
	if err := json.Unmarshal(result, &ok); err != nil {
		return fmt.Errorf("invalid result: %w", err)
	}
	if ok.Type != "ok" {
		return fmt.Errorf("unexpected result type %q", ok.Type)
	}
	return nil
}

// readHerdrResponse reads one bounded, newline-delimited JSON response.
func readHerdrResponse(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > herdrMaxResponseBytes {
			return nil, fmt.Errorf("response exceeds %d bytes", herdrMaxResponseBytes)
		}
		line = append(line, fragment...)
		if err == nil {
			if len(line) == 1 {
				return nil, errors.New("empty response")
			}
			return line[:len(line)-1], nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return nil, err
	}
}
