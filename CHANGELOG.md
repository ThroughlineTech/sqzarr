# Changelog

## 2026-05-09

### Bug fixes
- **Robust hardware encoder detection and fallback** — NVENC probing now validates CUDA device initialization, VAAPI probing is hardened, and failed hardware transcodes automatically retry with software encoding to avoid hard job failures (TKT-011)
- **Clearer hardware diagnostics in logs** — startup logging now reports detected/selected encoders more explicitly, making container misconfiguration easier to diagnose (TKT-011)

### Deployment improvements
- **Container passthrough guide expanded** — added generic GPU passthrough guidance for Intel, AMD, and NVIDIA, including unprivileged LXC mapped-group troubleshooting and service `SupplementaryGroups` guidance for `/dev/dri` access (TKT-011)

## 2026-04-09

### New features
- **Space saved detail page** — dashboard stat is now clickable, links to `/savings` with per-file breakdown showing original size, new size, savings, and percentage (TKT-001)
- **Interactive dashboard stats** — Jobs Done and Failed cards link to filtered history views; History page has status filter pills (TKT-002)
- **Bitrate skip margin** — scanner skips files already within a configurable margin of the target bitrate, avoiding unnecessary re-encodes. Default 10% (TKT-003)
- **Directory presets** — copy settings from an existing directory to pre-fill the add form; batch-add multiple directories at once via a single API call (TKT-005)
- **Encoder selection** — Settings page shows all detected hardware encoders + software fallback; select and switch encoders at runtime without restarting (TKT-008)
- **Password management UI** — set, change, or remove password from the Settings page instead of CLI-only `sqzarr hash-password` (TKT-009)

### Bug fixes
- **Files no longer reappear in queue after processing or exclusion** — introduced durable `processed_files` table that tracks processed/excluded files independently of job history. Clearing history no longer re-queues files on the next scan (TKT-004)
- **Clear history now includes completed jobs** — `done` and `staged` jobs are cleared alongside failed/cancelled, with processed_files preserving re-queue prevention (TKT-006)
- **Permission denied errors correctly classified** — I/O errors (permission denied, disk full, network errors) now show as red "Error" badges instead of being silently marked "Excluded" (TKT-007)
- **Config file writer no longer corrupts bcrypt hashes** — `$` characters in TOML values were being interpreted as regex backreferences; fixed with literal string replacement
- **Login page handles password-less mode** — redirects to dashboard when no password is configured instead of showing a dead-end login form

## 2026-04-07

### Improvements
- Persist pause state to `sqzarr.toml` so it survives restarts
- Harden scanner against edge cases
- UI improvements across Settings, Directories, and Queue file picker

## 2026-04-06

### Initial release
- Go backend with SQLite, React/Vite frontend embedded in binary
- VAAPI, VideoToolbox, NVENC, and software encoding
- Filesystem scanner with configurable per-directory bitrate thresholds
- Job queue with pause/resume and consecutive failure auto-pause
- Output verification (duration match, size reduction)
- Originals retention with review/restore/delete workflow
- Plex library refresh integration
- JWT authentication (optional)
- systemd and launchd service support
- CI/CD workflows, cross-platform release builds

