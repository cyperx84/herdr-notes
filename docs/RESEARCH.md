# Implementation research

The initial implementation reviewed the public behavior of
`alexarthurs/herdr-notes` (README, manifest, state/launcher architecture and
history), Herdr issue #10, and the local `herdr-loop` / `herdr-sesh-bro`
manifests and safe bootstrap pattern. No Rust implementation was copied.

## Decisions

- Preserve one stable-workspace note and the compact preview/edit interaction.
- Use canonical Markdown rather than JSON state; do not persist UI mode.
- Use Glamour and terminal-adaptive styling rather than a fixed custom palette.
- Hash unsafe workspace IDs instead of sharing a singleton fallback.
- Serialize toggles and stamp pane metadata before/while live to avoid both
  rapid-toggle duplicates and restored-label corpses.
- Keep processes pane-owned: no detached startup daemon.
- Treat Neovim as the default optional external editor, represented as argv.

## Honest limits

macOS is the local live-verification platform. Linux is exercised in CI once
hosted. Windows is cross-compiled with platform-specific named-pipe and atomic
replacement code, but native Herdr integration is not yet live-verified. Herdr's
split API controls exact final geometry. See README for migration ambiguity and
concurrent external-editor behavior.
