# Adaptive transcoding release outcome

Completed 2026-10-20. The same locally built release is installed and validated
on both hosts; CT261 on pve5 is the active production instance.

## Scope and implementation

One binary supports automatic H.264 8-bit SDR VAAPI decoding, standard HEVC VBR,
and a separately quality-qualified low-power VBR path. Unsupported input classes
retain software decoding. The production target/max/buffer remain
2300/4000/4600 kbit, with async depth 2 and the existing scale filter.
No CQP substitution, CPU policy changes, or higher queue concurrency are included.

Per-job encoder and policy snapshots replace shared fallback mutation. Bounded
capability probes/cache, bounded diagnostics, unique scratch outputs and
selective retries protect concurrent jobs and failures. Scratch media is excluded
from scanning and manual enqueue. Cancellation after verification prevents
replacement. Invalid/nonfinite durations fail verification. Explicit stream maps
preserve primary video, audio/subtitles and attachments.
Reserved scratch outputs explicitly retain the previous `0644` media permissions;
the final Linux regression and application staging checks cover readability by
Plex running under another UID.

Low-power selection requires a root-owned, non-group/world-writable qualification
manifest matching the device/software/kernel/input-class/settings fingerprint and
a successful current-boot production VBR probe. Updating those dependencies
invalidates qualification; an ordinary reboot triggers a fresh runtime probe.
The tracked `scripts/vaapi-qualification` helper only emits a candidate record,
never an automatic quality endorsement.

## Team and review

- Implementation lead: integration, verifier/cancellation safety, local builds,
  isolated real-host tests, sequential deployment and this report.
- Pipeline worker: `internal/transcoder` planning/probes/attempts/tests.
- Product worker: configuration, API/UI, scanner scratch protection, docs and
  quality-validation tooling.
- Independent parent review: concurrency, fallback classification, output safety,
  qualification trust, settings compatibility, docs and secret scanning.

Existing Plex verification and unrelated workspace changes were preserved.
No commit or push was requested or performed.

Subsequent delivery: the user requested commits and a push. The release and
documentation were pushed as `88fa3ee` and `7f5443c`.

### Production qualification correction

A subsequent production job ran near 7.5× because the actual systemd service
rejected the qualification record and selected standard VBR. The validation
shell resolved FFmpeg/ffprobe through `/bin`; the service PATH resolved them
through `/usr/bin`. These are the same files on this container, but the
fingerprint includes the executable path string. The isolated application
validation therefore did not detect this production-environment mismatch.

The original qualified stack fingerprint was rechecked, both executable aliases
were verified to identify identical files, and only the record fingerprint was
regenerated using the running service's PATH. The prior qualification time,
input class and settings were retained, with a private backup alongside the
record. The production `/api/v1/config` then reported `low_power_status` as
`qualified`. No service restart, reboot or job cancellation was performed.
This check confirms production qualification; it is not a new throughput
measurement. Jobs already planned retain their selected pipeline.

The durable code fix now resolves executable symlinks before fingerprinting.
The Linux regression verifies that PATH aliases preserve qualification while
a different executable installation invalidates it. Local Go vet/build and the
Linux transcoder suite passed. Both `/bin`-first and `/usr/bin`-first PATHs matched
the existing qualified record before installation, and the restarted production
service reported `qualified` afterward. Queue state was restored to unpaused.
CT260 was updated offline and remained stopped.

Current installed SHA-256 on both hosts after this correction:
`fce00d7e6c15b4cb82e8d9c74dcd40ea15c3d694e82b173133f74e4e16c02920`.
Encoding arguments and subtitle metadata handling are unchanged.

## Completed validation

- Local frontend TypeScript/Vite production build and `go vet ./...` passed.
- Linux binaries and integration-tagged tests were compiled on the workstation.
  No compiler ran in either media container.
- CT261 API/config/database/Plex/queue/rename/scanner/transcoder/verifier suites
  passed, including integration-tagged tests and concurrent eight-job fallback.
- The same nine integration-tagged suites, isolated staging/restoration and
  saved-output quality harness also passed on real CT260 on pve2.
- Real CT261 isolated application validation passed: standard/software-decode
  and automatic-decode outputs, stream preservation, full output decode,
  cancellation cleanup, queue staging, retained-original hash, and restoration.
- Three pre-existing integration fixtures were corrected: requested ABR on simple
  HEVC/AV1 test patterns did not guarantee a bitrate above scanner thresholds;
  fixtures now assert actual bitrate. The verifier size-gate fixture now uses
  deterministic files above its 1 MiB floor. Corrected Linux reruns passed.
