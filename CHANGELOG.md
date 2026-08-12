# Changelog

All notable changes follow [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

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
