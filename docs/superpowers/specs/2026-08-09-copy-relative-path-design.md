# Copy Relative Path Design

## Status

- Date: 2026-08-09
- Status: Approved for implementation planning

## Goal

Copy paths relative to the directory SpiceEdit opened, excluding that directory
name. For example, with `/a/b` as the opened directory, copying `/a/b/c/d`
produces `c/d`.

## Decision

`relativePathFor` resolves both its base directory and target path to absolute
paths before calling `filepath.Rel`. The directory-tree root remains the
preferred base when available. In single-file mode, where no tree exists,
`App.rootDir` is normalized in the same way.

## Behaviour

- Directory-tree and active-tab copy actions share `relativePathFor` and
  return paths below the opened root without its basename.
- Single-file mode uses the opened file's parent directory as its normalized
  base.
- A path outside the opened root may retain `..` segments; an irrecoverable
  path-resolution error falls back to the input path.

## Validation

- Add tests for tree-backed and tree-free Apps using absolute and relative
  configured roots.
- Run `gofmt` and `make test`.