- Standard pve5 quality checks passed for the approximately 124-second real sample, generated
  motion/dark/grain clips, multiple audio/subtitle tracks with metadata and
  dispositions, and unsupported VP9 hardware decoding. Frame counts, durations,
  dimensions, meaningful color metadata and stream signatures matched.
  Baseline/automatic SSIM deltas were 0 to +0.000008. Real sample and multistream
  decoded pixels matched exactly. Truncated input failed without changing source
  bytes or leaving scratch output.
- Race instrumentation was unavailable on this Windows workstation (no C compiler
  available). Linux concurrency behavior tests passed; this is not a race-detector
  run.

## Host preparation

Both CT260 and CT261 were backed up and raised from 512 MiB to 1 GiB RAM;
512 MiB swap and two-CPU allocations were preserved. CT260 now explicitly pins
physical cores 2 and 3 after checking host topology, matching CT261's allocation.
Host available RAM before the
change was approximately 7.5 GiB on pve2 and 6.2 GiB on pve5.
The production queue was originally unpaused, then paused and confirmed drained.
Only one container owned the shared IP at a time throughout validation.

Private host backups: `/var/lib/vz/sqzarr-release-20261020`.
Approved HuC proposal/rollback files are under that directory's `huc-proposal`
subdirectory on pve5. The approved isolated `enable_guc=2` option was applied,
initramfs regenerated and pve5 rebooted. HuC 7.9.3 reported `RUNNING` with status
`0x00090001`. All previously running guests returned (two VMs and twelve
containers); SQZARR and Plex services were active.

## Low-power qualification so far

Explicit saved-output tests confirmed `EncSliceLP` and VBR, retaining production
2300/4000/4600 settings. Structural checks passed. SSIM changes versus standard:

| Sample | SSIM change | Standard / LP container kbit/s |
|---|---:|---:|
| Real approximately 124-second sample | +0.000514 | 2732.7 / 2760.3 |
| Motion | -0.000013 | 2254.1 / 2234.4 |
| Dark | -0.000001 | 2142.4 / 2129.2 |
| Grain | +0.012031 | 2257.5 / 2371.3 |
| Multiple audio/subtitle tracks | +0.000084 | 1920.3 / 2063.4 |

All were inside the predeclared maximum SSIM regression of 0.002. Container
bitrates include copied audio/subtitles where present and are not instantaneous
video VBV measurements. These short samples do not establish universal
perceptual equivalence.

Automatic approval review initially rejected production manifest activation
because the earlier maintenance approval covered enabling HuC and testing.
No manifest was installed at that point. The user then explicitly approved
production activation **after sustained tests pass**. Both full-input pve5 runs
and strict full-output decode passed, followed by root-owned `0600` manifest
activation and an actual application low-power staging/restoration smoke test.

## Completed pve5 full-input comparison

The identical local source was 3551.936 seconds, 2051466334 bytes, SHA-256
`eff454f2d6db4af2bf986f9dbb818a6581311bf37c4fede77feca2de7dec6761`.
Other guests remained active. These saved-file runs are not directly comparable
with historical null-output benchmarks.

| Measurement | Standard/software decode | Qualified LP/GPU decode |
|---|---:|---:|
| Internal seconds (Transcoder.Run / FFmpeg benchmark respectively) | 492.994 | 128.520 |
| Throughput, launcher-inclusive | 7.16× | 27.26× |
| Process CPU, 100% = one core | 141.76% | 31.99% |
| Process peak RSS, KiB | 168440 | 132464 |
| Package peak temperature | 93°C | 79°C |
| Host package thermal-throttle counter increase | 26 | 0 |
| Output bytes | 1280270724 | 1282317715 |

