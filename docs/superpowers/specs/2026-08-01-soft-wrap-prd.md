# Soft Wrap PRD

## Document information

- **Authors:** SpiceEdit maintainers
- **Reviewers:** SpiceEdit maintainer
- **Date:** 2026-08-01
- **Status:** Proposed

| Version | Date | Change |
|---|---|---|
| 0.1 | 2026-08-01 | Initial PRD for editor soft wrap, default-off behavior, and `Esc z` toggle. |
| 0.2 | 2026-08-01 | Scope V1 to readable soft wrap, menu/leader toggle, and basic cursor visibility; defer full visual-row navigation. |
| 0.3 | 2026-08-01 | Add Markdown display enhancement for headings and fenced code blocks, inspired by Glow-style terminal Markdown rendering. |
| 0.4 | 2026-08-01 | Bring visual-row navigation, page scrolling, wheel scrolling, selection extension, and continuation-row mouse hit testing into soft-wrap scope. |
| 0.5 | 2026-08-02 | Add OS-default application handoff for unsupported files and an `Esc o` / main-menu action for opening any file-backed tab externally on macOS, Windows, and Linux. |

## 1. Executive summary

SpiceEdit will add an optional soft-wrap mode for text tabs so long lines can be viewed within the editor pane without horizontal scrolling. Soft wrap is off by default to preserve the current editing model and terminal-editor behavior. Users can toggle it with `Esc z` or the main `≡` action menu; the setting applies to the current editor session and affects text rendering, visual-row navigation, scrolling, selection extension, and mouse placement, but not file contents.

For Markdown files, SpiceEdit will also enhance display styling so common document structure is easier to scan while editing. Headings that begin with `#` use distinct colors by level, and fenced code-block markers such as ```` ```go ```` use a different style from normal prose. The goal is a lightweight Glow-inspired terminal reading feel inside the existing editor, not a separate Markdown preview mode.

SpiceEdit will also distinguish files it can safely edit or preview from files that belong in a desktop application. Supported text files remain editable in the terminal, and supported images remain viewable in the editor. Selecting any other file opens it with the operating system's default application and activates a read-only placeholder tab that says `编辑器只支持文本及图片文件在线编辑.`. Users can also press `Esc o` or use the main `≡` menu to open the current text, image, or unsupported file with the default application. The behavior must work on macOS, native Windows, and Linux without blocking the editor.

## 2. Problem statement

### Who has this problem?

- **Primary users:** developers editing or reading files with long logical lines, such as Markdown paragraphs, logs, JSON, generated output, and prose.
- **Mixed-project users:** developers browsing repositories that also contain PDFs, office documents, archives, audio/video, and other files that SpiceEdit should not interpret as editable text.
- **Secondary users:** developers working over SSH or inside tmux/zellij where mouse gestures and some terminal shortcuts are unreliable, and where every action must remain reachable from the `≡` menu.

### What is the problem?

SpiceEdit currently renders long lines as single horizontal rows. Users must scroll horizontally to inspect content beyond the viewport. That is appropriate for code in many cases, but it is painful for prose, logs, and structured data where reading continuity matters more than preserving one logical line per screen row.

Markdown files have an additional readability issue: document structure is visually flat compared with terminal Markdown readers. Headings, fenced-code markers, and code-block language labels do not stand out enough for users scanning README files, PRDs, notes, or documentation.

File opening is also too permissive. The current tab constructor treats every non-image file as text, so clicking a PDF, office document, archive, executable, or other binary file can load arbitrary bytes into an editable buffer. Users also lack a built-in way to hand a supported text or image file to the operating system's default desktop application.

### Why is it painful?

- Long-line reading requires repeated horizontal scrolling and makes nearby context hard to scan.
- The cursor can be visible while the relevant right-side content is off-screen.
- Users who expect a terminal editor to offer a wrap toggle have no discoverable way to trade horizontal scrolling for wrapped visual rows.
- Markdown authors must visually parse raw punctuation such as `#`, ```` ``` ```` and language tags rather than seeing stronger structural cues.
- Unsupported files can display unreadable binary data and may be accidentally modified or saved as if they were text.
- Users must leave SpiceEdit and manually locate a file before opening it in Preview, a browser, an office suite, a media player, or another associated application.

