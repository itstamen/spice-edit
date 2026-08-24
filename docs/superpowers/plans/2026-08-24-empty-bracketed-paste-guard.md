# Empty Bracketed Paste Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Prevent empty or non-text bracketed-paste boundaries from reading and inserting the macOS system clipboard.

**Architecture:** Add one paste-capture boolean to App. Set it only when accepted paste text is appended; finishPaste returns before clipboard access when it remains false.

**Tech Stack:** Go, tcell, Go test.

## Global Constraints

- Empty or ignored-control paste sequences must not call the system reader or mutate any editor or modal state.
- Textual bracketed paste retains system-clipboard precedence and captured-payload fallback.
- Ordinary key handling and explicit Paste commands remain unchanged.

---

### Task 1: Guard Empty Bracketed Paste

**Files:**
- Modify: internal/app/app.go
- Modify: internal/app/eventburst_test.go

- [ ] **Step 1: Write failing tests**

Add tests using an injected readFromSystem function that fails the test if called. Send empty EventPaste start/end and a start/control-key/end sequence. Assert unchanged buffer, selection, and system-reader count. Extend the existing textual bracketed-paste test to assert the reader remains called for accepted text.

- [ ] **Step 2: Verify test failure**

Run: go test ./internal/app -run 'TestHandleEvent_BracketedPaste' -count=1

Expected: FAIL because empty paste currently invokes the system reader.

- [ ] **Step 3: Implement capture flag**

Add a private App boolean such as pasteHasText. Reset it in beginPaste. In appendPastedKey, set it only in the KeyRune, Enter or CR, LF, and Tab branches when text is appended. In finishPaste, copy the flag then reset capture state; return immediately when false before reading clipboard or routing text.

- [ ] **Step 4: Verify and commit**

Run:

    gofmt -w internal/app/app.go internal/app/eventburst_test.go
    go test ./internal/app -run 'TestHandleEvent_BracketedPaste' -count=1
    make test
    make build
    git diff --check

Commit:

    git add internal/app/app.go internal/app/eventburst_test.go
    git commit -m "Ignore empty bracketed paste events"
