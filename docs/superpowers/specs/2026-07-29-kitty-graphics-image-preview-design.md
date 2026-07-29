# Kitty Graphics Image Preview PRD

## Document information

- **Authors:** SpiceEdit maintainers
- **Reviewers:** SpiceEdit maintainer; Herdr maintainer for the adapter contract
- **Date:** 2026-07-29
- **Status:** Proposed, design approved; pending PRD review

| Version | Date | Change |
|---|---|---|
| 0.1 | 2026-07-29 | Initial PRD for Kitty Graphics Protocol image previews and Herdr support. |

## 1. Executive summary

SpiceEdit will use the Kitty Graphics Protocol (KGP) as the preferred image-preview renderer for developers running native Kitty, WezTerm, or Ghostty with KGP enabled. It will retain the current half-block renderer as an automatic fallback for every other environment and for any graphics failure. Images remain read-only, preserve their aspect ratio, and continue to support PNG, JPEG, and the first GIF frame.

Herdr receives a dedicated adapter because it manages pane graphics through its socket API. A user must enable Herdr's experimental graphics capability before SpiceEdit uses that adapter. 🔶 **Assumption:** a configured local Herdr session is the intended use case; remote attachments remain on half-block. Zero-configuration Herdr graphics is an external Herdr product dependency, not functionality SpiceEdit may enable by rewriting user configuration.

## 2. Problem statement

### Who has this problem?

- **Primary users:** developers who open PNG, JPEG, or GIF files in SpiceEdit from a KGP-capable terminal and need a useful image preview without leaving the editor.
- **Secondary users:** developers who run SpiceEdit inside a configured local Herdr pane.

### What is the problem?

`internal/editor/image.go` currently maps source pixels to U+2580 half-block cells. Each cell carries two vertical color samples, so detail is capped by the terminal grid instead of using the terminal emulator's graphics layer. The renderer produces a usable preview, but it cannot provide the fidelity expected from KGP-capable terminals.

### Why is it painful?

- **User impact:** detailed images, text in images, and high-density screenshots lose detail; users must open another viewer for an accurate inspection.
- **Product impact:** SpiceEdit advertises image tabs but does not use the graphic capability available in its target terminal environments.

### Evidence

- `internal/editor/image.go` documents the current half-block renderer and its grid-limited resolution.
- `internal/editor/image_test.go` asserts half-block glyph and color output, confirming it is the active rendering path.
- 🔵 **Open Question:** no user interviews, usage telemetry, or support tickets were supplied for this feature.

## 3. Target users and jobs-to-be-done

### Primary persona: terminal-native developer

- **Context:** works in Kitty, WezTerm, or Ghostty, opens repository assets and screenshots while editing code.
- **Goal:** inspect an image accurately without interrupting the terminal workflow.
- **Current behavior:** opens an image tab and receives a half-block approximation.
- **Job to be done:** when I open a supported image in a capable terminal, show a sharp, proportionally fitted preview in the editor pane.

### Secondary persona: Herdr user

- **Context:** runs SpiceEdit inside a local Herdr pane and has enabled Herdr graphics in its configuration.
- **Goal:** receive the same image-preview behavior without protocol artifacts or stale images across pane operations.
- **Job to be done:** when a pane hosts SpiceEdit, render and clear images through Herdr's pane-graphics lifecycle.

🔶 **Assumption:** the target terminals and Herdr are used locally rather than through an unconfigured terminal multiplexer or remote client.

## 4. Strategic context

### Why now?

The existing image-tab feature is already isolated in `internal/editor/image.go` and its current tests define clear fallback behavior. Adding a graphics presenter now improves fidelity without changing image-tab editing semantics.

### Strategic alignment

This work supports SpiceEdit's terminal-native workflow: users should be able to inspect common repository assets in the editor rather than switch tools.

🔵 **Open Question:** no business OKRs, revenue target, or product-adoption baseline was supplied. This PRD therefore uses technical release criteria instead of business targets.

### Alternatives considered

1. **Direct in-process KGP encoder — selected.** A small internal implementation owns PNG encoding, chunking, placement IDs, and cleanup without adding a runtime dependency.
2. **Third-party KGP library — rejected.** It still requires lifecycle coordination with tcell and introduces a dependency for a compact, protocol-specific surface.
3. **External image command — rejected.** A subprocess cannot reliably synchronize an image with SpiceEdit's redraw, modal, tab, resize, and shutdown lifecycle.

## 5. Solution overview

### Solution description

Introduce `internal/termgraphics` as the owner of terminal-specific image presentation. It exposes one lifecycle-oriented `ImagePresenter` contract and selects one of three implementations: a Herdr adapter, a direct KGP presenter, or the existing half-block presenter.