### Evidence

- `internal/editor/tab.go` tracks `ScrollX` and renders one logical buffer line per screen row today.
- `README.md` documents the `Esc` leader model and the requirement that hotkey actions also live in the `≡` menu.
- Glow demonstrates the user expectation that terminal Markdown can be styled for reading, with themes, word wrapping, and document-oriented formatting. SpiceEdit should borrow the readability direction without becoming a separate Markdown reader.
- `internal/editor.NewTab` currently branches only for recognised image extensions; every other path is read with `os.ReadFile` and converted directly into an editable text buffer.
- `internal/app.openFile` is shared by the file tree, finder, and command-line open paths, but there is no platform-aware default-application opener or `Esc o` leader binding.
- 🔵 **Open Question:** no user analytics or support-ticket volume was supplied for long-line pain; this PRD is based on a direct feature request.

## 3. Target users and jobs-to-be-done

### Primary persona: terminal-native developer

- **Context:** edits code, Markdown, config, logs, and generated files in a terminal.
- **Goal:** choose whether long lines wrap, scan Markdown structure quickly, and safely browse mixed file types without treating binary content as editable text.
- **Job to be done:** when I am reading or editing a long-line Markdown file, let me turn on soft wrap and visually distinguish headings and fenced code blocks without modifying the file.
- **Job to be done:** when I select a file SpiceEdit cannot edit or preview, open it in the operating system's associated application while keeping my project context visible in the editor.

### Secondary persona: SSH/tmux user

- **Context:** uses SpiceEdit inside terminal environments where `Ctrl+` shortcuts or mouse interactions may be intercepted.
- **Goal:** access wrap and external-open actions through the established `Esc` leader and the main action menu.
- **Job to be done:** when I need to change wrapping or open the current file with its associated application, give me a keyboard path and a menu path that work consistently in terminal sessions.

🔶 **Assumption:** "local default application" means the default application on the host where the SpiceEdit process is running. In an SSH session, the opener runs on the remote host; forwarding a remote file to the SSH client's desktop is outside this release.

## 4. Strategic context

### Why now?

Soft wrap is a high-leverage readability feature that fits SpiceEdit's terminal-native workflow. The existing renderer already owns visual columns, CJK width handling, horizontal scroll, mouse hit testing, and viewport calculations, giving the feature a clear implementation surface.

Markdown display enhancement is a natural companion because the most common soft-wrap beneficiaries are README files, docs, notes, and PRDs. SpiceEdit already uses Chroma for syntax highlighting, so the first release should extend editor styling for Markdown-specific structure before considering heavier rendering libraries.

Default-application handoff is the next step in making the file tree safe for real project directories. SpiceEdit already has central file-opening paths, image-tab modes, read-only rendering patterns, and asynchronous process execution for custom actions. These provide a clear surface for adding file classification and a small cross-platform opener without adding CGO or a runtime dependency.

### Strategic alignment

This work supports SpiceEdit's goal of being a practical terminal editor for real project files, including files that are not code-shaped. It also preserves the project's shortcut philosophy: no `Ctrl+` shortcuts, and every shortcut action must be discoverable from the main `≡` menu.

🔵 **Open Question:** no release milestone, business metric, or adoption target was supplied. This PRD uses functional acceptance criteria and regression tests as success gates.

## 5. Solution overview

### Solution description

Add a soft-wrap state to the editor session, defaulting to disabled. When disabled, rendering and navigation behave exactly as they do today: one logical buffer line maps to one visible editor row, and long content uses `ScrollX`.

