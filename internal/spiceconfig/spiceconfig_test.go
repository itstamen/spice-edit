// =============================================================================
// File: internal/spiceconfig/spiceconfig_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package spiceconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestDefaults pins the documented default — icons mode "auto" — so a
// future refactor of the Defaults helper can't silently flip user-
// visible behaviour for everyone who has no config file.
func TestDefaults(t *testing.T) {
	got := Defaults()
	if got.Icons != IconsAuto {
		t.Fatalf("Defaults().Icons = %q, want %q", got.Icons, IconsAuto)
	}
	if got.SidebarPathCollapsed {
		t.Fatal("Defaults().SidebarPathCollapsed = true, want false")
	}
}

// TestLoadEmptyPath verifies that calling Load with no path resolves
// to defaults rather than an error — the editor uses this when
// XDG_CONFIG_HOME is unset and the user has no home directory (CI,
// containers without HOME, etc.).
func TestLoadEmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	if cfg.Icons != IconsAuto {
		t.Fatalf("Load(\"\").Icons = %q, want %q", cfg.Icons, IconsAuto)
	}
}

// TestLoadMissingFile verifies a non-existent config file is treated
// as "no preferences set" — the common case for fresh installs.
func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "does-not-exist.json"))
	if err != nil {
		t.Fatalf("Load(missing): unexpected error: %v", err)
	}
	if cfg.Icons != IconsAuto {
		t.Fatalf("missing file should yield default IconsAuto, got %q", cfg.Icons)
	}
}

// TestLoadEmptyFile covers the "user touched the file but didn't
// write anything" edge case — should be indistinguishable from no
// file at all.
func TestLoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load(empty): %v", err)
	}
	if cfg.Icons != IconsAuto {
		t.Fatalf("empty file should yield default, got %q", cfg.Icons)
	}
}

// TestLoadHappyValues exercises every recognised icons mode exactly
// once so a typo in the switch arms shows up immediately.
func TestLoadHappyValues(t *testing.T) {
	cases := map[string]IconsMode{
		`{"icons":"auto"}`: IconsAuto,
		`{"icons":"on"}`:   IconsOn,
		`{"icons":"off"}`:  IconsOff,
		`{"icons":"AUTO"}`: IconsAuto, // case-insensitive
		`{"icons":" On "}`: IconsOn,   // whitespace-tolerant
		`{}`:               IconsAuto, // omitted field uses default
	}
	for body, want := range cases {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "config.json")
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}
			cfg, err := Load(p)
			if err != nil {
				t.Fatalf("Load(%s): %v", body, err)
			}
			if cfg.Icons != want {
				t.Fatalf("Load(%s).Icons = %q, want %q", body, cfg.Icons, want)
			}
		})
	}
}

// TestLoadSidebarPathCollapsed verifies omitted and explicit persisted path
// panel preferences resolve to their documented boolean values.
func TestLoadSidebarPathCollapsed(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "omitted", body: `{}`, want: false},
		{name: "true", body: `{"sidebar_path_collapsed":true}`, want: true},
		{name: "false", body: `{"sidebar_path_collapsed":false}`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tt.body), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load(%s): %v", tt.body, err)
			}
			if cfg.SidebarPathCollapsed != tt.want {
				t.Fatalf("Load(%s).SidebarPathCollapsed = %t, want %t", tt.body, cfg.SidebarPathCollapsed, tt.want)
			}
		})
	}
}