`internal/editor/image.go` keeps image decoding, first-frame GIF behavior, and aspect-ratio fitting. It no longer owns protocol writes. `internal/app` owns presentation timing because it calls `screen.Show()` and can safely invoke an external graphics presenter only after tcell has flushed its frame.

```text
image tab (decoded image + editor cell rectangle)
                    |
                    v
          terminal graphics selector
          /            |             \
         v             v              v
HerdrPresenter   KittyPresenter   HalfBlockPresenter
socket API        underlying TTY   tcell cell renderer
```

### Terminal selection and fallback

1. When SpiceEdit detects an eligible Herdr session, it tries `HerdrPresenter` first.
2. Otherwise, it selects `KittyPresenter` only for native Kitty, WezTerm, or Ghostty environments in which KGP is enabled.
3. It selects `HalfBlockPresenter` in every other environment, including tmux and Zellij. KGP passthrough through multiplexers is out of scope.
4. If an eligible graphics presenter cannot initialize, encode, or write, SpiceEdit redraws the current frame with half-block output and disables that presenter for the rest of the session.

### KGP presentation lifecycle

- Encode the already-decoded `image.Image` as PNG, preserving current support for PNG, JPEG, and GIF first frames.
- Transmit PNG data in protocol-safe chunks and place it at the fitted editor-cell rectangle after `screen.Show()`.
- Use stable image and placement IDs. A changed image, rectangle, or tab replaces the previous placement; inactive tabs, modal overlays, and shutdown remove it.
- Set quiet transmission behavior so terminal protocol replies do not interfere with tcell input handling.
- Opening any SpiceEdit menu, context menu, prompt, or confirmation overlay temporarily clears the image placement. When the overlay closes, the presenter restores the active image. This prevents a graphic from bleeding through or covering terminal UI layers.

### Herdr configuration and adapter

Herdr image preview is available only after the user adds this setting to `~/.config/herdr/config.toml`:

```toml
[experimental]
kitty_graphics = true
```

After editing the configuration, the user runs:

```sh
herdr server reload-config
```

Reload applies most Herdr UI settings. If Herdr still reports graphics as disabled after reload, the user must restart the Herdr server before expecting image previews. SpiceEdit must never create, edit, reload, or restart Herdr configuration. If the setting is absent, Herdr reports the feature as disabled and SpiceEdit uses half-block rendering after one status-bar hint.

For a detected Herdr session, `HerdrPresenter` must:

- require Herdr's injected `HERDR_ENV=1`, `HERDR_SOCKET_PATH`, and `HERDR_PANE_ID` values before connecting to the current pane;
- call `pane.graphics.info` before the first placement to verify availability and retrieve the pane's cell-pixel dimensions;
- call `pane.graphics.set` with PNG `data_base64`, source `image_width` and `image_height`, and the current `viewport_col`, `viewport_row`, `grid_cols`, and `grid_rows`;
- call `pane.graphics.clear` before replacement, deactivation, overlay display, or shutdown;
- fall back on `feature_disabled`, socket errors, malformed responses, or failed requests.

The first release uses set and clear operations for static images. It does not use streaming operations or animation.

### Zero-configuration Herdr support

SpiceEdit cannot provide zero-configuration Herdr graphics on its own. That outcome requires an upstream Herdr change that makes graphics enabled by default or exposes a safe per-pane temporary authorization API. Until that dependency exists, the configured adapter and half-block fallback are the supported behavior.

## 6. Success metrics

### Primary metric

**Supported-environment preview success rate** — the percentage of defined manual acceptance scenarios in which an image tab renders with the selected graphics presenter and no residual placement remains after its lifecycle event.

- **Current:** 0% KGP/Herdr graphics coverage; the current implementation only renders half-block cells.
- **Target:** 100% of the defined local Kitty, WezTerm, Ghostty, and configured-local-Herdr acceptance scenarios pass before release.
- **Timeline:** release-candidate validation.

### Secondary metrics

- All existing PNG/JPEG decoding, first-frame GIF, aspect-fit, and half-block tests remain passing.
- 100% of simulated presentation failures redraw an image with half-block output rather than leaving a blank editor pane.
- 0 residual image placements after tab switch, close, overlay open, resize replacement, and orderly shutdown in manual target-terminal checks.

### Guardrails

- Non-KGP terminals preserve the current image-preview behavior without configuration.
- Image tabs remain read-only; editing, saving, find, and text-tab behavior do not regress.
- No Ctrl-key shortcut is introduced; all existing actions stay reachable from the main menu.

🔶 **Assumption:** release validation can use a representative locally installed version of each target terminal. No automated terminal-emulator telemetry or CI matrix exists today.

## 7. User stories and requirements

### Epic hypothesis