When enabled, each logical buffer line maps to one or more visual rows based on the current editor content width. Wrapped rows are display-only. The underlying buffer remains unchanged: no newline is inserted, file saves are byte-for-byte consistent with user edits, undo history records only real edits, and cursor positions remain logical line/rune columns.

```text
buffer logical lines
        |
        v
soft-wrap layout for current viewport width
        |
        +--> visual rows for rendering
        +--> cursor screen position
        +--> cursor segment visibility
```

The first implementation should add a small unexported wrap-layout helper inside `internal/editor`, not a new package. The helper should expose enough structure for `Tab.Render` and cursor visibility to share one grapheme/tab/cell-width calculation:

```text
logical line runes + content width
        |
        v
wrapLine(...)
        |
        +--> []wrapSegment{line, startRune, endRune, startVisual, width}
```

The same layout model should also drive soft-wrap cursor visibility, vertical movement, page movement, wheel scrolling, selection drag hit testing, and continuation-row mouse placement so rendered rows and input behavior cannot drift apart.

### Markdown display enhancement

Markdown files (`.md`, `.markdown`, and common README/license Markdown names) receive a lightweight structural style layer in the existing text editor renderer:

- ATX headings that begin with one to six `#` characters use level-aware colors. `#` / `##` should be more prominent than deeper levels while still fitting the existing theme.
- Fenced code-block delimiter lines beginning with ```` ``` ```` or `~~~` use a distinct muted/accent style so code blocks are easy to spot.
- A fenced code-block language label such as ```` ```go ```` or ```` ```ts ```` uses an accent style distinct from the delimiter punctuation.
- Text inside fenced code blocks continues to be rendered as editable text. 🔶 **Assumption:** V1 may style code-block body text with the Markdown lexer or existing base style; nested language-specific highlighting inside fenced blocks can be deferred unless Chroma already provides it cleanly.
- The enhancement is display-only. It does not hide Markdown punctuation, render proportional layout, insert blank rows, center headings, draw boxes, or alter file contents.

```text
markdown logical lines
        |
        v
existing Chroma style grid
        |
        v
markdown structural style overlay
        |
        +--> heading line styles
        +--> fence delimiter styles
        +--> fence language-label style
        |
        v
