# Adaptive transcoding release: implementation, tests, and dual-host deployment

## Objective and authorization

Deliver one SQZARR release that selects a validated efficient pipeline on pve2
and pve5, preserves output safety, and reports its actual selected path.
The user requested implementation, tests, deployment to both hosts, use of
subagents, and a reviewable outcome report. Execute this plan, not only research.

Read `WORKFLOW.md`, `README.md`, `docs/architecture.md`,
`docs/container-deployment.md`, and `docs/transcode-investigation-results.md`.
Existing uncommitted work includes the Plex connection verifier and investigation
documentation. Preserve all unrelated changes and include the intended existing
feature in the resulting build. Do not commit or push unless separately asked.

The investigation measured substantial CPU savings with hardware decoding on
both hosts, a 20% full-input speed improvement on pve2, and no reliable standard
VBR speed improvement on pve5. pve5 low-power CQP was fast but is NOT an approved
substitute for bitrate-controlled output. Do not promise a 31x production result.

## Team responsibilities

The implementation lead owns delivery and must delegate at least:

1. A worker owning transcoder planning, capability probing, retries and tests.
2. A worker owning configuration/API/UI diagnostics, docs or build-validation
   tooling, assigned nonoverlapping files after inspecting the repository.

The lead integrates, reviews, builds locally, executes tests remotely, and
deploys sequentially. Workers must be told they share the workspace and must not
revert others' edits. An independent review must cover retry isolation, output
safety, cancellation, credential handling and capability selection. Track exact
files and responsibilities so edits do not collide.

## Product behavior

### Capability-driven plans

- Keep existing encoder selection compatible. Add a small, documented policy
  surface: automatic hardware decoding with an explicit off override, and
  low-power encoding `auto/off` (or equivalent clearly validated controls).
  Preserve existing config files through defaults/migration.
- Prefer GPU-resident decode for qualified H.264 8-bit SDR inputs after an
  actual bounded decode/filter/encode probe succeeds.
- Do not choose paths by hostname or advertised CPU generation.
- For other codecs/profile/depth/color classes retain the safe existing path
  until a real capability probe and correctness test qualify that class.
- Handle HDR/10-bit explicitly: do not introduce an implicit conversion to the
  tested SDR NV12 path. Preserve existing behavior where out of scope and report
  unsupported optimization clearly.
- Standard production settings remain HEVC VBR, 2300k target, 4000k maxrate,
  4600k buffer, existing scaling and async depth 2. Do not remove the passthrough
  scaler or increase async depth without new evidence.
- Prefer low-power VBR only when the exact production chain succeeds AND its
  quality/correctness has been qualified. Without that, use standard VBR.
  Never silently substitute CQP for requested VBR.
- Cache probe results by device, FFmpeg/driver identity and relevant input
  class/settings. Bound cache growth and probe duration; allow explicit refresh
  or restart invalidation. Device/driver changes must not retain stale success.
- Do not run expensive benchmarks on every queued file.

### Job isolation and failure handling

- Snapshot configuration/encoder state for each job. Remove fallback's mutation
  of the shared `Transcoder.encoder`; protect concurrent configuration changes.
- Use an immutable sequence of eligible attempts, e.g. GPU decode/qualified
  low-power VBR -> GPU decode/standard VBR -> software decode/hardware VBR ->
  existing software fallback, skipping inapplicable or duplicate attempts.
- Retry only appropriate capability/device errors. Cancellation, disk full,
  permission failures and invalid source must not cause indiscriminate retries.
- Bound retries, reset progress coherently, clean only owned scratch files, and
  never expose incomplete output as a successful result.
- Retain original media until existing verification/replacement gates succeed.
  Preserve rollback if staging/moving files fails.
- Preserve intended video/audio/subtitle streams, metadata, dispositions,
  timestamps and color characteristics; explicitly test multi-stream cases.
  Avoid unrelated stream-selection changes without validation.
- Bound captured FFmpeg error/log buffers; drain pipes continuously.

### Diagnostics and operator experience

- Show/log the actual decode and encode path, hardware device and fallback reason
  per job, with a concise automatic-mode status. Do not flood the UI with raw
  hardware knobs or require users to understand HuC to run normal jobs.
- Report unavailability accurately (e.g. low-power VBR unavailable; standard
  hardware encoder selected). Capability failures are not false success.
- Preserve secret redaction and Plex-verification functionality.
- Keep any UI change small and consistent with existing settings/job views.

## Validation gates

1. Unit tests for source eligibility, argument ordering before `-i`, unchanged
   VBR settings, capability invalidation, timeout/cancellation, bounded retries,
   per-job isolation, cleanup, status reporting, malformed inputs and config
   compatibility. Test behavior, not copies of implementation details.
2. Regression tests across existing Go packages and frontend build/type checks.
   Run race testing where a suitable local environment exists; if unavailable,
   document that limit and perform concurrency-focused behavior tests.
3. Real-host tests using locally built binaries and sample copies:
   qualified H.264 SDR; unsupported hardware-decode input; standard VBR fallback;
   failed low-power VBR; cancellation and restart; multi-audio/subtitle media;
   corrupted/truncated source; source retention/replacement on disposable files.
4. Output validation: successful decode, aligned frame count/duration, dimensions,
   pixel/color metadata, stream retention, bitrate and representative visual or
   SSIM quality. Use additional motion/dark/grain content if available. Do not
   label one checked segment universal quality parity.