We believe that using native graphics presenters for KGP-capable terminals and configured local Herdr panes will let terminal-native developers inspect supported images with higher fidelity while retaining a reliable preview in every other environment. We will validate this through the supported-environment preview success rate and no-regression fallback tests.

### Story 1: Render an image through KGP

As a developer using native Kitty, WezTerm, or Ghostty with KGP enabled, I want a supported image file to render through the graphics protocol, so that I can inspect it at terminal graphics quality.

**Acceptance criteria:**

- [ ] Opening a PNG, JPEG, or GIF opens the existing read-only image tab.
- [ ] PNG and JPEG display their decoded content; GIF displays only its first frame.
- [ ] The rendered image preserves its aspect ratio and fits inside the editor pane.
- [ ] The KGP presenter writes only after tcell has flushed its screen frame.
- [ ] The KGP presenter does not emit half-block glyphs inside the visible image rectangle on success.
- [ ] A failure before or during presentation immediately produces the half-block preview in the same visible state.

### Story 2: Keep graphics synchronized with the editor

As a developer viewing an image tab, I want the graphics placement to follow the editor state, so that I never see stale or misplaced image content.

**Acceptance criteria:**

- [ ] Resizing the terminal recomputes the fitted rectangle and replaces the visible placement.
- [ ] Switching away from an image tab removes its placement before another tab becomes visible.
- [ ] Reloading an image replaces the previous data and placement.
- [ ] Opening any SpiceEdit overlay clears the placement; closing the overlay restores it when the image tab remains active.
- [ ] Closing an image tab or exiting SpiceEdit clears the placement.

### Story 3: Preserve universal fallback behavior

As a developer using an unsupported terminal, I want image tabs to keep working, so that the graphics enhancement never turns a preview into a blank pane.

**Acceptance criteria:**

- [ ] Unsupported terminals, tmux, and Zellij use the existing half-block renderer.
- [ ] No raw KGP sequence is emitted in a fallback environment.
- [ ] A graphics presenter failure disables only that presenter for the remainder of the session and uses half-block thereafter.
- [ ] The first fallback caused by an eligible-but-unavailable presenter displays one actionable status-bar message; later draws do not spam messages.

### Story 4: Render inside configured local Herdr

As a developer running SpiceEdit in configured local Herdr, I want image tabs to use Herdr pane graphics, so that previews work inside the pane without terminal artifacts.

**Acceptance criteria:**

- [ ] The PRD and user-facing documentation state the exact `[experimental] kitty_graphics = true` prerequisite, the `herdr server reload-config` command, and the requirement to restart Herdr if the feature remains disabled after reload.
- [ ] The adapter calls pane-graphics capability information before its first placement.
- [ ] The adapter uses pane-relative viewport and grid coordinates for every placement.
- [ ] The adapter clears Herdr graphics for every Story 2 lifecycle event.
- [ ] A disabled feature or socket/API error falls back to half-block and shows the one-time configuration hint.
- [ ] Remote Herdr attachments remain on half-block unless a future Herdr contract explicitly supports them.

### Constraints and edge cases

- Direct tcell cell drawing and external graphics writes must not race; the app event loop owns both operations in order.
- An image may be smaller than the pane; its computed rectangle remains centered and fitted as today.
- A zero-sized editor rectangle, nil image, unreadable file, or failed decode keeps existing safe no-op/error behavior.
- KGP must not use protocol replies as a capability mechanism in the first release because tcell owns terminal input parsing. Terminal selection is a controlled environment allowlist.
- The implementation must add short doc comments above new public and private helpers, follow `gofmt`, and add same-package tests for each non-trivial source file.

## 8. Out of scope

- Automatically editing, enabling, or reloading Herdr configuration.
- Zero-configuration Herdr graphics before Herdr supplies a default-enabled or per-pane authorization capability.
- Herdr remote-attachment graphics support.
- tmux or Zellij KGP passthrough.
- GIF animation, image zooming, panning, rotation, or editing.
- New image formats such as WebP, SVG, BMP, or animated image decoding.
- Replacing the current decoder or adding an image-processing library solely to change interpolation quality.

## 9. Dependencies and risks

### Dependencies

- **tcell:** `tcell.Screen.Tty()` must expose a writable terminal TTY for direct KGP presentation. If unavailable, the fallback is mandatory.
- **Terminal capability:** the local Kitty, WezTerm, or Ghostty installation must support and enable the required KGP behavior.
- **Herdr configuration:** local Herdr users must set `[experimental] kitty_graphics = true` and reload configuration before the adapter can present graphics.
- **Herdr socket contract:** access to pane information, graphics set, and graphics clear operations must remain compatible with the adapter.
- **Upstream Herdr change:** required only for the explicitly requested zero-configuration Herdr future state.

### Risks and mitigations