Tab.Render / soft-wrap segments
```

Glow is the visual reference for terminal-friendly Markdown readability, but SpiceEdit should not shell out to `glow` or embed a read-only Glow preview. The editor remains a raw Markdown editor with richer styling.

### Local default application handoff

Every file-opening entry point uses one shared classification policy before file contents enter an editable buffer:

1. **Supported image:** a format supported by the existing inline image decoder; V1 must include `.png`, `.jpg`, `.jpeg`, and `.gif`, case-insensitively. The file opens in the existing read-only image preview.
2. **Supported text:** a known text/source/config/document extension or filename, or an unknown extension whose bounded content sample is valid UTF-8 and contains no NUL byte or other strong binary signal. At minimum this includes Markdown, plain text, JSON, XML, YAML, TOML, CSV, logs, and source/config files already handled by SpiceEdit. Empty files and paths created through the New File workflow remain text files.
3. **Unsupported:** anything that is neither a decodable supported image nor safe text. This includes PDFs, office documents, archives, executables, audio/video, database files, and binary content disguised with a text extension.

The classifier must inspect only a bounded sample when content sniffing is needed. A strong binary signal overrides a text-looking extension so SpiceEdit never loads arbitrary binary bytes into an editable buffer. An image-looking file that fails inline decoding falls back to unsupported-file handling rather than becoming text.

When an unsupported file is selected from the left file tree, finder, command-line path, or another normal file-open entry point:

- SpiceEdit activates or reuses a read-only placeholder tab for that absolute path.
- The editor pane displays the exact message `编辑器只支持文本及图片文件在线编辑.` in a clearly visible position.
- The tab retains the filename and path, participates in normal tab switching/closing, and keeps the file-tree active-file highlight.
- Text editing, save, undo/redo, find, formatting, line comments, and soft wrap are unavailable for the placeholder tab.
- SpiceEdit asynchronously requests that the operating system open the path with its default associated application.
- A repeated explicit selection or `Esc o` invocation may launch the associated application again, but it must not create duplicate tabs for the same path.
- If the external launch fails, the placeholder remains visible and the status bar reports a concise error; the file is never reinterpreted as text.

Supported text and image files continue to open inline and do not automatically launch an external application. For any active file-backed tab, `Esc o` and a main-menu row labeled `Open with default app` invoke the same external-open action. The action opens the on-disk path and does not auto-save or export unsaved editor-buffer changes. It is disabled in the menu when there is no active file path; invoking the shortcut without an open file produces a concise status message.

The platform adapter is selected from the runtime operating system and starts the opener without blocking the TUI:

| Runtime OS | Default application handoff |
|---|---|
| macOS (`darwin`) | `open <absolute-path>` |
| Windows (`windows`) | `cmd.exe /c start "" <absolute-path>` or an equivalent native association API |
| Linux (`linux`) | `xdg-open <absolute-path>`, with `gio open <absolute-path>` as an optional fallback when `xdg-open` is unavailable |

Paths are passed as process arguments rather than interpolated into a user-controlled shell command. Spaces, Unicode, quotes, and shell metacharacters in filenames must not change command meaning. Unsupported operating systems, missing opener commands, missing files, permission errors, and headless Linux sessions report failure in the status bar and leave SpiceEdit responsive.

### Toggle behavior

- Soft wrap is off by default for every new SpiceEdit session.
- `Esc z` toggles soft wrap on and off.
- The main `≡` action menu includes a dynamic row:
  - `Enable soft wrap` with shortcut `Esc z` when wrap is off.
  - `Disable soft wrap` with shortcut `Esc z` when wrap is on.
- Toggling wrap displays a short status message, for example `Soft wrap enabled` or `Soft wrap disabled`.
- The toggle applies to all text tabs in the current App session. 🔶 **Assumption:** session-wide behavior is the first release because it is simpler and matches display preferences such as file explorer visibility better than per-file persistence.
- Enabling soft wrap clears horizontal scroll (`ScrollX`) for text tabs. While soft wrap is enabled, horizontal scrolling is ignored; disabling soft wrap resumes the normal horizontal-scroll model from column zero.

### Rendering behavior

- Wrapped visual rows use the editor content width after gutter and overflow-marker reservations.
- Wrap decisions respect terminal-cell width and grapheme boundaries. CJK wide characters and combining sequences must never split across visual rows.
- Tabs keep the existing four-cell tab-stop behavior when calculating wrap width.
- Syntax highlighting, selection highlighting, find highlighting, and active-line styling continue to apply to the correct logical text ranges on every wrapped row.
- Horizontal overflow markers are not shown for text that continues only because of soft wrapping. A marker may still be used for an individual grapheme or tab expansion that cannot fit in the available content width. 🔶 **Assumption:** extremely narrow viewports should prioritize not corrupting display over perfect wrap aesthetics.
- Soft-wrap mode keeps a visual-row scroll offset separate from the existing logical-line `ScrollY`. Rendering starts at the first visible wrapped row, while the logical buffer positions remain unchanged.

### Navigation and editing behavior

- Left/right, insertion, deletion, selection, undo, redo, save, find, and line-comment operations continue to operate on logical buffer lines and columns.
- Up/down and Page up/down move by wrapped visual rows when soft wrap is enabled.
- Vertical wheel scrolling moves the soft-wrap visual viewport. Horizontal wheel scrolling remains ignored for text tabs while soft wrap is enabled.
- Mouse hit testing maps wrapped continuation rows back to the correct logical line and rune column, including CJK wide characters, combining sequences, and tab boundaries.
- Drag selection and auto-scroll extend selections through the same soft-wrap hit-testing model.
- Find-next and find-previous continue to move the logical cursor to the active match; the soft-wrap viewport must reveal the wrapped segment containing that cursor when possible.
- Toggling soft wrap recalculates viewport state so the logical cursor line and the wrapped segment containing the cursor are visible when possible.

### Persistence

The first release does not persist soft-wrap preference to config. 🔶 **Assumption:** default-off, session-only behavior reduces surprise and avoids adding a config migration before users validate the interaction.

## 6. Success metrics

### Primary metric

**Release acceptance pass rate** — the percentage of defined soft-wrap, Markdown styling, file-classification, external-opening, and regression scenarios that pass before release.

- **Current:** 🔵 **Open Question:** soft-wrap coverage exists, but no aggregate baseline has been recorded for the expanded PRD and external-opening behavior does not exist yet.
- **Target:** 100% of required automated tests and manual smoke checks pass.
- **Timeline:** release-candidate validation.

### Secondary metrics

- Existing no-wrap rendering, horizontal scroll, CJK width, selection, find, and image-tab tests remain passing.
- `Esc z` and the `≡` menu toggle exercise the same code path.
- No saved file changes occur from toggling soft wrap alone.
- Wrapped rendering tests include cell-level `tcell.NewSimulationScreen("UTF-8")` assertions for CJK wide characters, combining sequences, and tab-stop boundaries.
- Markdown rendering tests verify heading-level colors and fenced-code delimiter/language-label styles without changing the underlying buffer text.
- File-classification tests cover known text, unknown-but-valid UTF-8 text, binary content with a misleading extension, supported images, corrupt images, empty files, and missing New File paths.
- Platform-opener tests verify macOS, Windows, and Linux command selection through an injectable command runner without launching real desktop applications during tests.
- File-tree, finder, command-line, leader, and menu tests verify that unsupported files use one placeholder tab and that `Esc o` opens the active file path asynchronously.

### Guardrails

- Soft wrap is off by default.
- No `Ctrl+` shortcut is introduced.
- Image tabs remain unaffected; soft wrap applies only to text tabs.
- The feature must not degrade CJK or combining-character rendering.
- Markdown display enhancement must remain edit-safe: raw Markdown text stays visible, selectable, searchable, and saved unchanged.
- Unsupported files must never enter an editable buffer or become savable from their placeholder tab.
- External opening must not auto-save dirty text buffers, block the TUI event loop, or introduce a `Ctrl+` shortcut.
- External-open failures must not close the current tab, lose editor state, or crash the process.

## 7. User stories and requirements

### Epic hypothesis

We believe that adding a default-off soft-wrap toggle will improve long-line readability for terminal-native developers without disrupting existing code-editing workflows. We will validate this through rendering/navigation acceptance tests and no-regression coverage for the current horizontal-scroll behavior.

We also believe that classifying files before opening them and handing unsupported files to the operating system will make mixed-project browsing safer and faster. We will validate this through cross-platform command-selection tests, unsupported-file placeholder tests, and regressions proving that supported text/image files retain their current inline behavior.

### Story 1: Toggle soft wrap from the keyboard

As a SpiceEdit user, I want to press `Esc z` to enable or disable soft wrap, so that I can quickly switch between reading long lines and preserving the current no-wrap view.

**Acceptance criteria:**

- [ ] A new App session starts with soft wrap disabled.
- [ ] Pressing `Esc z` enables soft wrap when it is disabled.
- [ ] Pressing `Esc z` again disables soft wrap.
- [ ] The toggle works only through the existing `Esc` leader timing rules.
- [ ] A lone `Esc` followed by an unbound key still falls through to normal editing.
- [ ] A status message confirms each toggle.
- [ ] Enabling soft wrap clears text-tab horizontal scroll to column zero.

### Story 2: Toggle soft wrap from the main menu

As a user who relies on discoverable actions, I want soft wrap to be available in the `≡` menu, so that I do not need to memorize `Esc z`.

**Acceptance criteria:**

- [ ] The main menu includes a soft-wrap row.
- [ ] The row shortcut displays `Esc z`.
- [ ] The row label is `Enable soft wrap` when soft wrap is off.
- [ ] The row label is `Disable soft wrap` when soft wrap is on.
- [ ] Activating the row toggles the same state as `Esc z`.

### Story 3: Render long lines as wrapped visual rows

As a user reading long-line content, I want long logical lines to wrap inside the editor pane, so that I can read the full content without horizontal scrolling.

**Acceptance criteria:**

- [ ] With soft wrap disabled, existing long-line rendering and horizontal scrolling are unchanged.
- [ ] With soft wrap enabled, a logical line longer than the content width renders across multiple visual rows.
- [ ] With soft wrap enabled, horizontal wheel/shift-wheel paths do not move text-tab `ScrollX`.
- [ ] Wrapped rows do not insert newline characters into the buffer.
- [ ] CJK wide characters and combining sequences are never split between visual rows.
- [ ] Syntax, selection, and find highlighting remain aligned to the correct text on wrapped rows.
- [ ] Toggling wrap off restores horizontal scrolling behavior without changing cursor logical position.

### Story 4: Navigate wrapped visual rows

As a user editing wrapped content, I want vertical movement, page scrolling, mouse placement, and drag selection to follow the visual rows on screen, so that soft wrap behaves like a complete editor mode.

**Acceptance criteria:**

- [ ] Left/right continues to move by logical character position.
- [ ] Up/down moves by wrapped visual row while preserving the current visual cell as closely as possible.
- [ ] Shift+up/down extends selection across wrapped visual rows without moving the anchor.
- [ ] Page up/down moves by a page of wrapped visual rows.
- [ ] Mouse wheel scrolling moves the wrapped visual viewport.
- [ ] Mouse hit testing on continuation rows returns the correct logical line and column.
- [ ] Drag selection and auto-scroll use continuation-row hit testing.
- [ ] Find-next and find-previous keep moving the logical cursor to the active match and reveal the wrapped segment containing the cursor when possible.
- [ ] The cursor remains visible after terminal resize and after toggling soft wrap when its logical line is in the visible viewport.

### Story 5: Enhance Markdown structure styling

As a developer editing Markdown in SpiceEdit, I want headings and fenced code blocks to stand out visually, so that README files, PRDs, and docs are easier to scan without opening a separate preview tool.

**Acceptance criteria:**

- [ ] Markdown heading lines beginning with `#`, `##`, `###`, `####`, `#####`, and `######` receive distinguishable heading styles.
- [ ] Heading styling preserves the literal `#` characters and heading text in the buffer and on screen.
- [ ] Fenced-code delimiter lines beginning with ```` ``` ```` or `~~~` receive a distinct style from normal Markdown prose.
- [ ] A language label after the opening fence, such as `go` in ```` ```go ````, receives a distinct accent style from the fence punctuation.
- [ ] Closing fence lines receive the fence delimiter style.
- [ ] Styling works with soft wrap on and off.
- [ ] Selection and find highlighting still override or combine correctly with Markdown structural styles.
- [ ] Non-Markdown files do not receive Markdown heading or fence styling.

