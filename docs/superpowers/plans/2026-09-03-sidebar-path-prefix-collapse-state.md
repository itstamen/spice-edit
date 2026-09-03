# Sidebar Path Prefix And Collapse State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make pasted path prefixes match visibly identical paths and add a compact, persisted expand/collapse control to the PATH INFO panel.

**Architecture:** Give PATH INFO its own paste normalization so trailing clipboard line endings do not become invisible spaces. Store the collapsed state as a backward-compatible field in the existing user config while preserving unknown JSON keys. Derive both tree and panel rectangles from one dynamic panel-height helper, and treat a header press as a complete mouse gesture so geometry cannot change under a held click.

**Tech Stack:** Go, tcell simulation screens, JSON config, injected filesystem paths, existing clipboard and terminal-text helpers.

## Global Constraints

- `docs/` must strip the visible `docs/` prefix from `docs/superpowers/...`.
- PATH INFO paste removes only trailing CR/LF line endings before converting remaining internal line breaks to spaces; it must not trim valid leading/trailing spaces from path names.
- PATH INFO is expanded by default and uses `▲` while expanded (the action collapses upward) and `▼` while collapsed (the action expands downward).
- Expanded height remains eight rows; collapsed height is exactly one header row and releases seven rows to the directory tree.
- Clicking anywhere on the PATH INFO header toggles it once per physical press, blurs the prefix input, and persists the new state.
- The collapsed header absorbs right-click and wheel input and never becomes a tree hit target.
- Persist state as `sidebar_path_collapsed` in the existing `config.json`; missing fields default to `false` and unknown JSON fields must survive updates.
- A malformed existing config must not be overwritten; the in-memory toggle still succeeds and displays a `config:` error.
- Single-file mode and a hidden sidebar render no PATH INFO panel and perform no persistence writes.
- Preserve the final-cursor precedence established by commit `4b50808`.

---

### Task 1: Prefix Paste Normalization

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/sidebarpath_test.go`

**Interfaces:**
- Produces: `normalizeSidebarPathPrefixPaste(string) string`.
- Consumes: existing `normalizePastedText`, `insertPastedRunes`, and `copyCurrentSidebarPath`.

- [x] **Step 1: Add failing paste regressions**

Add tests that focus the prefix input for a selected `docs/superpowers/spec.md` node, route authorized pasted text, and assert:

```go
normalizeSidebarPathPrefixPaste("docs/\r\n") == "docs/"
normalizeSidebarPathPrefixPaste("dir with spaces/\n") == "dir with spaces/"
normalizeSidebarPathPrefixPaste("a\nb\n") == "a b"
```

For the first case, also assert the stored rune slice is exactly `docs/`, `currentSidebarPath()` returns `superpowers/spec.md` with `valid=true`, and the injected system clipboard receives that suffix once.

- [x] **Step 2: Verify the regression fails**

Run: `go test ./internal/app -run 'TestSidebarPathPrefixPaste' -count=1`

Expected: FAIL because the current common normalizer produces `docs/ `.

- [x] **Step 3: Implement PATH INFO-specific normalization**

Implement exactly this ordering:

```go
func normalizeSidebarPathPrefixPaste(text string) string {
    text = normalizePastedText(text)
    text = strings.TrimRight(text, "\n")
    return strings.ReplaceAll(text, "\n", " ")
}
```

Use it only in the `sidebarPathPrefixFocused` branch of `routePastedText`; leave prompt, form, find, and finder behavior unchanged.

- [x] **Step 4: Format, test, and commit**

Run:

```bash
gofmt -w internal/app/app.go internal/app/sidebarpath_test.go
go test ./internal/app -run 'TestSidebarPathPrefixPaste' -count=1
go test ./internal/app -count=1
git diff --check
```

Expected: PASS.

```bash
git add internal/app/app.go internal/app/sidebarpath_test.go
git commit -m "Fix pasted sidebar path prefixes"
```

---

### Task 2: Persisted Collapse Preference

**Files:**
- Modify: `internal/spiceconfig/spiceconfig.go`
- Modify: `internal/spiceconfig/spiceconfig_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Produces: `Config.SidebarPathCollapsed bool` and `SetSidebarPathCollapsed(path string, collapsed bool) error`.
- Consumes: `spiceconfig.DefaultPath()` and `App.loadSpiceConfig()`.

- [x] **Step 1: Add failing config tests**

Assert `Defaults().SidebarPathCollapsed == false`; omitted fields load false; both JSON booleans load exactly; missing/empty files can be updated; and this input:

```json
{"icons":"on","theme":"future","unknown":{"a":1}}
```

becomes a valid object containing the same semantic values plus `"sidebar_path_collapsed":true`. Assert updating malformed JSON returns an error and leaves its bytes unchanged. Assert an empty path is a successful no-op.

- [x] **Step 2: Verify config tests fail**

Run: `go test ./internal/spiceconfig -run 'Test(Default|Load.*Sidebar|SetSidebarPathCollapsed)' -count=1`

Expected: FAIL because the field and update API do not exist.

- [x] **Step 3: Extend load schema and add narrow update API**

Add `SidebarPathCollapsed bool` to `Config`, default false, and a pointer boolean to `fileFormat` so omission remains distinguishable. Promote a non-nil value during `Load` after the icons validation.