| Risk | Mitigation |
|---|---|
| tcell and graphics output corrupt each other's terminal state. | Restrict graphics calls to the main event loop after `screen.Show()`; unit-test sequence generation and manually test redraw transitions. |
| A terminal advertises an expected identity but lacks KGP support. | Treat any initialization or write failure as a session-level fallback trigger. |
| A stale placement remains after a modal, resize, or tab switch. | Centralize `Present`, `Replace`, and `Clear` ownership in one presenter lifecycle and test every state transition. |
| Herdr feature configuration is absent or unavailable. | Detect the failure, display the exact one-time configuration hint, and continue with half-block. |
| Herdr API or terminal behavior changes. | Keep adapters small and isolated; pin their contract with unit tests and a manual release matrix. |
| A large image makes PNG encoding or protocol output expensive. | Reuse the decoded image, chunk output, and add a 🔵 **Open Question** before implementation: define a source-size or encoded-payload limit only if profiling shows a meaningful regression. |

## 10. Open questions

| Question | Owner | Deadline | Status |
|---|---|---|---|
| What minimum versions and feature settings of WezTerm and Ghostty pass the required KGP acceptance matrix? | SpiceEdit maintainer | Before implementation | Open |
| Which Herdr version and attached-client modes support the required pane graphics calls? | SpiceEdit maintainer and Herdr maintainer | Before implementation | Open |
| Will Herdr add default-enabled graphics or a safe per-pane authorization API to satisfy the zero-configuration request? | Herdr maintainer | Before a future zero-config release | Open |
| Does profiling require a source-image or encoded-payload limit? | SpiceEdit maintainer | During implementation | Open |
| Should a later release add an explicit user configuration override for KGP detection? | SpiceEdit maintainer | After initial release feedback | Open |

## Validation plan

### Automated tests

- Keep `internal/editor/image_test.go` coverage for extension recognition, image decoding, nearest-neighbor sizing, half-block fitting, rendering, and themed background behavior.
- Add `internal/termgraphics` tests for environment selection, chunk boundaries, quiet KGP sequence generation, image/placement replacement, cleanup, and write failures.
- Add Herdr adapter tests with a mock socket client for successful `info`/`set`/`clear`, disabled graphics, malformed response, and socket failure.
- Add App-level lifecycle tests with a fake presenter for active-tab changes, terminal resize, overlay open/close, image reload, and shutdown cleanup.
- Run `make test` before review.

### Manual acceptance matrix

| Environment | Expected presenter | Required result |
|---|---|---|
| Native Kitty | KGP | Sharp PNG/JPEG/GIF-first-frame preview, fitted and cleanly removed. |
| WezTerm with KGP enabled | KGP | Same as Kitty. |
| Ghostty with KGP enabled | KGP | Same as Kitty. |
| Local Herdr with experimental graphics enabled | Herdr adapter | Image appears through pane graphics; resize, overlay, tab switch, and exit clear it. |
| Local Herdr without experimental graphics | Half-block | Preview remains visible; one configuration hint appears. |
| macOS Terminal or another unsupported terminal | Half-block | Existing preview behavior remains intact. |
| tmux or Zellij | Half-block | No KGP output is attempted. |

## PRD self-assessment

### Strongest section

Section 5 is strongest because the existing `image.go`, tcell draw loop, and requested terminal scopes establish clear ownership boundaries and fallback behavior.

### Weakest section

Section 6 is weakest because SpiceEdit has no supplied adoption telemetry or verified terminal-version matrix. The release uses technical acceptance criteria until that information exists.

### Top assumptions to validate

| # | Assumption | Section | Risk if wrong | Proposed validation |
|---|---|---|---|---|
| 1 | Local target versions of WezTerm and Ghostty implement the required KGP placement behavior. | 6, 10 | Expected graphics path falls back or renders incorrectly. | Run the manual acceptance matrix before release. |
| 2 | Local configured Herdr exposes stable pane-graphics behavior to SpiceEdit. | 5, 9 | Herdr adapter cannot reliably place or clear images. | Test a mock socket contract and a real configured Herdr pane. |
| 3 | KGP PNG encoding and transfer do not create an unacceptable performance regression for typical repository images. | 9 | Opening images feels slow or consumes excessive memory. | Profile representative images during implementation. |

### Recommended next step

Review this PRD, especially the Herdr configuration prerequisite and the explicit zero-configuration dependency. Once approved, create a detailed implementation plan before code changes begin.

## References

- Current image-tab implementation: `internal/editor/image.go` and `internal/editor/image_test.go`.
- [Kitty Graphics Protocol](https://sw.kovidgoyal.net/kitty/graphics-protocol/).
- [Herdr configuration](https://herdr.dev/docs/configuration/).
- [Herdr socket API](https://herdr.dev/docs/socket-api/).