5. Repeat paired baseline/optimized performance tests on both hosts using equal
   local bytes and production resource limits. Record CPU, throughput, memory
   pressure, temperatures and throttle events. Include at least one full-input
   or >=5-minute wall-time run per host when practical.
6. Never treat missing telemetry as zero or failed encodes as fast results.
   Record internal FFmpeg versus launcher elapsed time separately.

## Resources and host configuration

- Preserve current cpufreqd/TLP policy and `intel_pstate=disable`; do not revisit
  BIOS/power tuning. CT261's firmware profile is balanced; the old custom
  performance-profile service is disabled. Correct stale WORKFLOW descriptions.
- Back up CT261 resource config and raise RAM from 512 MiB to 1 GiB initially,
  provided host headroom allows. Preserve swap/CPU pinning. This addresses
  observed enforcing-parent cgroup memory pressure, not an assumed speed gain.
  Validate memory.events/pressure during real jobs and document final sizing.
- Apply and validate an appropriate equivalent memory allocation for deployed
  CT260 so the release is tested under real container limits, not merely chroot.
- Keep queue concurrency at one for qualification. Do not change other guests'
  resource limits or stop them for benchmarks.

### HuC boundary

Do all code/build/standard-pipeline work without requiring HuC changes.
Read-only firmware/capability checks and failed LP VBR probes are authorized.
If a host reboot/firmware change is needed to qualify low-power VBR, prepare a
concrete backed-up change and request the maintenance-window approval before
changing host firmware/module policy. This affects Plex and other guests, unlike
an ordinary SQZARR service deployment. Do not let this optional optimization
block delivery of the fully working standard-VBR release to both hosts.
Report low-power VBR as unavailable/unqualified if this phase is not performed.

If authorized later: preserve existing i915 settings, add only the appropriate
HuC-loading request for the running kernel, rebuild initramfs, reboot, confirm
authentication, validate actual LP VBR entrypoint, output quality and sustained
behavior, check Plex/all guests and retain rollback. Do not enable GuC submission
or raw power overrides as an incidental change.

## Build and deployment procedure

Build frontend, release and Linux package-test binaries on the Windows
workstation using explicit Git Bash and the repository's documented Go cross-
compilation flow. Never compile in the media containers (SQLite previously OOMed).
Execute compiled tests in isolated remote release directories before installation.
Do not use the legacy remote-compiling deployment scripts.

Known access:

- `ssh -l root pve2.throughlinetech.net`; CT260 on pve2.
- `ssh -l root pve5.throughlinetech.net` or hop from pve2 to `pve5`; CT261.
- Service `sqzarr`, binary `/usr/local/bin/sqzarr`,
  config `/etc/sqzarr/sqzarr.toml`; API port 8080.
- CT260 and CT261 currently share IP/MAC and media library. Never start both.
- Current production owner is CT261. Final owner should remain CT261; CT260
  retains the release installed/validated and is stopped with autostart disabled.

Before any switch:

1. Verify storage health, mount availability, package/device configuration and
   exact service data paths. Do not print secret config contents.
2. Capture queue state, pause/drain, back up binary/config/database privately on
   the hosts, and stop services before SQLite/state synchronization.
3. Keep an inventory of the original container resources, autostart settings,
   binary hashes and application version. Use consistent database backups;
   include WAL considerations. Never copy live SQLite files naively.
4. Install/test on CT261 first, verify health and a disposable end-to-end job.
5. For live CT260 validation, stop CT261 and disable its autostart before starting
   CT260. Synchronize current state/config safely with services stopped and
   backups retained. Deploy the identical binary and validate the real service,
   frontend, probes, output and memory limits.
6. Drain/stop CT260 and disable its autostart. Carry any state changes made during
   validation back safely or use an explicitly isolated disposable job/database
   fixture so production state is not forked. Restore CT261 as sole owner,
   restore its autostart and original queue state, and verify final health.
7. If a stage fails, restore the prior working binary/config/state as needed,
   return to the original active host, and report exactly what did not deploy.

Deploying to both does not mean simultaneously active processing against a
shared library. Do not create a second production identity/IP without asking.

## Documentation and outcome report

Update README, architecture/deployment documentation, example config and
WORKFLOW to reflect implemented behavior and current host settings.
Record tests and deployment evidence in
`docs/transcode-product-release-outcome.md`, including:

- Implemented features and explicit exclusions.
- Files changed and subagent responsibilities.
- Test commands/results, failures resolved and remaining limitations.
- Per-host software/device capabilities, selected pipeline and fallback.
- Baseline/optimized performance, CPU/memory/thermal data, quality evidence.
- Actual low-power VBR status; no CQP speed advertised as production VBR.
- Installed release SHA-256 and version on each host.
- Database/config/binary backup and rollback locations (no secrets).
- Final service, queue, autostart, IP ownership, memory and mount state.
- Security review: no credentials in changed/committable files or logs.
- Decisions still requiring user review.

The task is complete only when the standard optimized release is built, tests
have executed successfully, both real container deployments are validated,
production is restored on CT261, and the report accurately describes the outcome.
If access or approval blocks a required step, report it explicitly; do not mark
that step complete or substitute a compilation/chroot-only check for deployment.
