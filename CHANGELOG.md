# Changelog

All notable changes follow [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

## [0.2.0] - 2026-08-16

### Added

- OKF v0.2 bundle: herdr-notes is now a producer and consumer of plain
  Markdown-with-frontmatter directories, not a single per-workspace file.
  Strict on write (refuses a document without a non-empty `type`), permissive
  on read per §11 — unknown types, keys, and broken links are reported, never
  rejected, and unknown keys survive a round trip intact.
- Scope model: `workspace`, `project`, and `global`. Project scope keys on
  the git repository (resolving linked worktrees back to their parent), so
  every agent on one project shares a page — the fleet-notes case.
- CLI surface: `ls`, `show`, `edit`, `append`, `log`, `search`, `links`,
  `backlinks`, `path`, `index`, `doctor`. `append` is the agent primitive:
  one command, no format knowledge, creates a conformant page if absent.
- Bundle-aware TUI: browse pages (`l`), follow links, view backlinks,
  type-to-filter, reload, and edit in `$EDITOR` — editing is delegated to the
  external editor so writes stay on the single CLI path.
- Live-refresh: the pane reloads a page when an agent appends to it from
  another pane, detected by mod time on the heartbeat tick.
- Existence-gated vault default: points at `openwiki/` only when that
  directory exists locally, otherwise falls back to plugin state, so no
  personal path leaks onto machines that don't have it.

### Fixed

- Pane identity is now the page path, not the workspace id, so toggling under
  project scope no longer opens a duplicate per workspace.
- The metadata token key is now schema-legal (`herdr-notes-page`); the
  earlier `herdr-notes.page` violated herdr's `^[A-Za-z0-9_-]{1,32}$` rule and
  silently dropped every heartbeat, so a toggle could never close.
- Memoized rendering: repeat renders went from ~2.6ms and 2.1MB to ~7.5ns and
  zero allocations on the hot path.
- Workspace ids no longer collapse punctuation (`w49:p1` ≠ `w49p1`), and
  workspace-scoped logs no longer land in the shared `workspaces/log.md`.

## [0.1.1] - 2026-08-13

### Fixed

- Release archives are now built with `-buildvcs=false`, making their bytes and
  source-pinned SHA256 values independent of checkout revision/dirty metadata.
- Cold Herdr pane startup now gets 15 seconds to publish its first heartbeat;
  the original 5-second handshake produced a false failure on a live cold run.
- Toggle focus now uses Herdr's plugin-pane focus API rather than changing zoom.

## [0.1.0] - 2026-08-10

### Added

- Bubble Tea scratch-note TUI with Glamour preview and terminal-adaptive colors.
- One canonical Markdown file per stable workspace ID, atomic debounced saves,
  and legacy JSON/fallback migration.
- Heartbeat-aware, serialized Herdr split toggle and optional external editing.
- Linux/macOS builds and CI cross-compilation for Windows.