### Story 6: Open unsupported files safely

As a developer browsing a mixed project, I want unsupported files to open in their associated desktop application, so that I can inspect them without loading binary data into the text editor.

**Acceptance criteria:**

- [ ] Selecting a supported text file opens an editable text tab and does not automatically invoke the operating-system opener.
- [ ] Selecting a supported image opens the existing read-only inline image preview and does not automatically invoke the operating-system opener.
- [ ] Selecting an unsupported file activates or reuses a read-only placeholder tab for that path and asynchronously invokes the default application.
- [ ] The placeholder displays exactly `编辑器只支持文本及图片文件在线编辑.`.
- [ ] The unsupported file's bytes are not loaded into an editable buffer, and Save/edit/find/format/comment actions are unavailable.
- [ ] A corrupt supported-image file follows unsupported-file handling rather than falling back to editable text.
- [ ] Reopening the same unsupported path does not create a duplicate tab.
- [ ] If external opening fails, the placeholder remains active and a concise error appears in the status bar.

### Story 7: Open the current file with `Esc o` or the main menu

As a SpiceEdit user, I want to open the current file with its operating-system default application, so that I can switch to a richer viewer or editor without locating the file again.

**Acceptance criteria:**

- [ ] Pressing `Esc o` opens the active text, image, or unsupported file's absolute on-disk path with the default application.
- [ ] The main `≡` menu includes `Open with default app` with shortcut `Esc o`.
- [ ] The menu row and `Esc o` use the same action path.
- [ ] The menu row is disabled when the active tab has no file path.
- [ ] Invoking `Esc o` without a file-backed tab reports a concise status message and does not alter editor state.
- [ ] Dirty text buffers are not auto-saved; opening externally does not clear the dirty state or modify undo history.
- [ ] Paths containing spaces, Unicode, quotes, or shell metacharacters open as one literal file path.