Implement `SetSidebarPathCollapsed` by reading the existing object into `map[string]json.RawMessage`, treating missing/empty/whitespace-only content as `{}`, setting only `sidebar_path_collapsed`, `json.MarshalIndent` plus newline, creating the parent directory with `0755`, and writing with `0600`. Return parse/read/mkdir/marshal/write errors with path context; never overwrite malformed input; return nil immediately for an empty path.

- [x] **Step 4: Load preference through App state**

Add `spiceConfigPath string` and `sidebarPathCollapsed bool` to `App`. Production constructors initialize `spiceConfigPath` from `spiceconfig.DefaultPath`; `loadSpiceConfig` reads that path and sets the collapsed state. Tests keep the path empty unless explicitly exercising persistence, preventing writes to the developer's real config.

Add an App test that writes `sidebar_path_collapsed:true`, calls `loadSpiceConfig`, and observes the field; also assert malformed config leaves the zero-value expanded state and produces a `config:` status.

- [x] **Step 5: Format, test, and commit**

Run:

```bash
gofmt -w internal/spiceconfig/spiceconfig.go internal/spiceconfig/spiceconfig_test.go internal/app/app.go internal/app/app_test.go
go test ./internal/spiceconfig ./internal/app -count=1
git diff --check
```

Expected: PASS.

```bash
git add internal/spiceconfig/spiceconfig.go internal/spiceconfig/spiceconfig_test.go internal/app/app.go internal/app/app_test.go
git commit -m "Persist sidebar path panel state"
```

---

### Task 3: Collapsible Panel Interaction And Documentation

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/sidebarpath.go`
- Modify: `internal/app/sidebarpath_test.go`
- Modify: `README.md`
- Modify: `docs/superpowers/plans/2026-09-03-sidebar-path-prefix-collapse-state.md`

**Interfaces:**
- Consumes: Task 2's `sidebarPathCollapsed`, `spiceConfigPath`, and `SetSidebarPathCollapsed`.
- Produces: `sidebarPathHeight() int`, one-row collapsed rendering, and persisted header-toggle behavior.

- [x] **Step 1: Add failing layout, drawing, and event tests**

Cover expanded/default and collapsed rectangles, proving the tree gains exactly seven rows. Assert expanded header contains `PATH INFO ▲`, collapsed header contains `PATH INFO ▼`, and collapsed body labels are absent. Assert clicking the header through `handleMouse` toggles once, sets `dragMode="path"`, blurs prefix focus, persists once, consumes held Button1 motion after the geometry changes, clears on release, and permits a later independent toggle.

Also assert collapsed header right-click and vertical/horizontal wheels do not open tree context or scroll tree/editor; collapsed input/radio hit-testing is unreachable; collapsing restores the editor cursor; and hidden/sidebar-less/tiny layouts still return zero rectangles and never persist.

- [x] **Step 2: Verify interaction tests fail**

Run: `go test ./internal/app -run 'TestSidebarPath.*(Collapse|Collapsed|Header|Persist)' -count=1`

Expected: FAIL because the panel still has a fixed eight-row height and no toggle.

- [x] **Step 3: Implement dynamic geometry and header rendering**

Add constants for expanded height eight and collapsed height one. Make `sidebarPathHeight()` return zero when the panel is unavailable, otherwise one/eight based on state. Both `sidebarRect` and `sidebarPathRect` must use the same returned height. `sidebarPathInputRect` returns zero while collapsed.

Render the header in both states and return immediately after it when collapsed. Use `▲` for expanded and `▼` for collapsed. Keep the post-editor PATH INFO draw order so the prefix cursor remains correct when expanded.

- [x] **Step 4: Implement one-toggle-per-press persistence**

When a left press lands on the header, flip `sidebarPathCollapsed`, blur prefix focus, reset prefix scroll as needed, and call `spiceconfig.SetSidebarPathCollapsed(a.spiceConfigPath, a.sidebarPathCollapsed)`. Flash `config: <error>` on failure without reverting the in-memory state. In `handleMouse`, set `dragMode="path"` for every consumed panel press and absorb subsequent held Button1 events until release.

Panel/header containment must be checked before tree routing after every geometry change. Preserve the existing right-click blur and automatic path-copy behavior; toggling itself must not copy a path.

- [x] **Step 5: Document and fully validate**

Update README's PATH INFO paragraph to say the `▲`/`▼` header control collapses/expands the panel, it starts expanded, and the choice is remembered in `config.json`.

Run:

```bash
gofmt -w internal/app/app.go internal/app/sidebarpath.go internal/app/sidebarpath_test.go internal/spiceconfig/spiceconfig.go internal/spiceconfig/spiceconfig_test.go
go test -race -count=1 ./...
make build
git diff --check
```

Expected: all pass and `bin/spiceedit` is rebuilt but remains ignored.

- [x] **Step 6: Commit Task 3**

```bash
git add internal/app/app.go internal/app/sidebarpath.go internal/app/sidebarpath_test.go README.md docs/superpowers/plans/2026-09-03-sidebar-path-prefix-collapse-state.md
git commit -m "Add collapsible sidebar path panel"
```
