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

## 1. Executive summary

SpiceEdit will add an optional soft-wrap mode for text tabs so long lines can be viewed within the editor pane without horizontal scrolling. Soft wrap is off by default to preserve the current editing model and terminal-editor behavior. Users can toggle it with `Esc z` or the main `≡` action menu; the setting applies to the current editor session and affects text rendering and basic cursor visibility only, not file contents.

For Markdown files, SpiceEdit will also enhance display styling so common document structure is easier to scan while editing. Headings that begin with `#` use distinct colors by level, and fenced code-block markers such as ```` ```go ```` use a different style from normal prose. The goal is a lightweight Glow-inspired terminal reading feel inside the existing editor, not a separate Markdown preview mode.

## 2. Problem statement

### Who has this problem?

- **Primary users:** developers editing or reading files with long logical lines, such as Markdown paragraphs, logs, JSON, generated output, and prose.
- **Secondary users:** developers working over SSH or inside tmux/zellij where mouse gestures and some terminal shortcuts are unreliable, and where every action must remain reachable from the `≡` menu.

### What is the problem?

SpiceEdit currently renders long lines as single horizontal rows. Users must scroll horizontally to inspect content beyond the viewport. That is appropriate for code in many cases, but it is painful for prose, logs, and structured data where reading continuity matters more than preserving one logical line per screen row.

Markdown files have an additional readability issue: document structure is visually flat compared with terminal Markdown readers. Headings, fenced-code markers, and code-block language labels do not stand out enough for users scanning README files, PRDs, notes, or documentation.

### Why is it painful?

- Long-line reading requires repeated horizontal scrolling and makes nearby context hard to scan.
- The cursor can be visible while the relevant right-side content is off-screen.
- Users who expect a terminal editor to offer a wrap toggle have no discoverable way to trade horizontal scrolling for wrapped visual rows.
- Markdown authors must visually parse raw punctuation such as `#`, ```` ``` ```` and language tags rather than seeing stronger structural cues.

### Evidence

- `internal/editor/tab.go` tracks `ScrollX` and renders one logical buffer line per screen row today.
- `README.md` documents the `Esc` leader model and the requirement that hotkey actions also live in the `≡` menu.
- Glow demonstrates the user expectation that terminal Markdown can be styled for reading, with themes, word wrapping, and document-oriented formatting. SpiceEdit should borrow the readability direction without becoming a separate Markdown reader.
- 🔵 **Open Question:** no user analytics or support-ticket volume was supplied for long-line pain; this PRD is based on a direct feature request.

## 3. Target users and jobs-to-be-done

### Primary persona: terminal-native developer

- **Context:** edits code, Markdown, config, logs, and generated files in a terminal.
- **Goal:** choose whether long lines stay horizontally scrollable or wrap to the pane width, and scan Markdown structure quickly while editing.
- **Job to be done:** when I am reading or editing a long-line Markdown file, let me turn on soft wrap and visually distinguish headings and fenced code blocks without modifying the file.

### Secondary persona: SSH/tmux user

- **Context:** uses SpiceEdit inside terminal environments where `Ctrl+` shortcuts or mouse interactions may be intercepted.
- **Goal:** access the wrap toggle through the established `Esc` leader and the main action menu.
- **Job to be done:** when I need to change wrapping, give me a keyboard path and a menu path that both work in remote terminal sessions.

## 4. Strategic context

### Why now?

Soft wrap is a high-leverage readability feature that fits SpiceEdit's terminal-native workflow. The existing renderer already owns visual columns, CJK width handling, horizontal scroll, mouse hit testing, and viewport calculations, giving the feature a clear implementation surface.

Markdown display enhancement is a natural companion because the most common soft-wrap beneficiaries are README files, docs, notes, and PRDs. SpiceEdit already uses Chroma for syntax highlighting, so the first release should extend editor styling for Markdown-specific structure before considering heavier rendering libraries.

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

This keeps V1 explicit and DRY without committing to a full visual-row navigation model.

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
- `ScrollY` remains a logical-line scroll offset in V1. Rendering expands wrapped segments only from the current visible logical-line window until the screen is filled; it does not build a full-file visual-row index.

### Navigation and editing behavior

- Left/right, insertion, deletion, selection, undo, redo, save, find, and line-comment operations continue to operate on logical buffer lines and columns.
- Up/down, Page up/down, wheel scrolling, mouse hit testing, and find navigation keep their current logical-line behavior in V1. Full visual-row navigation is deferred.
- Toggling soft wrap recalculates viewport state so the logical cursor line and the wrapped segment containing the cursor are visible when possible.

### Persistence

The first release does not persist soft-wrap preference to config. 🔶 **Assumption:** default-off, session-only behavior reduces surprise and avoids adding a config migration before users validate the interaction.

## 6. Success metrics

### Primary metric

**Soft-wrap acceptance pass rate** — the percentage of defined rendering, navigation, toggle, and regression scenarios that pass before release.

- **Current:** 0%; there is no soft-wrap mode.
- **Target:** 100% of required automated tests and manual smoke checks pass.
- **Timeline:** release-candidate validation.

### Secondary metrics

- Existing no-wrap rendering, horizontal scroll, CJK width, selection, find, and image-tab tests remain passing.
- `Esc z` and the `≡` menu toggle exercise the same code path.
- No saved file changes occur from toggling soft wrap alone.
- Wrapped rendering tests include cell-level `tcell.NewSimulationScreen("UTF-8")` assertions for CJK wide characters, combining sequences, and tab-stop boundaries.
- Markdown rendering tests verify heading-level colors and fenced-code delimiter/language-label styles without changing the underlying buffer text.

### Guardrails