### Story 8: Behave consistently across supported operating systems

As a user on macOS, Windows, or Linux, I want external opening to use my platform's normal file association, so that the feature behaves like opening the same file from the desktop.

**Acceptance criteria:**

- [ ] macOS uses `open` with the absolute file path as an argument.
- [ ] Native Windows uses `cmd.exe /c start ""` or an equivalent native file-association mechanism.
- [ ] Linux uses `xdg-open`; an available `gio open` fallback may be used when `xdg-open` is missing.
- [ ] Starting or waiting for the opener never blocks keyboard, mouse, rendering, or file-tree refresh handling.
- [ ] Missing commands, missing files, permission failures, unsupported operating systems, and headless-session failures surface through the status bar.
- [ ] Tests replace the real process runner and assert executable/argument selection without opening an application on the developer machine.

## 8. Out of scope

- Persisting soft-wrap preference to `config.json`.
- Per-file or per-tab soft-wrap settings.
- Hard wrap, reflow, or automatic insertion/removal of newline characters.
- Word-boundary wrapping as a required first-release behavior. The required behavior is grapheme-safe cell wrapping; word-aware wrapping may be considered later.
- Minimap, wrap indicators, or additional gutter symbols for continued visual rows.
- Full-file visual-row indexing or caching.
- A separate Markdown preview mode.
- Shelling out to `glow` or adding a runtime dependency on the `glow` binary.
- Hiding Markdown punctuation, rendering proportional document layout, or adding non-file rows to make Markdown look like rich text.
- Full Glow/Glamour feature parity, including tables, block quotes, lists, links, images, and custom JSON stylesheets.
- Nested language-specific syntax highlighting inside Markdown fenced code blocks when it requires a broader parser integration.
- Inline editing or rich preview of PDFs, office documents, archives, databases, audio, video, or executable formats.
- An application chooser, per-extension association settings, custom opener commands, or overriding the operating system's default association.
- Copying or streaming a file from an SSH server to an application on the user's client machine.
- Automatically saving dirty buffers before external opening or synchronising unsaved in-memory edits into the external application.
- Adding new inline image codecs solely for this feature; supported inline formats remain governed by the image-preview subsystem.
- Any `Ctrl+` shortcut.

