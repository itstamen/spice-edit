# Herdr Right-Click Passthrough Design

## Document information

- **Date:** 2026-08-03
- **Status:** Approved
- **Related design:** `docs/superpowers/specs/2026-08-03-tab-context-close-design.md`
- **Selected approach:** Configure Ctrl+right-click passthrough in Herdr and expose batch tab-close actions in SpiceEdit's main menu.

## 1. Goal

Make the tab-close actions usable when SpiceEdit runs inside Herdr. Herdr reserves an unmodified right-click for its pane menu, so SpiceEdit cannot receive that mouse event. The fix must provide one Herdr-supported mouse path and one SpiceEdit-owned fallback that works without Button3 delivery.

## 2. Observed behavior

The reported popup contains Herdr pane actions such as `Rename pane`, `Split right`, `Zoom`, and `Close pane`. The running environment exports `HERDR_ENV=1`, and the local Herdr configuration does not define `ui.right_click_passthrough_modifier`.

SpiceEdit already calls tcell's mouse support and correctly handles `tcell.Button3` when the event reaches the process. Unit tests exercise that event path. No SpiceEdit code can dismiss or replace a menu opened by the outer Herdr client before the event enters the pane terminal stream.

## 3. Herdr passthrough configuration

Update `~/.config/herdr/config.toml` without replacing existing settings:

```toml
[ui]
right_click_passthrough_modifier = "ctrl"
```

If `[ui]` already exists, add or replace only `right_click_passthrough_modifier`. Do not create a duplicate TOML table. Preserve `onboarding`, experimental graphics, custom keys, and every unrelated user setting.

After editing:

1. Run `herdr config check` and require a successful validation.
2. Run `herdr server reload-config` so the active session receives the change.
3. Use Ctrl+right-click on a SpiceEdit tab to forward Button3 to the pane application.

Plain right-click remains Herdr's pane-context gesture. Shift is not selected because Herdr intentionally rejects Shift in this setting and terminal emulators commonly reserve it for local selection or mouse override behavior.

## 4. SpiceEdit main-menu fallback

Add these rows next to the existing `Close tab` main-menu action:

- `Close other tabs`
- `Close all tabs`

The rows have no new shortcut labels.

### Enabled states

- `Close tab`: preserve current behavior.
- `Close other tabs`: enabled only when an active tab exists and at least two tabs are open.
- `Close all tabs`: enabled when at least one tab is open.

### Actions

`Close other tabs` captures the active `*editor.Tab` and calls the existing `requestCloseTabs(a.tabsExcept(target))` flow. The target remains active after successful closure.

`Close all tabs` snapshots all current tab pointers and calls `requestCloseTabs`. The existing batch Save / Discard / Cancel semantics remain authoritative:

- one modal for all dirty targets;
- Save writes only targets;
- Cancel closes nothing;
- a save failure closes no target tabs.

The context menu and main menu must use the same close helpers so they cannot diverge.

## 5. Interaction matrix

| Input | Herdr behavior | SpiceEdit behavior |
|---|---|---|
| Plain right-click on tab | Open Herdr pane menu | No event received |
| Ctrl+right-click on tab | Forward mouse gesture | Open tab context menu |
| Click `≡` or double-tap Esc | Forward normal input | Open main action menu with all three close actions |
| `Esc w` | Forward key input | Close active tab through existing flow |

## 6. Error handling

- If Herdr config validation fails, restore or correct the edited setting before reloading; do not leave an invalid config active.
- If no Herdr server is running, `reload-config` may report that condition, but the validated file remains ready for the next Herdr launch.
- Main-menu actions do not depend on Herdr detection and work in every supported terminal.
- No startup warning or automatic global-config mutation is added to the SpiceEdit binary.

## 7. Testing

SpiceEdit tests will cover:

- the main menu contains `Close tab`, `Close other tabs`, and `Close all tabs` in one group;
- `Close other tabs` is disabled for zero or one tab and enabled for two or more tabs;
- `Close all tabs` is disabled with no tabs and enabled otherwise;
- activating `Close other tabs` preserves only the active tab;
- activating `Close all tabs` closes every clean tab;
- dirty main-menu actions reuse the existing batch confirmation behavior;
- the original tab context menu tests remain passing;
- `Esc w` and existing menu-close behavior remain unchanged.

Run focused menu and tab-close tests, `make test`, `make build`, `make build-linux`, and Windows cross-compilation.

Validate the external configuration with `herdr config check` and apply it with `herdr server reload-config`. A manual Ctrl+right-click smoke test is required because unit tests cannot synthesize a mouse event through the outer Herdr client.

## 8. Documentation

Update README and website mouse/SSH troubleshooting documentation to distinguish:

- ordinary terminals that forward Button3 directly;
- Herdr's plain right-click pane menu;
- Herdr Ctrl+right-click passthrough after configuration;
- the `≡` / double-Esc main-menu fallback.

Do not state that SpiceEdit can override Herdr's unmodified pane menu.

## 9. Non-goals

- Do not change Herdr source code or its plain right-click behavior.
- Do not add Shift+right-click support.
- Do not add shortcuts for `Close other tabs` or `Close all tabs`.
- Do not remove the existing tab context menu.
- Do not add terminal-specific branches to SpiceEdit's mouse event handler.
- Do not overwrite unrelated Herdr configuration.
