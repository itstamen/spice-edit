// =============================================================================
// File: internal/termgraphics/kitty.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-07-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package termgraphics

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"

	"github.com/cloudmanic/spice-edit/internal/imageutil"
)

const (
	kittyImageID       uint32 = 1
	kittyPlacementID   uint32 = 1
	kittyChunkSize            = 4096
	directPayloadLimit        = 16 << 20
)

// cacheKey identifies a PNG prepared for one immutable image view and target
// pixel size.
type cacheKey struct {
	imageID     uint64
	revision    uint64
	pixelWidth  int
	pixelHeight int
}

// kittyPresenter sends image previews directly through the Kitty Graphics
// Protocol while retaining one prepared PNG and tracking the terminal upload
// separately from its visible placement.
type kittyPresenter struct {
	writer        io.Writer
	preparedKey   cacheKey
	preparedPNG   []byte
	hasPrepared   bool
	uploadedKey   cacheKey
	hasUploaded   bool
	hasPlacement  bool
	lastPlacement Placement
	encodePNG     func(image.Image) ([]byte, error)
}

// NewKittyPresenter creates a direct Kitty Graphics Protocol presenter that
// writes protocol escapes to writer.
func NewKittyPresenter(writer io.Writer) *kittyPresenter {
	return &kittyPresenter{
		writer:    writer,
		encodePNG: encodePNG,
	}
}

// Present prepares req at its fitted physical size, uploads its PNG in quiet
// chunks, and places the uploaded image at the fitted cell position.
func (p *kittyPresenter) Present(req Request) error {
	if p == nil || p.writer == nil {
		return errors.New("Kitty graphics presenter has no writer")
	}

	placement, ok := Fit(req.Image.Source, req.Rect, req.Cells)
	if !ok {
		return errors.New("invalid Kitty graphics image placement")
	}

	key := cacheKey{
		imageID:     req.Image.ID,
		revision:    req.Image.Revision,
		pixelWidth:  placement.PixelWidth,
		pixelHeight: placement.PixelHeight,
	}
	if p.hasPlacement && p.hasUploaded && p.uploadedKey == key && p.lastPlacement == placement {
		return nil
	}

	if !p.hasUploaded || p.uploadedKey != key {
		if !p.hasPrepared || p.preparedKey != key {
			prepared, err := prepareKittyPNG(req.Image.Source, placement.PixelWidth, placement.PixelHeight, p.encodePNG)
			if err != nil {
				return err
			}
			if len(prepared) > directPayloadLimit {
				return ErrPayloadTooLarge
			}

			p.preparedKey = key
			p.preparedPNG = prepared
			p.hasPrepared = true
		}

		// Reusing the stable Kitty image ID can replace its prior image and
		// delete its placements. Treat both as gone before beginning a transfer
		// so either a transfer or subsequent placement failure leaves safe state.
		p.hasPlacement = false
		p.lastPlacement = Placement{}
		p.hasUploaded = false
		if err := p.writeTransfer(p.preparedPNG); err != nil {
			return err
		}

		p.uploadedKey = key
		p.hasUploaded = true
	}

	// Replacing a placement with the same stable placement ID can still leave
	// the terminal in an uncertain state if its write fails. Clear the visible
	// state before the command and record its geometry only after success.
	p.hasPlacement = false
	p.lastPlacement = Placement{}
	if err := p.writePlacement(placement); err != nil {
		return err
	}

	p.hasPlacement = true
	p.lastPlacement = placement
	return nil
}

// Clear removes only this presenter's active placement and leaves no state
// change behind if the terminal rejects the clear request.
func (p *kittyPresenter) Clear() error {
	if p == nil || !p.hasPlacement {
		return nil
	}
	if p.writer == nil {
		return errors.New("Kitty graphics presenter has no writer")
	}

	if err := writeAll(p.writer, []byte("\x1b_Ga=d,d=i,i=1,p=1,q=2;\x1b\\")); err != nil {
		return err
	}

	p.hasPlacement = false
	p.lastPlacement = Placement{}
	return nil
}

// prepareKittyPNG scales src to its target dimensions before encoding it as a
// PNG suitable for transfer through the Kitty Graphics Protocol.
func prepareKittyPNG(src image.Image, width, height int, encode func(image.Image) ([]byte, error)) ([]byte, error) {
	if encode == nil {
		return nil, errors.New("Kitty graphics presenter has no PNG encoder")
	}

	resized := imageutil.ResizeNearest(src, width, height)
	if resized == nil {
		return nil, errors.New("invalid Kitty graphics target size")
	}

	return encode(resized)
}

// encodePNG returns the standard PNG encoding of src.
func encodePNG(src image.Image) ([]byte, error) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// writeTransfer uploads png in base64 chunks whose encoded payloads fit the
// Kitty Graphics Protocol's 4096-byte chunk limit.
func (p *kittyPresenter) writeTransfer(png []byte) error {
	chunks := chunkBase64(png)
	for index, chunk := range chunks {
		more := 0
		if index < len(chunks)-1 {
			more = 1
		}

		var command string
		if index == 0 {
			command = fmt.Sprintf("\x1b_Ga=t,f=100,i=%d,q=2,m=%d;%s\x1b\\", kittyImageID, more, chunk)
		} else {
			command = fmt.Sprintf("\x1b_Gq=2,m=%d;%s\x1b\\", more, chunk)
		}
		if err := writeAll(p.writer, []byte(command)); err != nil {
			return err
		}
	}

	return nil
}

// writePlacement positions the cursor at placement's zero-based origin and
// creates a cell-aligned placement for the uploaded stable image ID.
func (p *kittyPresenter) writePlacement(placement Placement) error {
	cursor := fmt.Sprintf("\x1b[%d;%dH", placement.ViewportRow+1, placement.ViewportCol+1)
	command := fmt.Sprintf(
		"\x1b_Ga=p,i=%d,p=%d,c=%d,r=%d,C=1,q=2;\x1b\\",
		kittyImageID,
		kittyPlacementID,
		placement.GridCols,
		placement.GridRows,
	)

	if err := writeAll(p.writer, []byte(cursor)); err != nil {
		return err
	}
	return writeAll(p.writer, []byte(command))
}

// chunkBase64 encodes data and splits it into whole-base64 chunks no larger
// than kittyChunkSize bytes.
func chunkBase64(data []byte) []string {
	encoded := base64.StdEncoding.EncodeToString(data)
	if encoded == "" {
		return []string{""}
	}

	chunks := make([]string, 0, (len(encoded)+kittyChunkSize-1)/kittyChunkSize)
	for len(encoded) > 0 {
		size := kittyChunkSize
		if len(encoded) < size {
			size = len(encoded)
		}
		chunks = append(chunks, encoded[:size])
		encoded = encoded[size:]
	}
	return chunks
}

// writeAll writes data completely, retrying partial writes and rejecting a
// zero-byte success as io.ErrShortWrite.
func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if written < 0 || written > len(data) {
			return fmt.Errorf("invalid write count %d for %d-byte buffer", written, len(data))
		}
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}