## 9. Dependencies and risks

### Dependencies

- `internal/editor/tab.go` rendering, viewport, `EnsureVisible`, `ScrollH`, and mouse hit-testing logic.
- Existing terminal-cell width helpers used by `LineVisualCol`, `RuneColAtVisual`, and rendering.
- `internal/app/leader.go` for the `Esc z` and new `Esc o` bindings.
- `internal/app/app.go` central file opening, tab rendering, menu layout, status messages, and asynchronous event delivery.
- `internal/editor/tab.go` for shared file classification and a read-only unsupported-file tab mode that does not allocate an editable buffer from binary content.
- Go's `runtime` and `os/exec` packages plus the host's platform opener (`open`, Windows file association, or `xdg-open`/`gio`).
- Existing Chroma-based highlighting in `internal/editor/highlight.go`; Markdown styling should extend or overlay this path rather than introducing an unrelated renderer.
- README and website hotkey documentation.

### Risks and mitigations

- **Risk:** wrapped rendering duplicates or misaligns highlighting across visual rows.
  - **Mitigation:** add simulation-screen tests for syntax/find/selection on wrapped rows.
- **Risk:** CJK or combining text splits at row boundaries.
  - **Mitigation:** reuse existing grapheme-aware visual-column helpers and add cell-level SimulationScreen boundary tests with CJK, combining, and tab samples.