The LP path was about 3.8× faster with approximately 0.16% larger output.
The speed comparison uses consistent launcher timing; the internal timers have
different scopes (application transcode call versus FFmpeg's own benchmark).
Input and output duration matched; the LP output retained 85154 video packets
and 110998 audio packets. Both complete outputs passed strict software decode.
The LP full-input encode lasted about 2.1 minutes, not a five-minute thermal soak.
Cgroup memory-high events occurred (10242/10047), with about 0.32/0.35 seconds
full memory PSI; no max/OOM/OOM-kill events occurred. The 1 GiB allocation
therefore avoided OOM but did not eliminate memory reclaim pressure.
Selected grain stills showed no obvious new crop/color/block artifacts;
this and SSIM are supporting checks, not universal perceptual guarantees.
Independent review of representative 60-second real-source/standard/LP stills
also found no obvious additional crop/color/block defect in LP; face, stage and
audience detail looked broadly comparable to standard. Both are lossy relative
to source, and still inspection does not establish equivalent motion quality.

On pve2, the same bounded low-power HEVC VBR probe failed before output:
`No usable encoding entrypoint found for profile VAProfileHEVCMain (17).`
No host firmware settings were changed there. Its automatic policy uses supported
GPU decoding with standard HEVC VBR and retains the software-decode fallback.

## Completed pve2 full-input comparison

The same source and standard HEVC VBR settings ran in real CT260, with both
complete outputs passing strict software decode.

| Measurement | Software decode | Automatic GPU decode |
|---|---:|---:|
| Transcoder.Run seconds | 524.931 | 446.385 |
| Throughput, launcher-inclusive | 6.723× | 7.900× |
| Process CPU, 100% = one core | 188.67% | 26.84% |
| Process peak RSS, KiB | 149272 | 129468 |
| Package peak temperature | 80°C | 75°C |
| Host package thermal-throttle counter increase | 73 | 42 |
| Output bytes | 1278102049 | 1278102049 |

Automatic decoding improved throughput by 17.5% while reducing CPU use by about
86%. Both outputs retained the source duration. Memory-high events decreased
from 12043 to 10087 and full PSI from 0.586 to 0.324 seconds; no max/OOM/OOM-kill
events occurred. Thermal counters remained nonzero, so this does not establish
that pve2 is free of throttling. Other guests stayed active throughout.
On this matched source, optimized pve5 throughput was about 3.45× optimized pve2.

The offline state transfer initially exposed a permissions mismatch: both guests
use explicit unprivileged UID/GID mappings (container root to host 100000), while
host extraction created root-owned files. This was detected before accepting
service health and corrected only on the copied configuration, state, artifacts
and binary after checking both mappings. The deployment documentation now
explains offline mapped ownership. Private old-state backups remain available.

## Final deployment and recovery

Both hosts passed the final targeted Linux transcoder regression and real
application `0644` staging/retained-original restoration check. No encoder
arguments changed in this final permission fix, so sustained tests were not
repeated for it.

| Final state | pve2 CT260 | pve5 CT261 |
|---|---|---|
| Container | Stopped | Running |
| Autostart | Disabled | Enabled |
| Encoding policy | Automatic GPU decode, standard VBR | Qualified GPU decode and LP VBR |
| Production queue | Standby | Unpaused, restored to original state |

Initial release application SHA-256 (before the path correction above):
`f50b02653c772972181278e10273fbea5c1178fd59fad5d9754bd95df18e5393`.
The application's reported version remains `1.0.0`; use the hash to identify this
uncommitted release precisely.

Final SQZARR health passed, Plex verification reported 18 libraries, and the
Plex service in CT331 was active. All previously running pve5 guests were running.
The qualification record remains root-owned `0600` on CT261 and was excluded
from cross-host state copies. After future kernel/driver/FFmpeg changes, standard
VBR remains available while LP requires renewed matching qualification as
documented in `docs/container-deployment.md`.

No database migration was introduced. Prefer a binary-only rollback, preserving
current job history. On pve5, stop SQZARR inside CT261, restore the original
binary, then start the service:

```sh
pct exec 261 -- systemctl stop sqzarr
pct exec 261 -- install -m 0755 /opt/sqzarr-release-20261020/before-install/binary /usr/local/bin/sqzarr
pct exec 261 -- systemctl start sqzarr
```

For an LP-specific issue, set `low_power="off"` through settings/configuration
and retain the new release's standard VBR path. Host HuC rollback is the prepared
`huc-proposal/rollback.sh` under the private host release directory and requires
a coordinated maintenance reboot; do not reboot automatically for an app issue.

Private recovery material:

- pve5 CT261: `/opt/sqzarr-release-20261020/before-install` contains original
  binary/config/stopped-service data; `/var/lib/sqzarr.before-return-20261020`
  retains state immediately before the final cold state return.
- pve2 host: `/var/lib/vz/sqzarr-release-20261020/260-before-state.tar.gz`
  contains the original CT260 binary/config/data, alongside `260-before.conf`.
  Extract the desired binary to a private temporary location for binary-only
  rollback, observing the documented unprivileged UID mapping if offline.
- Both hosts: `/var/lib/vz/sqzarr-release-20261020` contains private state
  snapshots and deployment evidence. Restore whole database snapshots only as
  coordinated recovery; doing so later would discard intervening job state.

The final independent secret scan covered 118 tracked/candidate files and found
zero findings; whitespace checks passed. Secrets/private state backups were not
added to the repository. No commit or push was performed.
