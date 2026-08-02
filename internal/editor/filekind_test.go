// =============================================================================
// File: internal/editor/filekind_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestClassifyFile covers the three file kinds and the content signals that
// must override a misleading filename before bytes enter an editable buffer.
func TestClassifyFile(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		data    []byte
		want    fileKind
		missing bool
	}{
		{name: "missing path remains text", path: filepath.Join(dir, "new.txt"), want: fileKindText, missing: true},
		{name: "empty file", path: filepath.Join(dir, "empty.unknown"), want: fileKindText},
		{name: "unknown utf8 text", path: filepath.Join(dir, "notes.custom"), data: []byte("hello 世界\n"), want: fileKindText},
		{name: "nul overrides text extension", path: filepath.Join(dir, "payload.txt"), data: []byte{'a', 0, 'b'}, want: fileKindExternal},
		{name: "invalid utf8 overrides text extension", path: filepath.Join(dir, "invalid.md"), data: []byte{0xff, 0xfe}, want: fileKindExternal},
		{name: "ascii pdf remains external", path: filepath.Join(dir, "manual.pdf"), data: []byte("%PDF-1.7\nplain header"), want: fileKindExternal},
		{name: "supported image candidate", path: filepath.Join(dir, "photo.PNG"), data: []byte{0x89, 'P', 'N', 'G', 0}, want: fileKindImage},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.missing {
				if err := os.WriteFile(tc.path, tc.data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := classifyFile(tc.path)
			if err != nil {
				t.Fatalf("classifyFile: %v", err)
			}
			if got != tc.want {
				t.Fatalf("kind = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestClassifyFile_DirectoryReturnsError prevents directories from becoming
// empty text tabs or unsupported placeholders through the shared open path.
func TestClassifyFile_DirectoryReturnsError(t *testing.T) {
	if _, err := classifyFile(t.TempDir()); err == nil {
		t.Fatal("expected directory classification error")
	}
}

// TestClassifyFile_UTF8AcrossSampleBoundary prevents a valid multibyte rune
// split at the 8 KiB sniff boundary from turning a text file into external.
func TestClassifyFile_UTF8AcrossSampleBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "boundary.txt")
	data := append(make([]byte, fileSniffLimit-1), []byte("界 after boundary")...)
	for i := 0; i < fileSniffLimit-1; i++ {
		data[i] = 'a'
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := classifyFile(path)
	if err != nil {
		t.Fatalf("classifyFile: %v", err)
	}
	if got != fileKindText {
		t.Fatalf("kind = %v, want text", got)
	}
}

// TestClassifyFile_IncompleteUTF8AtEOF ensures the boundary allowance does
// not accept a genuinely truncated final rune when no more bytes exist.
func TestClassifyFile_IncompleteUTF8AtEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.txt")
	data := make([]byte, fileSniffLimit)
	for i := range data {
		data[i] = 'a'
	}
	data[len(data)-1] = 0xe7
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := classifyFile(path)
	if err != nil {
		t.Fatalf("classifyFile: %v", err)
	}
	if got != fileKindExternal {
		t.Fatalf("kind = %v, want external", got)
	}
}