- **Risk:** large files with very long lines become expensive to lay out across visual rows.
  - **Mitigation:** benchmark large files, cache layout only when needed, and invalidate the cache on edits or viewport-width changes.
- **Risk:** Markdown structural styling conflicts with syntax, selection, or find highlighting.
  - **Mitigation:** define style precedence explicitly: selection and current find match remain most visible; Markdown styles provide foreground/accent defaults underneath.
- **Risk:** Markdown enhancement grows into a full renderer and breaks edit fidelity.
  - **Mitigation:** keep all raw Markdown punctuation visible and exclude preview-mode behavior from V1 scope.
- **Risk:** extension-only detection misclassifies binary data as text or unknown text as unsupported.
  - **Mitigation:** combine known names/extensions with a bounded UTF-8/NUL content sample, let strong binary signals override extensions, and add regression fixtures for misleading filenames.
- **Risk:** desktop opening fails because no graphical session or opener is available.
  - **Mitigation:** keep the placeholder usable, report the process-start/exit error in the status bar, and never fall back to interpreting the file as text.
- **Risk:** filenames are interpreted as shell syntax on Windows or Unix-like systems.
  - **Mitigation:** isolate platform command construction behind a tested adapter and pass the absolute path as one argument without building a shell command string.
- **Risk:** users on SSH expect the file to open on the client workstation rather than the SpiceEdit host.
  - **Mitigation:** document the host-local behavior and keep client-side transfer/desktop integration out of scope until a transport design exists.

## 10. Open questions

- Should soft wrap eventually persist in `config.json`, and if so should it be global or per-extension?
- Should a later implementation wrap at word boundaries when possible, or keep grapheme-safe fixed-width wrapping?
- Should wrapped continuation rows indent to match the first non-whitespace column, or start at content column zero?
- Should the status bar expose the current wrap state persistently, or is the menu label plus toggle status enough?
- Should Markdown fenced-code bodies eventually use language-specific highlighting based on the fence label?
- Which Markdown structures beyond headings and fenced code blocks should receive lightweight styling next, if any?
- Should users eventually be able to override file classification or choose an application instead of always using the operating-system default?
- Should WSL prefer a Windows-host opener such as `wslview`, or continue to follow Linux runtime behavior?
- Which additional image formats, if any, should become eligible for inline preview rather than external opening?

## Self-assessment

- **Strongest section:** user stories and acceptance criteria; the requested behavior is clear and testable.
- **Weakest section:** evidence and product metrics; the request did not include usage data.
- **Top assumptions to validate:** session-wide wrap setting, no first-release persistence, grapheme-safe fixed-width wrapping as the minimum viable behavior, Glow-inspired Markdown structural styling being enough without a separate preview, bounded UTF-8/NUL sniffing being sufficient for safe file classification, and host-local opening matching user expectations in SSH workflows.
- **Recommended next step:** engineering plan covering the shared file classifier, unsupported-file tab mode, injectable platform opener, `Esc o`/menu wiring, and regression tests alongside the remaining Markdown styling work.
