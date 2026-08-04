# Large Text Editing Performance Design

## Document information

- **Date:** 2026-08-04
- **Status:** Approved
- **Selected approach:** Bounded event batching with syntax-highlight and soft-wrap caches.
- **Example file:** `/Users/itstamen/workspace/doc_factory/2608珊瑚海/slide_jobs.json`

## 1. Problem

Rapid navigation through a text file can make SpiceEdit flash and stop responding. Slow PageUp/PageDown input usually works, while rapid PageUp/PageDown, arrow-key navigation, or mouse-wheel scrolling can reproduce the problem with soft wrap either enabled or disabled.

The example JSON file is about 20 KiB, contains 557 lines, and has a longest line of about 230 bytes. A file of this size should not overload the editor. The behavior points to repeated synchronous work per input event rather than raw file size alone.

The current event loop redraws and flushes one complete frame after every event. Rapid input therefore builds a queue while every intermediate cursor position triggers rendering. Each changed viewport also runs Chroma again. With soft wrap enabled, movement and rendering repeatedly rebuild the visual-row layout for the complete buffer and scan it from the beginning.

## 2. Goals

- Keep rapid PageUp/PageDown, arrow-key navigation, mouse-wheel scrolling, and typing responsive.
- Preserve every input event and produce the same final cursor, selection, buffer, and viewport state as sequential processing.
- Avoid redundant frame flushes during an input burst.
- Reuse syntax highlighting while the viewport remains inside a prefetched cache window.
- Reuse soft-wrap layout until text content or editor width changes.
- Keep the event loop single-threaded and race-free.
- Preserve existing editing, undo, selection, find, modal, image, and save behavior.

## 3. Non-goals

- Do not replace the string-per-line buffer model.
- Do not add asynchronous syntax highlighting.
- Do not drop repeated keys or wheel events.
- Do not impose a maximum editable file size.
- Do not change PageUp/PageDown distance, soft-wrap semantics, or keyboard shortcuts.
- Do not redesign tcell rendering or terminal graphics presentation.

## 4. Event batching

### 4.1 Burst processing

`App.Run` will process compatible queued events in bounded bursts and flush one frame after each burst. A burst stops when any of these conditions is true:

- 64 events have been handled;
- 8 milliseconds have elapsed;
- `Screen.HasPendingEvent()` reports no queued event;
- the next event is not safe to combine with the current burst;
- the application enters or leaves a modal state;
- a resize, command, tab switch, file operation, background completion, or quit event requires a visible state boundary.

The loop preserves event order. It applies every event to application state before drawing the final frame. Ten queued PageDown events therefore move the cursor exactly ten pages while avoiding ten complete terminal flushes.

The event loop retains one deferred non-burst event when it encounters a boundary. It flushes the current state, then processes that deferred event on the next iteration. This prevents a queued confirmation key from activating a modal before the user sees it.

### 4.2 Burst-compatible events

The following events may share a frame when no modal or Esc-leader sequence is active:

- PageUp, PageDown, Up, Down, Left, Right, Home, and End;
- printable text input, Enter, Tab, Backspace, and Delete;
- vertical and horizontal mouse-wheel events;
- mouse-motion events that only update hover state.

Mouse clicks, drag starts, leader commands, modal input, file actions, tab changes, resize events, and asynchronous completion events end the current burst.

### 4.3 Starvation protection

The event-count and time limits guarantee regular frame delivery during continuous input. The count limit gives deterministic behavior in tests. The time limit bounds latency when individual events become expensive.

## 5. Syntax-highlight window cache

### 5.1 Relative cache

The render path will replace the absolute `Styles` grid with a relative window:

- `highlightStart`: first cached logical line;
- `highlightRows`: style rows relative to `highlightStart`;
- `highlightEnd`: one past the last cached logical line;
- the theme used to build the cache.

The renderer resolves a line with `highlightRows[lineIdx-highlightStart]`. This avoids allocating an outer slice whose length equals the complete file on every cache miss.

### 5.2 Prefetch policy

On a cache miss, the requested window contains the visible logical lines plus at least 256 logical lines above and below. Chroma receives its existing bounded lexical lead outside that requested cache window so multi-line strings and comments retain context.

The cache is reused while the complete visible logical range remains inside `[highlightStart, highlightEnd)`. Scrolling within that range performs no tokenisation. The example 557-line JSON file should normally fit in one initial cache window.

### 5.3 Invalidation

The highlight cache is invalidated when:

- text content changes;
- a file reload or revert replaces the buffer;
- undo or redo restores a snapshot;
- the file path or detected lexer changes;
- the render theme differs from the cached theme.

