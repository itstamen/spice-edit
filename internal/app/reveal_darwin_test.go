//go:build darwin

// =============================================================================
// File: internal/app/reveal_darwin_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-08-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package app

import (
	"reflect"
	"testing"
)

// TestDarwinRevealArgs keeps paths containing spaces, Unicode, and shell
// metacharacters in one literal argument.
func TestDarwinRevealArgs(t *testing.T) {
	path := "/tmp/规格 draft $?.pdf"
	tests := []struct {
		name       string
		selectFile bool
		want       []string
	}{
		{name: "select file", selectFile: true, want: []string{"-R", path}},
		{name: "open directory", selectFile: false, want: []string{path}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := darwinRevealArgs(path, tc.selectFile); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("args = %#v, want %#v", got, tc.want)
			}
		})
	}
}
