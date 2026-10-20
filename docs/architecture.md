# SQZARR Architecture (Current)

This document is the concise, current architecture reference for the running system.

## Runtime shape

- Single Go binary (sqzarr) serves API + embedded frontend.
- SQLite database in data_dir.
- ffmpeg/ffprobe are external runtime dependencies.
- Primary deployment target is Linux (systemd), with macOS launchd support.

## Core components

- API server: HTTP routes under /api/v1 and static frontend assets.
- Scanner: periodic directory scan; applies per-directory rules.
- Queue worker: executes transcode jobs, updates progress/state.
- Transcoder: hardware probe + ffmpeg invocation with software fallback.
- Verifier: confirms output validity before replacing source.
- Originals workflow: processed originals moved to per-root processed dir, with retention/restore controls.
- Integrations: optional Plex refresh after successful replacement.
- `POST /api/v1/plex/verify` checks saved Plex settings through the existing API
  authentication middleware. It reads `/library/sections`, validates the XML
  container, and returns only `library_count`. Disabled/incomplete configuration
  returns 400; upstream failures return 502 with sanitized messages (Plex 401/403
  do not log the user out of SQZARR). Requests use a 10-second timeout, bounded
  response decoding, header-only tokens, and no redirects. Saving Plex settings
  replaces the worker notifier for subsequent refreshes without a restart.

## Data and state

### Transcoding policy

`[transcoder] hardware_decode` and `low_power` accept `auto` or `off`, defaulting
to `auto` for existing configurations. Startup and the runtime settings API apply
these policies through `Transcoder.SetPolicy`; each job snapshots its policy.
`GET /api/v1/config` reports both policies and `low_power_status` as `off`,
`unqualified` or `qualified` from the transcoder's host qualification check.
`PUT /api/v1/config` rejects invalid policy values before changing settings.

Automatic VAAPI decoding is restricted to qualified H.264 8-bit SDR inputs.
The transcoder probes the actual production chain and retains software decoding
for other input classes or unsupported chains. Low-power VBR requires a trusted
host qualification manifest matching the device/software/input class/settings,
plus a successful runtime chain probe. Without qualification it stays unavailable
even in `auto` mode: a fast CQP encode is not proof of equivalent bitrate-controlled
behavior. No implicit CQP switch or host-name-specific selection is permitted.

Settings exposes a small hardware-decoding override. Per-job diagnostics report
the actual selected pipeline and fallback; a configured `auto` value alone does
not claim that hardware decoding succeeded.

- DB: SQLite (sqzarr.db) with migrations applied on startup.
- Primary entities:
  - directories (scan roots + rules)
  - jobs (queue + history)
  - originals (held originals for review/retention)
  - scan_runs (scan audit)
  - stats (aggregates)
- Config file: sqzarr.toml (TOML), editable via Settings API and UI.

## Request/processing flow

1. Scanner walks configured directories.
2. Candidate files are evaluated by age, size, bitrate, and margin rules.
3. Eligible files are enqueued as jobs.
4. Worker starts ffmpeg transcode with selected encoder.
5. Output is verified for integrity and savings criteria.
6. Original handling:
   - keep transcoded output
   - move original to processed_dir_name
   - retain for originals_retention_days
7. Job and aggregate stats are updated; optional Plex refresh is triggered.

## API surface (high level)

- Status/stats: service health, encoder, queue summary.
- Jobs: list/filter, enqueue, cancel, retry, clear terminal history.
- Directories: CRUD, batch create, browse filesystem, enqueue by directory.
- Originals review: list, delete (accept), restore, restore+exclude.
- Settings: read/update runtime configuration.
- Auth: optional password-based admin auth.

## Frontend

- React + TypeScript app embedded in backend binary.
- Main pages:
  - Dashboard
  - Queue
  - History
  - Directories
  - Review
  - Settings

## Safety and operational guarantees

- No Docker runtime dependency.
- Per-job temp-space checks prevent unsafe transcodes.
- Output verification before source replacement.
- Restore path for held originals.
- Path validation/allowlisting for filesystem-facing API calls.

## Script entry points (repo root)

- build_linux.sh: local Linux target build.
- deploy.sh: frontend build + source sync + remote rebuild/restart.
- build_now.sh: convenience wrapper for deploy.
- deploy.sh.example: template for machine-local wrapper.

## Source-of-truth docs

- Product/ops usage: README.md
- Container/GPU runtime notes: docs/container-deployment.md
- This file: current architecture overview