Changing only the cursor, selection, find match, horizontal scroll, or viewport position inside the cached range does not invalidate syntax styles.

## 6. Soft-wrap layout cache

### 6.1 Cached layout

Each text tab will own one cached wrap layout:

- editor content width;
- text-content revision;
- ordered `wrapRow` entries;
- a `lineStarts` index that maps each logical line to its first visual row.

The cache key is `(content revision, content width)`. `wrapRows`, cursor movement, hit testing, wheel scrolling, visibility clamping, and rendering all consume the same cached layout.

### 6.2 Indexed cursor lookup

`visualRowForPosition` currently scans visual rows from the beginning of the file. The new `lineStarts` index restricts the search to the wrapped segments of the cursor's logical line. Position lookup becomes independent of how far down the file the cursor has moved.

### 6.3 Content revision

A tab increments its text-content revision after every successful content mutation:

- insert, paste, Enter, Backspace, Delete, and selection deletion;
- line-comment toggle;
- undo, redo, and revert;
- disk reload.

Pure navigation and selection changes do not increment the revision. Width changes select a new layout key without changing content revision.

The existing dirty flag and undo grouping remain separate from cache revision. Cache invalidation must not alter save state or undo history.

## 7. Rendering behavior

`flushFrame` remains the only path that draws and presents a complete frame. Image graphics keep their existing show-before-present ordering. Event batching reduces how often `flushFrame` runs but does not change one frame's drawing order.

The screen still receives an immediate frame when there are no queued compatible events. Normal single-key navigation therefore keeps its current feedback latency.

## 8. Error handling and fallback

- Empty and read-only tabs return empty caches without panicking.
- Invalid widths clamp to one cell, matching current soft-wrap behavior.
- A cache miss always rebuilds synchronously before rendering; the editor never displays stale text or styles.
- A resize ends the current event burst and rebuilds width-dependent layout on the next frame.
- Cache construction failures are not expected because the structures only allocate Go slices. Existing Chroma tokenisation errors continue to fall back to base and Markdown structural styles.
- No goroutine writes editor, highlight, or wrap state.

## 9. Testing

### Event-loop tests

- Queue 100 PageDown events and verify the final cursor equals sequential handling.
- Verify the same burst produces only a bounded number of frame flushes.
- Cover PageUp, arrow keys, shifted selection, typing, Backspace/Delete, and mouse wheels.
- Verify a modal-opening event creates a frame boundary before a later confirmation key.
- Verify resize, background completion, and quit events are not delayed behind an unbounded burst.

### Highlight-cache tests

- Rendering and scrolling inside the cached range reuse the same style window.
- Crossing the cache boundary rebuilds the window.
- Edits, undo/redo, revert, reload, path changes, and theme changes invalidate it.
- A generated large file does not allocate an outer style slice proportional to total line count.

### Soft-wrap cache tests

- Repeated movement, scrolling, hit testing, and rendering at one width reuse the layout.
- Editing and snapshot restoration increment content revision and rebuild the layout.
- Width changes rebuild the layout without changing text revision.
- Indexed logical-to-visual lookup matches the previous linear behavior for ASCII, tabs, CJK text, combining characters, and long lines.

### Performance checks

- Add benchmarks for a representative JSON document, a many-line document, and long wrapped lines.
- Record allocations for cached and uncached highlight and wrap operations.
- Manually open the supplied `slide_jobs.json` and rapidly exercise PageUp/PageDown, arrow keys, wheel scrolling, selection, and typing with soft wrap both off and on.
- Run `make test`, `make build`, `make build-linux`, and Windows cross-compilation.

## 10. Acceptance criteria

- Rapid navigation no longer makes the editor stop responding or visibly replay every intermediate page.
- All input events are preserved and final editor state matches sequential processing.
- Scrolling inside a highlight window performs no Chroma tokenisation.
- Soft-wrap scrolling and cursor movement reuse one width-and-revision layout.
- Cache invalidation never displays stale content, stale highlighting, or incorrect cursor placement.
- Race-enabled tests and supported-platform builds pass.

## 11. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Batching hides a modal before a queued key activates it. | Stop before non-burst events and flush on modal-state transitions. |
| Continuous input prevents rendering. | Enforce both 64-event and 8-millisecond limits. |
| Cached styles become stale after an uncommon mutation path. | Route all content replacements through one cache-invalidation helper and test every mutation family. |
| A full wrap cache retains too much memory for extremely large files. | Retain only one layout per tab and replace it on width or revision changes. Benchmark allocation size. |
| Multi-line syntax loses lexer context at cache boundaries. | Keep the existing bounded lexical lead outside the cached style window. |
