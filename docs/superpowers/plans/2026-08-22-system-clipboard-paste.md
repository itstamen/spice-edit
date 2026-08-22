# System Clipboard Paste Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Make menu Paste, Esc+v, and local Command+V use the current macOS system clipboard instead of stale editor-local text.

**Architecture:** Add a clipboard read API backed by pbpaste on macOS and inject it into App. Explicit Paste commands read only that source; bracketed terminal paste prefers it and falls back to captured terminal text when unavailable.

**Tech Stack:** Go, standard library os/exec and runtime, tcell, Go test.

## Global Constraints

- Explicit menu and Esc+v Paste never fall back to stale clipBuf after a system-read failure.
- Bracketed terminal paste must prefer the system reader but retain captured-text fallback for SSH and unavailable local readers.
- Every inserted source continues through CRLF/CR normalization and one structural undo step.
- Paste is enabled for writable text tabs independent of clipBuf.
- Preserve copy/cut system publishing, modal paste routing, and non-macOS terminal-paste compatibility.

---

### Task 1: Add System Clipboard Reader

**Files:**
- Modify: internal/clipboard/clipboard.go
- Modify: internal/clipboard/clipboard_test.go

**Interfaces:**
- Produce: func ReadSystem() (string, error).
- Use pbpaste on darwin and return a concrete unsupported-platform error elsewhere.
- Keep CopyToSystem unchanged.

- [ ] **Step 1: Write failing reader tests**

Add test seams around command execution so tests prove ReadSystem returns command text without newline rewriting and propagates command failures. Add a platform-selection test that proves non-darwin does not attempt pbpaste.

- [ ] **Step 2: Verify focused test failure**

Run: go test ./internal/clipboard -run 'TestReadSystem' -count=1

Expected: FAIL because ReadSystem and its test seam do not exist.

- [ ] **Step 3: Implement reader**

Add a short doc-commented ReadSystem next to CopyToSystem. Use runtime.GOOS to allow pbpaste only on darwin. Run pbpaste without a shell, return its stdout unchanged, and wrap execution failures with a system-clipboard context. Keep the command runner private and restore it in tests.

- [ ] **Step 4: Verify reader tests**

Run: go test ./internal/clipboard -run 'TestReadSystem' -count=1

Expected: PASS.

### Task 2: Route Explicit and Terminal Paste Through System Reader

**Files:**
- Modify: internal/app/app.go
- Modify: internal/app/app_test.go
- Modify: internal/app/eventburst_test.go
- Modify: internal/app/leader_test.go

**Interfaces:**
- Consume clipboard.ReadSystem through an injected App readFromSystem function.
- Explicit Paste calls readFromSystem then insertPastedText.
- finishPaste calls readFromSystem then routePastedText, falling back only to its captured terminal text.
- hasPasteTarget becomes equivalent to hasEditableTextTab.

- [ ] **Step 1: Write failing app tests**

Inject a reader returning system text different from clipBuf. Assert menu Paste and Esc+v insert only system text. Inject a reader error and assert both paths leave the buffer unchanged, retain clipBuf, and flash a system-clipboard error. For bracketed paste, assert system content wins over captured payload, and reader failure inserts captured multiline content with normalized line endings. Assert Paste is enabled for an editable tab with empty clipBuf and disabled for read-only or no-tab states.

- [ ] **Step 2: Verify focused test failure**

Run: go test ./internal/app -run 'Test(PasteClipboard|MenuPaste|LeaderPaste|HandleEvent_BracketedPaste|HasPasteTarget)' -count=1

Expected: FAIL because menu and leader paste still use clipBuf, and finishPaste always routes captured payload.

- [ ] **Step 3: Implement system-reader routing**

Add readFromSystem to App and initialize it to clipboard.ReadSystem in both production constructors and the shared test-app constructor. Add one helper that reads and inserts system clipboard text; explicit Paste uses it and flashes a concrete error without clipBuf fallback. Change finishPaste to read system text first, use that text when successful, otherwise route the captured payload. Keep existing modal-aware routePastedText unchanged. Change hasPasteTarget to return only hasEditableTextTab and update comments so clipBuf is no longer described as the explicit paste source.

- [ ] **Step 4: Verify focused and package tests**

Run:

    go test ./internal/app -run 'Test(PasteClipboard|MenuPaste|LeaderPaste|HandleEvent_BracketedPaste|HasPasteTarget)' -count=1
    go test ./internal/app -count=1

Expected: PASS.

- [ ] **Step 5: Format, validate, and commit**

Run:

    gofmt -w internal/clipboard/clipboard.go internal/clipboard/clipboard_test.go internal/app/app.go internal/app/app_test.go internal/app/eventburst_test.go internal/app/leader_test.go
    make test
    make build
    git diff --check

Commit:

    git add internal/clipboard/clipboard.go internal/clipboard/clipboard_test.go internal/app/app.go internal/app/app_test.go internal/app/eventburst_test.go internal/app/leader_test.go
    git commit -m "Read system clipboard for paste"

### Task 3: System-Paste Review

**Files:**
- Modify: no source changes expected.

- [ ] **Step 1: Verify source precedence**

Inspect tests and implementation to confirm explicit paste never falls back to clipBuf, while terminal bracketed paste has only captured-text fallback.

- [ ] **Step 2: Run final focused validation**

Run:

    go test ./internal/clipboard ./internal/app -count=1
    git diff --check
    git status --short

Expected: all tests PASS and unrelated existing worktree files remain unstaged.
