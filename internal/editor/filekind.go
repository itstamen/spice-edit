// =============================================================================
// File: internal/editor/filekind.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const fileSniffLimit = 8 * 1024

// fileKind is the mode NewTab should use after inspecting a path without
// loading an unsupported file's complete contents into memory.
type fileKind uint8

const (
	fileKindText fileKind = iota
	fileKindImage
	fileKindExternal
)

// externalExtensions lists formats that belong in a desktop application even
// when their first bytes happen to be valid UTF-8, as is common for PDFs.
var externalExtensions = map[string]struct{}{
	".pdf": {}, ".doc": {}, ".docx": {}, ".xls": {}, ".xlsx": {},
	".ppt": {}, ".pptx": {}, ".odt": {}, ".ods": {}, ".odp": {},
	".zip": {}, ".tar": {}, ".gz": {}, ".bz2": {}, ".xz": {},
	".7z": {}, ".rar": {}, ".jar": {}, ".war": {},
	".db": {}, ".sqlite": {}, ".sqlite3": {},
	".mp3": {}, ".wav": {}, ".flac": {}, ".aac": {}, ".ogg": {},
	".mp4": {}, ".mov": {}, ".avi": {}, ".mkv": {}, ".webm": {},
	".exe": {}, ".dll": {}, ".so": {}, ".dylib": {}, ".bin": {},
	".class": {}, ".o": {}, ".a": {},
	".woff": {}, ".woff2": {}, ".ttf": {}, ".otf": {},
	".webp": {}, ".bmp": {}, ".tif": {}, ".tiff": {}, ".ico": {}, ".heic": {},
}

// classifyFile returns the safe editor mode for path. It reads at most 8 KiB
// so large text files are not loaded twice before NewTab commits to text mode.
func classifyFile(path string) (fileKind, error) {
	if path == "" {
		return fileKindText, nil
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return fileKindText, nil
	}
	if err != nil {
		return fileKindText, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fileKindText, err
	}
	if !info.Mode().IsRegular() {
		return fileKindText, fmt.Errorf("%s is not a regular file", path)
	}

	sample := make([]byte, fileSniffLimit)
	n, err := io.ReadFull(f, sample)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return fileKindText, err
	}
	sample = sample[:n]

	if isImageExt(path) {
		return fileKindImage, nil
	}
	truncated := info.Size() > int64(n)
	if isExternalExt(path) || bytes.IndexByte(sample, 0) >= 0 || !validUTF8Sample(sample, truncated) {
		return fileKindExternal, nil
	}
	return fileKindText, nil
}

// validUTF8Sample accepts a valid prefix whose final rune crosses the sampling
// boundary, while still rejecting malformed UTF-8 elsewhere in the sample.
func validUTF8Sample(sample []byte, truncated bool) bool {
	if utf8.Valid(sample) {
		return true
	}
	if !truncated {
		return false
	}
	for suffixLen := 1; suffixLen < utf8.UTFMax && suffixLen <= len(sample); suffixLen++ {
		prefix := sample[:len(sample)-suffixLen]
		suffix := sample[len(sample)-suffixLen:]
		if utf8.Valid(prefix) && !utf8.FullRune(suffix) {
			return true
		}
	}
	return false
}

// isExternalExt reports whether a file format should always use the host's
// default application instead of SpiceEdit's text or image renderers.
func isExternalExt(path string) bool {
	_, ok := externalExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}