- Soft wrap is off by default.
- No `Ctrl+` shortcut is introduced.
- Image tabs remain unaffected; soft wrap applies only to text tabs.
- The feature must not degrade CJK or combining-character rendering.
- Markdown display enhancement must remain edit-safe: raw Markdown text stays visible, selectable, searchable, and saved unchanged.

## 7. User stories and requirements

### Epic hypothesis

We believe that adding a default-off soft-wrap toggle will improve long-line readability for terminal-native developers without disrupting existing code-editing workflows. We will validate this through rendering/navigation acceptance tests and no-regression coverage for the current horizontal-scroll behavior.

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

### Story 4: Preserve current navigation model in V1

As a user editing wrapped content in V1, I want existing editing commands to remain stable, so that enabling soft wrap does not unexpectedly rewrite navigation semantics.

**Acceptance criteria:**

- [ ] Left/right continues to move by logical character position.
- [ ] Up/down continues to move by logical buffer line in V1.
- [ ] Page up/down and mouse wheel scrolling continue to move by logical buffer lines in V1.
- [ ] Mouse hit testing remains correct for unwrapped rows and the first visible segment of a wrapped logical line; precise continuation-row hit testing is deferred.
- [ ] Find-next and find-previous keep moving the logical cursor to the active match; precise scrolling to a wrapped segment is deferred.
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

## 8. Out of scope

- Persisting soft-wrap preference to `config.json`.
- Per-file or per-tab soft-wrap settings.
- Hard wrap, reflow, or automatic insertion/removal of newline characters.
- Word-boundary wrapping as a required first-release behavior. The required behavior is grapheme-safe cell wrapping; word-aware wrapping may be considered later.
- Minimap, wrap indicators, or additional gutter symbols for continued visual rows.
- Full visual-row navigation for Up/Down, Page up/down, wheel scrolling, mouse hit testing on continuation rows, and find scrolling to the exact wrapped segment.
- Full-file visual-row indexing or caching.
- A separate Markdown preview mode.
- Shelling out to `glow` or adding a runtime dependency on the `glow` binary.
- Hiding Markdown punctuation, rendering proportional document layout, or adding non-file rows to make Markdown look like rich text.
- Full Glow/Glamour feature parity, including tables, block quotes, lists, links, images, and custom JSON stylesheets.
- Nested language-specific syntax highlighting inside Markdown fenced code blocks when it requires a broader parser integration.
- Any `Ctrl+` shortcut.

## 9. Dependencies and risks

### Dependencies

- `internal/editor/tab.go` rendering, viewport, `EnsureVisible`, `ScrollH`, and mouse hit-testing logic.
- Existing terminal-cell width helpers used by `LineVisualCol`, `RuneColAtVisual`, and rendering.
- `internal/app/leader.go` for the `Esc z` binding.
- `internal/app/app.go` menu layout and dynamic menu labels.
- Existing Chroma-based highlighting in `internal/editor/highlight.go`; Markdown styling should extend or overlay this path rather than introducing an unrelated renderer.
- README and website hotkey documentation.

### Risks and mitigations

- **Risk:** wrapped rendering duplicates or misaligns highlighting across visual rows.
  - **Mitigation:** add simulation-screen tests for syntax/find/selection on wrapped rows.
- **Risk:** CJK or combining text splits at row boundaries.
  - **Mitigation:** reuse existing grapheme-aware visual-column helpers and add cell-level SimulationScreen boundary tests with CJK, combining, and tab samples.
- **Risk:** V1 users expect Up/Down and mouse clicks to follow wrapped visual rows immediately.
  - **Mitigation:** document V1 navigation as logical-line based and track full visual-row navigation as follow-up work.
- **Risk:** large files with very long lines become expensive to lay out.
  - **Mitigation:** keep `ScrollY` logical in V1 and compute wrap segments only from the visible logical-line window until the screen is filled.
- **Risk:** Markdown structural styling conflicts with syntax, selection, or find highlighting.
  - **Mitigation:** define style precedence explicitly: selection and current find match remain most visible; Markdown styles provide foreground/accent defaults underneath.
- **Risk:** Markdown enhancement grows into a full renderer and breaks edit fidelity.
  - **Mitigation:** keep all raw Markdown punctuation visible and exclude preview-mode behavior from V1 scope.

## 10. Open questions

- Should soft wrap eventually persist in `config.json`, and if so should it be global or per-extension?
- Should a later implementation wrap at word boundaries when possible, or keep grapheme-safe fixed-width wrapping?
- Should wrapped continuation rows indent to match the first non-whitespace column, or start at content column zero?
- Should the status bar expose the current wrap state persistently, or is the menu label plus toggle status enough?
- What is the desired UX for V2 full visual-row navigation across Up/Down, Page up/down, wheel scrolling, mouse hit testing, and find scrolling?
- Should Markdown fenced-code bodies eventually use language-specific highlighting based on the fence label?
- Which Markdown structures beyond headings and fenced code blocks should receive lightweight styling next, if any?

## Self-assessment

- **Strongest section:** user stories and acceptance criteria; the requested behavior is clear and testable.
- **Weakest section:** evidence and product metrics; the request did not include usage data.
- **Top assumptions to validate:** session-wide setting, no first-release persistence, grapheme-safe fixed-width wrapping as the minimum viable behavior, logical-line navigation remaining acceptable for V1, and Glow-inspired Markdown structural styling being enough without a separate preview.
- **Recommended next step:** implementation plan for the minimal wrap-layout helper and Markdown structural style overlay, followed by focused tests for default-off state, `Esc z`, menu discoverability, wrapped rendering, basic cursor visibility, Markdown heading/fence styling, and CJK boundary behavior.