// TestSetSidebarPathCollapsed verifies the narrow update API creates blank
// configs and changes only its own top-level preference.
func TestSetSidebarPathCollapsed(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nested", "config.json")
		if err := SetSidebarPathCollapsed(path, true); err != nil {
			t.Fatalf("SetSidebarPathCollapsed: %v", err)
		}
		assertStoredSidebarPathCollapsed(t, path, true)
	})

	t.Run("empty file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := SetSidebarPathCollapsed(path, false); err != nil {
			t.Fatalf("SetSidebarPathCollapsed: %v", err)
		}
		assertStoredSidebarPathCollapsed(t, path, false)
	})

	t.Run("preserves unknown values", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		body := `{"icons":"on","theme":"future","unknown":{"a":1}}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := SetSidebarPathCollapsed(path, true); err != nil {
			t.Fatalf("SetSidebarPathCollapsed: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read result: %v", err)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("result must be an object: %v", err)
		}
		assertJSONValue(t, got, "icons", `"on"`)
		assertJSONValue(t, got, "theme", `"future"`)
		assertJSONValue(t, got, "unknown", `{"a":1}`)
		assertJSONValue(t, got, "sidebar_path_collapsed", `true`)
	})

	t.Run("malformed file remains unchanged", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		body := []byte(`{"icons":`)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := SetSidebarPathCollapsed(path, true); err == nil {
			t.Fatal("SetSidebarPathCollapsed(malformed) returned nil error")
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read malformed result: %v", err)
		}
		if string(got) != string(body) {
			t.Fatalf("malformed file changed: got %q, want %q", got, body)
		}
	})

	t.Run("empty path", func(t *testing.T) {
		if err := SetSidebarPathCollapsed("", true); err != nil {
			t.Fatalf("SetSidebarPathCollapsed(empty): %v", err)
		}
	})
}

// assertStoredSidebarPathCollapsed reads the persisted JSON object and checks
// the path panel preference without depending on object key ordering.
func assertStoredSidebarPathCollapsed(t *testing.T, path string, want bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("result must be an object: %v", err)
	}
	assertJSONValue(t, got, "sidebar_path_collapsed", map[bool]string{true: `true`, false: `false`}[want])
}

// assertJSONValue compares JSON semantically, preserving the type and value
// of values that should survive a narrow config update unchanged.
func assertJSONValue(t *testing.T, object map[string]json.RawMessage, key, want string) {
	t.Helper()
	got, ok := object[key]
	if !ok {
		t.Fatalf("result missing %q", key)
	}
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("parse %q: %v", key, err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("parse expected %q: %v", key, err)
	}
	gotJSON, _ := json.Marshal(gotValue)
	wantJSON, _ := json.Marshal(wantValue)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("%q = %s, want %s", key, gotJSON, wantJSON)
	}
}

// TestLoadUnknownValue verifies a typo in the icons field surfaces as
// a clear error rather than silently reverting to defaults — that's
// the bug we want users to notice and fix in their config file.
func TestLoadUnknownValue(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{"icons":"yes-please"}`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg, err := Load(p)
	if err == nil {
		t.Fatalf("expected error for unknown value, got nil")
	}
	if cfg.Icons != IconsAuto {
		t.Fatalf("on error should still return safe defaults, got %q", cfg.Icons)
	}
}

// TestLoadMalformedJSON verifies a syntactically broken config doesn't
// crash the editor — the user gets an error and the editor uses
// defaults until they fix the file.
func TestLoadMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{not json`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := Load(p); err == nil {
		t.Fatalf("expected error for malformed JSON, got nil")
	}
}

// TestLoadForwardCompat verifies the loader ignores top-level fields
// it doesn't recognise — so a future config.json with new keys keeps
// working on older binaries instead of erroring out.
func TestLoadForwardCompat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	body := `{"icons":"on","theme":"future-feature","unknown_block":{"a":1}}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("forward-compat config should not error, got %v", err)
	}
	if cfg.Icons != IconsOn {
		t.Fatalf("recognised field still expected: got %q", cfg.Icons)
	}
}

// TestDefaultPathHonoursXDG verifies XDG_CONFIG_HOME wins over the
// fallback when set — important for nix-style setups that move every
// dotfile out of $HOME.
func TestDefaultPathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	got := DefaultPath()
	want := filepath.Join("/tmp/xdg-test", "spiceedit", "config.json")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

// TestDefaultPathFallsBackToHome verifies the ~/.config fallback when
// XDG_CONFIG_HOME isn't set — the common path on macOS/Linux without
// XDG configured.
func TestDefaultPathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home-test")
	got := DefaultPath()
	want := filepath.Join("/tmp/home-test", ".config", "spiceedit", "config.json")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}
