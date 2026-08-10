# Copy Relative Path Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Copy paths relative to the SpiceEdit opened directory, without including that directory name.

**Architecture:** Keep `relativePathFor` as the shared active-tab and tree-context helper. Normalize its base and target with `filepath.Abs`, then use `filepath.Rel`.

**Tech Stack:** Go 1.24 standard-library `path/filepath`; same-package Go tests.

## Global Constraints

- Prefer `tree.Root.Path` when a directory tree exists.
- Support tree-free single-file mode through `App.rootDir`.
- Preserve the input only when path resolution or relativization fails.
- Do not change absolute-path copy actions.
- Run `gofmt` and `make test` before committing.

---

## File Structure

| Path | Responsibility |
|---|---|
| `internal/app/fileops.go` | Normalize relative-copy inputs before `filepath.Rel`. |
| `internal/app/fileops_test.go` | Cover relative configured roots without a file tree. |

### Task 1: Normalize Relative Copy Paths

**Files:**
- Modify: `internal/app/fileops.go`
- Modify: `internal/app/fileops_test.go`

**Consumes:** `App.rootDir`, `filetree.Tree.Root.Path`, `filepath.Abs`, and `filepath.Rel`.

**Produces:** `(*App).relativePathFor(path string) string` always produces `c/d` for `/a/b/c/d` relative to opened root `/a/b`.

- [x] **Step 1: Write the failing tree-free regression test**

Add after `TestRelativePathFor_RelativeRootDir`:

```go
func TestRelativePathFor_TreeFreeRelativeRoot(t *testing.T) {
	root := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil { t.Fatal(err) }
	if err := os.Chdir(filepath.Dir(root)); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	workingDir, err := os.Getwd()
	if err != nil { t.Fatal(err) }
	a := &App{rootDir: "."}
	target := filepath.Join(workingDir, "c", "d")
	if got, want := a.relativePathFor(target), filepath.Join("c", "d"); got != want {
		t.Fatalf("relativePathFor = %q, want %q", got, want)
	}
}
```

- [x] **Step 2: Verify the test fails before the fix**

Run: `go test ./internal/app -run TestRelativePathFor_TreeFreeRelativeRoot -count=1`

Expected: FAIL because `filepath.Rel` receives a relative base and absolute target, then returns the absolute fallback.

- [x] **Step 3: Normalize both `relativePathFor` operands**

Replace the method body with:

```go
func (a *App) relativePathFor(path string) string {
	base := a.rootDir
	if a.tree != nil && a.tree.Root != nil { base = a.tree.Root.Path }
	absBase, err := filepath.Abs(base)
	if err != nil { return path }
	absPath, err := filepath.Abs(path)
	if err != nil { return path }
	rel, err := filepath.Rel(absBase, absPath)
	if err != nil { return path }
	return rel
}
```

- [x] **Step 4: Format and run focused regressions**

Run: `gofmt -w internal/app/fileops.go internal/app/fileops_test.go && go test ./internal/app -run 'TestRelativePathFor_(InsideRoot|RelativeRootDir|TreeFreeRelativeRoot)' -count=1`

Expected: PASS.

- [x] **Step 5: Run complete validation and commit**

Run: `git diff --check && make test && git add internal/app/fileops.go internal/app/fileops_test.go && git commit -m "Normalize copied relative paths"`

Expected: race-enabled tests pass and only the implementation/tests are committed.

## Plan Review

- Spec coverage: Task 1 normalizes both operands, retains tree-root preference, and covers tree-free relative roots.
- Placeholder scan: each implementation and validation step is concrete.
- Type consistency: uses existing `App`, `rootDir`, `tree`, `filepath.Abs`, and `filepath.Rel` APIs.
