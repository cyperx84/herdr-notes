# herdr-notes

A focused scratch note beside your agents: one canonical Markdown file for each
stable Herdr workspace, with a rendered terminal preview and fast editing.

This is a Go reinterpretation of
[alexarthurs/herdr-notes](https://github.com/alexarthurs/herdr-notes), aligned
with CyperX's Go Herdr plugins. It was implemented from observed behavior and
public interfaces—not by porting or copying its Rust source.

## Features

- Bubble Tea TUI; Glamour Markdown preview using the terminal's palette/profile.
- Preview-first: `e`/`Enter` edit, `Esc` preview and save, `Ctrl+S` save.
- `x` clears only after `y` confirmation; `q` saves and quits.
- Preview scrolling with arrows, Page Up/Down, `g`/`G` (plus Home/End).
- Atomic, fsynced, 1.2-second debounced saves to plain `<workspace>.md`.
- Optional `notes_dir`; editor argv configuration; Neovim-first `E`/CLI action.
- Serialized, heartbeat-aware toggle that opens/focuses/closes one right split,
  and replaces stale or label-only panes restored without a live process.

## Install / develop

```sh
git clone https://github.com/cyperx84/herdr-notes
cd herdr-notes
./scripts/build.sh
herdr plugin link .
```

All manifest commands are plugin-relative (`./bin/herdr-notes`). The bootstrap
matches the local-first/fail-closed policy used by `herdr-loop` and
`herdr-sesh-bro`: it prefers an exact source build and otherwise downloads only
a release archive whose SHA256 is pinned in `scripts/build.sh`.

Invoke **Toggle workspace notes** from Herdr's plugin actions. CLI usage:

```sh
herdr-notes --workspace w1                 # TUI
herdr-notes --workspace w1 --path          # canonical file
herdr-notes --workspace w1 --external      # configured editor (nvim default)
herdr-notes --workspace w1 --toggle        # Herdr pane toggle
```

A workspace ID is intentionally mandatory. Running without one returns an error
instead of allowing unrelated workspaces to overwrite a shared fallback note.

## Storage and migration

The default directory is `HERDR_PLUGIN_STATE_DIR`, or the platform user config
directory's `herdr/herdr-notes` when run independently. Set `notes_dir` in the
plugin config (environment form: `HERDR_NOTES_NOTES_DIR`) to override it.
Normal IDs produce `<id>.md`; unusual IDs get a deterministic hash filename so
path traversal is impossible without collapsing distinct workspaces together.

At startup, when canonical Markdown is absent, the loader checks old plugin
state and config fallbacks including `<id>.json`, `note.json`,
`herdr/notes/<id>.json`, and `herdr/notes.json`. JSON `{ "text": ... }` is
converted to Markdown. It writes canonical state first, then renames the source
with `.migrated`; canonical files always win. Migration is deliberately
one-workspace-at-a-time because the old singleton fallback cannot identify its
original workspace.

Editor configuration is a JSON argv array, not a shell fragment:

```toml
editor_argv = "[\"nvim\", \"-f\"]"
notes_dir = "~/notes/herdr"
```

## Toggle correctness

The duplicate-pane demand is evidenced by
[`herdr-logbook` issue #10](https://github.com/Resetnak/herdr-logbook/issues/10),
while the upstream `herdr-notes` launcher independently converged on the same
toggle lifecycle. This plugin exposes one stable toggle action for that surface.
The action globally lists panes, scopes by canonical note identity, and chooses
OPEN/FOCUS/CLOSE/REPLACE. A live TUI stamps `herdr-notes` metadata every five
seconds. A restored `Notes` label without that token, or a token over 20 seconds
old, is a restart corpse and is replaced rather than focused.

A per-note atomic lock directory serializes rapid/concurrent actions, addressing
the startup race where two toggles could both observe no pane and open
duplicates. Herdr owns the split process lifecycle; no detached daemon or
startup process is created, so there are no restart corpses outside pane state.
The declarative pane is requested with `direction = "right"`; exact edge docking
and dimensions remain subject to Herdr's current layout behavior.

## Research comparison and limits

Behavior reviewed from the upstream README, manifest, launcher/state design,
release history, `herdr-logbook` issue #10, and adjacent `herdr-scratch` lifecycle
patterns. Kept: preview/edit ergonomics, confirmation, atomic autosave,
per-workspace identity, and heartbeat corpse detection. Changed:
plain Markdown is canonical; preview uses Glamour; mode is not persisted; unsafe
IDs remain isolated; external editing is first-class; Go/Bubble Tea replaces the
upstream stack.

Development and live TUI behavior were verified on macOS. CI tests Linux and
cross-compiles Darwin/Linux/Windows. Windows builds are feasible and include
named-pipe IPC plus replace-existing atomic writes, but native Windows Herdr pane
behavior has **not** been live-verified. Accordingly the manifest currently
advertises only macOS/Linux; cross-compilation is not a support claim.
Glamour's output depends on terminal capabilities, and external concurrent file
writers remain normal last-writer-wins behavior; the toggle prevents duplicate
plugin TUIs, not unrelated editors.

## Quality

```sh
gofmt -w .
go vet ./...
go test -race -count=1 ./...
```

MIT licensed. See [CONTRIBUTING.md](CONTRIBUTING.md) and
[SECURITY.md](SECURITY.md).
