---
title: "Troubleshooting"
metaTitle: "SpiceEdit Troubleshooting — Common Fixes"
metaDescription: "Right-click does nothing? Clipboard not working? Indexing slow? The fixes for the issues SpiceEdit users hit most, with terminal-specific notes."
summary: "Fixes for the most common SpiceEdit issues."
weight: 120
---

Common issues, with fixes.

## Right-click does nothing

You're probably in macOS Terminal + tmux. Apple's default Terminal eats Button3 events before tmux can forward them, so SpiceEdit never sees the right-click.

**Fix.** Use the `≡` icon in the top-left or double-tap `Esc` to open the action menu. Every right-click action also lives there. Long-term: switch to iTerm2, WezTerm, or Ghostty, which all forward Button3 correctly.

## Clipboard isn't working over SSH

OSC 52 is the mechanism. Most terminal emulators ship with OSC 52 writes disabled by default for security reasons. You have to opt in.

**iTerm2.** Settings → General → Selection → "Applications in terminal may access clipboard."

**Kitty.** Add `clipboard_control write-clipboard write-primary` to `kitty.conf`.

**WezTerm.** OSC 52 is on by default; check your `.wezterm.lua` doesn't have `enable_csi_u_key_encoding` blocking it.

**Ghostty.** Settings → "OSC 52 clipboard write" → Allow.

**tmux.** Run `tmux show-options -g set-clipboard`. If it's `off`, run `tmux set-option -g set-clipboard on` (default since tmux 3.2). SpiceEdit also wraps OSC 52 in tmux passthrough automatically when `$TMUX` is set, so this should mostly be a non-issue.

## OSC 52 isn't pasting into my local app

Even if SpiceEdit copies successfully, your *local* terminal has to be allowed to write to the system clipboard. Check the same setting above. Some corporate Macs disable clipboard access entirely via MDM; if `pbpaste` doesn't return your last copy, the issue is upstream of SpiceEdit.

## Indexing is slow

The fuzzy file finder uses `git ls-files` for git repos (~150 ms on 50,000 files) and falls back to a `filepath.Walk` on non-git projects.

**Slow on a git repo?** That's unusual. Run `git ls-files | wc -l` — if it's huge, see if you have committed `node_modules` or `vendor` directories that should be in `.gitignore`.

**Slow on a non-git project?** Check your `.gitignore`. SpiceEdit reads it during the walk and skips anything that matches. Add `node_modules`, `dist`, `build`, `.cache`, and any other large generated directories. The hardcoded ignore list catches the most common offenders, but a `.gitignore` is the canonical signal.

## `Ctrl+S` froze my terminal

That's XOFF — a real terminal flow-control signal. Press `Ctrl+Q` to release it (XON). SpiceEdit doesn't bind `Ctrl+S` for exactly this reason. Save with `Esc s` instead.

## Format on save isn't running

Three things to check:

1. Is `.spiceedit/format.json` in your project root? SpiceEdit looks for it relative to the project root, not the file being saved.
2. Does the file's extension match a key in `commands`? Keys are extensions *without* the leading dot.
3. Did you trust the config? On the first save with a new or edited config, SpiceEdit prompts. If you said No, every save in this project is a no-op until the config changes.

If the configured binary isn't installed, SpiceEdit silently does nothing on save — that's deliberate, so you don't have to install everyone's formatter to clone a repo. To debug, run the formatter command manually and confirm it works.

## Files I just created don't show up in the finder

The finder index refreshes every 10 seconds and immediately after any create / rename / delete *inside* SpiceEdit. If you create a file from another shell pane, wait up to 10 seconds, then reopen the finder.

## My terminal's color is off

SpiceEdit assumes 24-bit truecolor. Most modern terminals support it, but some older ones default to 256-color mode. Check `tput colors` — anything less than `16777216` means truecolor isn't on. Set `COLORTERM=truecolor` in your shell rc and reconnect.

## Image previews use blocks instead of terminal graphics

SpiceEdit uses native Kitty Graphics Protocol previews in Kitty, WezTerm, and Ghostty when it has a real terminal TTY with pixel cell dimensions. tmux, Zellij, SSH sessions, remote attachments, macOS Terminal, and unsupported terminals use the universal half-block preview; SpiceEdit does not attempt passthrough configuration.

## Herdr image previews are disabled

For a local Herdr pane, add the following to `~/.config/herdr/config.toml`:

```toml
[experimental]
kitty_graphics = true
```

Then run `herdr server reload-config`. If SpiceEdit still reports that graphics are disabled, restart the Herdr server. SpiceEdit never edits this file or reloads or restarts Herdr for you.

## Supported image formats and fallback

SpiceEdit previews PNG and JPEG images and the first frame of a GIF. If graphics preparation or presentation fails, SpiceEdit automatically uses the half-block preview.

## I want to see what `Esc t` does

The whole leader table is in the [Hotkeys](/docs/hotkeys/) doc. The action menu also shows the bound combo next to each item, so you can learn the table by using the menu first.
