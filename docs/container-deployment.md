# SQZARR Container Deployment Guide

Running SQZARR in a containerized environment requires careful configuration to enable hardware acceleration. This guide covers common container runtimes and how to pass through the necessary devices.

## Overview

SQZARR supports three hardware encoders for HEVC transcoding:
- **VAAPI** (Intel/AMD Linux) — requires `/dev/dri/renderD128`
- **VideoToolbox** (Apple Silicon) — native, no device pass-through needed
- **NVENC** (NVIDIA) — requires NVIDIA CUDA runtime
- **Software Fallback** — libx265, works on any system but slower

If hardware devices are not available or passed through to your container, SQZARR automatically falls back to software encoding. Check the startup logs to see which encoder was selected.

## Quick Reference

| GPU vendor | Primary nodes to pass through | Encoder path |
|-----------|-------------------------------|--------------|
| Intel | `/dev/dri/renderD128` (+ optional `/dev/dri/card0`) | VAAPI (`hevc_vaapi`) |
| AMD | `/dev/dri/renderD128` (+ optional `/dev/dri/card0` or `/dev/dri/card1`) | VAAPI (`hevc_vaapi`) |
| NVIDIA | `/dev/nvidia0`, `/dev/nvidiactl`, `/dev/nvidia-uvm`, `/dev/nvidia-uvm-tools` | NVENC (`hevc_nvenc`) |

In unprivileged LXC, visibility of device files is not enough. You must also ensure the sqzarr service process has a supplementary group matching the mapped device group.

## Linux GPU Device Primer (All Container Runtimes)

- **Intel and AMD (VAAPI):** both use `/dev/dri` nodes
   - required: `/dev/dri/renderD128`
   - sometimes needed: `/dev/dri/card0` or `/dev/dri/card1`
- **NVIDIA (NVENC/CUDA):** uses `/dev/nvidia*` nodes
   - usually needed: `/dev/nvidia0`, `/dev/nvidiactl`, `/dev/nvidia-uvm`, `/dev/nvidia-uvm-tools`
   - userspace libraries must be present in the container runtime environment (CUDA/NVENC libs)

## Proxmox LXC Containers (Unprivileged)

### Prerequisites
- Host with Intel or AMD GPU (iGPU or dedicated)
- Proxmox 6.0+ with LXC support

### Configuration

1. **Edit `/etc/pve/nodes/{nodename}/lxc/{vmid}.conf` on the Proxmox host:**

   Intel/AMD VAAPI example:

       features: nesting=1
       lxc.mount.entry: /dev/dri/ dev/dri/ none bind,optional,create=dir
       lxc.cgroup2.devices.allow: c 226:* rwm

   NVIDIA NVENC example:

       features: nesting=1
       lxc.mount.entry: /dev/nvidia0 dev/nvidia0 none bind,optional,create=file
       lxc.mount.entry: /dev/nvidiactl dev/nvidiactl none bind,optional,create=file
       lxc.mount.entry: /dev/nvidia-uvm dev/nvidia-uvm none bind,optional,create=file
       lxc.mount.entry: /dev/nvidia-uvm-tools dev/nvidia-uvm-tools none bind,optional,create=file
       lxc.cgroup2.devices.allow: c 195:* rwm
       lxc.cgroup2.devices.allow: c 511:* rwm

   Notes:
   - The old `dev0: ...,allow_cgroup_access=1` syntax is not accepted on newer Proxmox schema versions.
   - Use `lxc.mount.entry` plus `lxc.cgroup2.devices.allow` for predictable behavior.

2. **Restart container:**

       pct stop {vmid}
       pct start {vmid}

3. **Verify device visibility inside container:**

       ls -la /dev/dri
       ls -la /dev/nvidia* 2>/dev/null || true

4. **Verify actual access (not just visibility):**

       head -c 1 /dev/dri/renderD128 >/dev/null && echo ok || echo fail

### Troubleshooting Unprivileged LXC

**Device not visible in container:**
- Ensure `features: nesting=1` is set
- Check that the device path is correct (`renderD128` for Intel/AMD VAAPI)
- Verify host permissions: `ls -la /dev/dri/*` or `ls -la /dev/nvidia*`

**Device visible but still permission denied (common in unprivileged LXC):**
- Check numeric ownership inside container:

         ls -ln /dev/dri/renderD128

- In unprivileged containers, host GIDs are remapped. The effective group may not be `video` or `render` by name.
- Run SQZARR with a supplementary group matching the mapped device GID.

   Example if mapped group is `postdrop` (gid 104):

         # /etc/systemd/system/sqzarr.service
         User=root
         Group=root
         SupplementaryGroups=postdrop

         systemctl daemon-reload
         systemctl restart sqzarr

- Re-check startup logs for VAAPI detection after restart.

**VAAPI detection fails but device is present:**
- Intel iGPU: install Intel VAAPI driver on host and verify host-side with `vainfo`
- AMD iGPU/dGPU: install Mesa VAAPI stack on host and verify host-side with `vainfo`
- Then verify inside the container by probing ffmpeg VAAPI init.

## Other OCI Runtimes (Generic)

For OCI-compatible runtimes, the same rule applies:
- pass through `/dev/dri/*` for Intel/AMD VAAPI
- pass through `/dev/nvidia*` for NVIDIA NVENC/CUDA
- ensure container process group membership matches device group ownership

Example shape (runtime-agnostic):

      --device /dev/dri/renderD128
      --device /dev/dri/card0
      --group-add <mapped-video-group>

NVIDIA example shape:

      --device /dev/nvidia0
      --device /dev/nvidiactl
      --device /dev/nvidia-uvm
      --device /dev/nvidia-uvm-tools

## Verifying Hardware Encoder Selection

Check the startup logs to see which encoder was selected:

   # Proxmox LXC
   pct exec {vmid} journalctl -u sqzarr -f

   # Look for lines like:
   # "encoder available" type=vaapi
   # "encoder selected" "Intel VAAPI (hevc_vaapi)"

If you see "Software (libx265)", then:
1. Hardware device was not detected
2. Check device pass-through configuration (steps above)
3. Verify ffmpeg has hardware encoder support: `ffmpeg -encoders | grep hevc`
4. In unprivileged LXC, verify mapped group access for the device node and set `SupplementaryGroups` accordingly

## Fallback Behavior

If SQZARR starts with hardware acceleration but the device becomes unavailable during transcode (e.g., container migration, device hot-removed), SQZARR will:
1. Detect the device error
2. Log a warning: "Hardware encoder failed with device error, retrying with software encoder"
3. Automatically retry the transcode using software encoding
4. Complete successfully (but slower)

This ensures robustness in dynamic container environments.

## Performance Notes

See the [2026-10-20 transcoding investigation](transcode-investigation-results.md)
for controlled non-looped comparisons, GPU-engine measurements, hardware-decode
and low-power capability tests, quality checks, and sustained results. The
measurements below remain the history of the earlier configurations.

### Comparing Proxmox hosts with the same service IP

Keep the original container stopped and disable its autostart before starting
a test copy with the same IP/MAC. Preserve the application binary, configuration,
database, CPU/RAM allocation, and worker concurrency for the initial comparison.
Validate hardware encoding with ffmpeg, media create/read/delete access, service
health, and Plex connectivity before resuming the queue. Compare the same source
clip and encoder settings on each host; different media storage paths (local
versus NFS) can affect throughput independently of GPU speed.

The retained container's database becomes stale once the test copy processes
media. To switch back, pause and drain the active copy, stop SQZARR, and transfer
its current application data (including SQLite state) and configuration back to
the retained container while both services are stopped. Keep copies of the old
state. Stop the test container before starting the retained container, validate
it, and restore the intended queue and autostart settings. Do not run both copies
against the same IP and media library.

### pve5 slowdown investigation (2026-10-19)

A controlled test reproduced the reported slowdown using the same 720p H.264
sample, SQZARR's HEVC VAAPI bitrate/filter settings, local input, and null output:

- pve2 (i5-8350U), using CT260's userspace while its service/network remained
  stopped: 6.03x realtime on two sibling threads and 6.49x on two physical cores.
- pve5 (i7-1185G7), CT261: repeated 90-second default-path tests achieved
  1.73x and 1.68x realtime. Short tests varied substantially with host conditions.
- During a slow pve5 test, `throttle_reason_pl1=1` and
  `throttle_reason_status=1`; the requested GPU clock was 1350 MHz while the
  actual clock was 700 MHz. The MMIO package sustained power limit was 8 W,
  subsequently returning to 15 W after the tests. The separate MSR interface
  reported 64 W, so inspecting only that interface misses the lower limit.
- The firmware platform profile was `balanced`, AC was online, USB-C advertised
  20 V / 3.25 A, and lap mode was off. No userspace power-management service was
  active. The exact firmware trigger for the changing MMIO cap is not yet proven.
- Only the benchmark ffmpeg process held the GPU during monitored tests.
  Hardware thermal-throttle flags were clear and thermal counter deltas were
  zero. CT261 had no OOM events, swap usage, or CPU quota throttling.
- NFS was not the primary bottleneck: local input reproduced the slowdown,
  a 36 MB source sample copied in 0.77 seconds, and a separate temporary NFS
  output test completed and decoded successfully.
- CT261's assigned CPUs were 3 and 7, sibling threads of one physical core.
  Software decoding is used by the current SQZARR VAAPI command. Hardware
  decoding and thread/async-depth changes sometimes improved short tests, but
  results varied; these are not validated remedies for the host power cap.
- HuC was disabled. Low-power HEVC with the application's bitrate settings
  failed because the driver exposed only CQP for that path. Do not enable
  `-low_power 1` blindly. Looping the sample also caused hardware-decoder filter
  reinitialization failures; failed runs were excluded from speed comparisons.

The evidence points primarily to firmware/package power throttling, compounded
by other CPU workloads sharing the package budget. Next, compare the supported
firmware `performance` profile against `balanced` while monitoring MMIO limits,
actual GPU clocks, and temperatures, restoring the original profile afterward.
This is a host-wide experiment, not a container resource adjustment. No power,
firmware, CPU-affinity, or application encoder settings were changed during the
investigation. Avoid overriding raw power limits before validating firmware
policy and cooling. Linux documents these interfaces in the
[power-capping guide](https://www.kernel.org/doc/html/latest/power/powercap/powercap.html)
and [platform-profile guide](https://docs.kernel.org/userspace-api/sysfs-platform_profile.html).

#### Performance profile and physical-core pinning follow-up

At the user's request, pve5 was switched to `performance` and CT261 was pinned
to logical CPUs 2 and 3 (different physical cores), retaining two logical CPUs
and 512 MiB RAM. An enabled `pve-performance-profile.service` applies the profile
at boot before guest autostart; a CT261 systemd drop-in orders its startup after
the profile service. The service was started/restarted successfully and CT261's
effective cpuset was checked after a container restart and the benchmarks.
The host itself was not rebooted for this verification. See WORKFLOW.md for paths.

Three consecutive tests used the same local sample, unchanged application
encoding settings, and 90 seconds of video per run. Each completed successfully
with 2,152 frames and approximately 89.965 seconds of output:

| Run | Wall time | Realtime speed | Peak package temperature |
|-----|-----------|----------------|--------------------------|
| 1 | 13.52 s | 6.66x | 100°C |
| 2 | 21.04 s | 4.28x | 99°C |
| 3 | 34.57 s | 2.60x | 74°C |

The first run showed no GPU PL1 throttling and reached 1100 MHz actual GPU
frequency. Thermal-throttle counters increased during the first two runs.
The MMIO package limit was 12 W throughout; by the third run GPU PL1 throttling
was present almost continuously and actual GPU frequency peaked at 500 MHz.
This improves on the prior 1.68–1.73x result, but the initial 6.66x is not a
sustained-performance claim. Firmware power policy and cooling still constrain
longer workloads. Raw power limits, GPU firmware, and application encoder
settings were not overridden. The queue's previous pause state was restored.

To reverse this tuning, remove CT261's explicit cpuset line and restart it after
draining its queue. Remove its `performance-profile.conf` systemd drop-in,
disable `pve-performance-profile.service`, reload systemd, and explicitly select
`balanced` in `/sys/firmware/acpi/platform_profile`; disabling the service alone
does not undo the currently selected profile. Pre-change host/container settings
were backed up under `/var/lib/vz/sqzarr-pve5-test` on pve5.

#### pve2 thermal comparison and pve5 fan inspection

CT260 remained stopped. Its existing ffmpeg/userspace was exercised in an
isolated mount namespace on pve2, pinned to physical cores 2 and 3, without
starting its service or network. The media bind was read-only; the test encoded
the same local sample to null output. These are offline encoding measurements,
not a restart of the original production container.

| Run | pve2 speed | Peak package temperature | Package thermal-counter increase |
|-----|------------|--------------------------|----------------------------------|
| 1 | 6.28x | 80°C | 115 |
| 2 | 6.39x | 81°C | 300 |
| 3 | 6.17x | 73°C | 0 |

All runs completed with the same 2,152 frames / 89.965 seconds as the pve5
comparison. pve2's MMIO sustained package limit stayed at 15 W; GPU clocks
reached 1100 MHz, with sampled means around 908–968 MHz. Package thermal events
occurred, but throughput remained stable rather than declining across runs.
These counters cover the host package, including other workloads. The newer
GPU throttle-reason sysfs attributes were unavailable on this older GPU, so
absence of GPU power throttling was not established directly. Samples were
removed and CT260's filesystem unmounted afterward.

pve5's fan state was `auto` (`pwm1_enable=2`), with fan readings of 7462 and
7481 RPM. It was not manually locked to high. A PWM reading of 255 in automatic
mode is not proof of actual maximum output: the ThinkPad driver returns its
stored desired manual level in that mode. See the
[ThinkPad driver implementation](https://github.com/torvalds/linux/blob/master/drivers/platform/x86/lenovo/thinkpad_acpi.c).
No fan settings were changed. The performance profile remains enabled.

#### pve5 retest after storage recovery (2026-10-19)

After pve2's disconnected USB storage was reattached and the host rebooted,
both ZFS pools reported healthy, the EXT4 backup mount was active, and NFS
was running. CT260 stayed stopped. Repeating the same local-sample benchmark
in CT261 with the performance profile and effective CPU set `2-3` produced:

| Run | Elapsed | Speed | Peak package temperature |
|-----|---------|-------|--------------------------|
| 1 | 19.48 s | 4.62x | 65°C |
| 2 | 19.19 s | 4.69x | 66°C |
| 3 | 19.31 s | 4.66x | 67°C |

Each run completed 2,152 frames / 89.965 seconds with exit status zero.
Package thermal counters did not increase. The MMIO sustained package limit
remained 15 W, while the GPU PL1 throttling flag was active for nearly all
samples. Actual GPU clocks peaked at 750 MHz, with sampled means around
599–610 MHz, versus requests near 1350 MHz. Both fans ran around 6,000 RPM
in automatic mode. This retest was stable but still about 26% lower throughput
than the earlier pve2 mean of 6.28x; it indicates ongoing power limiting
without new thermal events during these tests. It does not establish
long-duration sustained performance or isolate other host workloads.
The queue was restored to unpaused and the scratch sample removed.

#### Retest after BIOS power-management changes (2026-10-19)

With user-reported SpeedStep and CPU power management disabled and AC/battery
schemes set to maximum performance, the same three tests passed at 3.83x,
3.67x, and 3.87x (23.48, 24.50, and 23.23 seconds). Average throughput was
3.79x, about 19% below the preceding 4.66x result. All produced 2,152 frames.
Peak temperatures were 52/53/55°C, with no new package thermal events or
asserted GPU PL1 flags. The package limit remained 15 W; GPU clocks peaked
at 1350 MHz in the first two runs. Fans stayed around 4,570 RPM.

A CPU-policy snapshot during encoding showed all logical CPUs near 1200 MHz,
`scaling_max_freq=1200000`, `no_turbo=1`, and the performance governor.
ffmpeg used about 177% CPU. This suggests CPU-side decoding/frame preparation
is now constrained; the test still uses software H.264 decoding. Other host
VM load was present, so these runs do not isolate the contribution of each
BIOS change. Re-enable SpeedStep and CPU power management while retaining
the maximum-performance thermal scheme, then verify CPU boost and retest.
No further power settings were changed by the benchmark. The queue was
restored to unpaused and the scratch sample removed.

#### Follow-up retest with CPU boost restored (2026-10-19)

The next identical three-run benchmark passed at 7.07x, 7.07x, and 7.08x
(12.73, 12.74, and 12.71 seconds), each completing 2,152 frames. The effective
CPU set remained `2-3` and the platform profile remained `performance`.
During encoding, CPU2/3 read approximately 4.3 GHz, `no_turbo=0`, and
`scaling_max_freq=4800000`. These observations confirm CPU boost was restored;
the individual BIOS settings were not read back.

GPU clocks averaged approximately 1000 MHz and peaked at 1100 MHz. The GPU
PL1 flag stayed clear despite the MMIO limit still reading 15 W. Peak package
temperatures increased across runs: 85/91/95°C, with host-wide thermal-counter
increments of 1/32/129. Fans rose from about 5970 to 6570 RPM in automatic
mode. Throughput remained stable across these short tests, averaging about
13% above the earlier pve2 average and 87% above the CPU-capped retest.
This does not establish sustained performance over a full movie: rising
temperature and thermal events warrant a longer test before that claim.
Other host VM activity was present. The queue was restored to unpaused and
the scratch sample removed.

#### CPU policy audit: pve2, pve3, and pve5

Read-only inspection found broken auto-cpufreq installation remnants on both
pve2 and pve3: `/usr/local/bin/auto-cpufreq --version` failed with
`ModuleNotFoundError: No module named 'auto_cpufreq'`. Neither host had an
auto-cpufreq service or running process, or a config in the checked standard
`/etc` locations.

Both older hosts instead run cpufreqd with identical `/etc/cpufreqd.conf`:
two-second polling; AC selects `Performance High` with min/max 100% and the
performance governor. A hot-CPU rule (55–100°C and 50–100% CPU) requests
the 80% `Performance Low` profile. Battery bands request 80/60/40%.
These are configured rules, not confirmation that every sensor rule matches.
TLP 1.8.0 also applies at boot, requesting `conservative` on AC with boost
enabled and `balance_performance` energy bias; cpufrequtils likewise requests
`conservative`. The observed live governor was `performance` on all CPUs,
consistent with cpufreqd's AC rule. These overlapping policies should not
be copied wholesale.

Both boot with `intel_pstate=disable` and use `acpi-cpufreq`, with boost
enabled. Reported policy min/max were 1.7 GHz on pve2 and 2.0 GHz on pve3,
but `/proc/cpuinfo` snapshots showed about 3.5–3.6 and 3.9–4.0 GHz
respectively; policy values must not be treated as measured turbo clocks.
pve5 uses active `intel_pstate`, performance governor/EPP, turbo enabled,
and a 0.4–4.8 GHz policy range, plus the persistent platform performance
service. No auto-cpufreq, cpufreqd, or TLP executable was found on pve5.
This audit did not change any host settings or establish that switching
CPU drivers would improve encoding performance.

#### Current pve5 policy: matched to pve2/pve3

At the user's request, pve5 was changed to the reference configuration and
rebooted. `/etc/cpufreqd.conf`, `/etc/default/cpufrequtils`, and `/etc/tlp.conf`
match pve2 and pve3 byte-for-byte (SHA-256 verified). Package versions match:
cpufreqd 2.4.2-3, cpufrequtils/libcpufreq0 008-2, and TLP 1.8.0-1.
The older CPU packages came from pve2's cached Debian packages; TLP and
required dependencies came from pve5's configured Debian repositories.

`intel_pstate=disable` was appended to pve5's existing GRUB arguments,
preserving its display settings, and `update-grub` completed. After reboot,
the live driver is `acpi-cpufreq`, cpufreqd and TLP are active, the CPU governor
is `performance`, boost is enabled, and energy-performance bias is 4
(`balance_performance`). The hardware-specific policy min/max reads
3001000 kHz; this is a policy request, not a measured turbo clock.

The previous custom `pve-performance-profile.service` is disabled and CT261's
dependency drop-in for it was removed. The platform profile now reads
`balanced`; the CPU governor separately reads `performance`. CT261's physical
core pinning remains `2-3`. All previously running guests returned after the
normal reboot. SQZARR's queue was restored to unpaused.

Three identical 90-second sample encodes completed successfully:

| Run | Speed | Elapsed | Peak package temperature | Thermal-counter increase |
|-----|-------|---------|--------------------------|--------------------------|
| 1 | 7.07x | 12.73 s | 84°C | 0 |
| 2 | 6.93x | 12.98 s | 75°C | 0 |
| 3 | 6.70x | 13.43 s | 69°C | 0 |

Each produced 2,152 frames. GPU PL1 flags stayed clear and the MMIO limit
read 15 W throughout. Mean throughput was 6.90x versus the previous 7.07x,
with lower observed peak temperatures and no new thermal events. This is a
short test, not an isolated driver comparison: the platform profile changed
too, and other host workloads were running.

Backups and the reference configs/packages are on pve5 under
`/var/lib/vz/sqzarr-pve5-test/cpu-policy-match/`; the original GRUB file,
custom profile service/drop-in, package inventory, and guest state are in
`before-20261020T063250Z`. To reverse the driver change, restore the saved
GRUB file, run `update-grub`, disable the added CPU-policy services, restore
and enable the saved custom profile service/drop-in, and reboot after draining
the queue. Review package removal separately; no automatic removal is needed.

#### Post-update comparison (kernel 7.0.14-25-pve)

After the user updated and rebooted pve2, pve3, and pve5, all three reported
the new kernel and active cpufreqd/TLP, with `acpi-cpufreq`, performance
governor, and boost enabled once startup completed. pve2's pools were healthy.
The same three 90-second tests ran on pve2 first, then pve5; CT260 stayed
stopped and pve5's queue state was restored to unpaused afterward.

| Host | Run speeds | Mean speed | Peak temperatures | Thermal-counter increments |
|------|------------|------------|-------------------|----------------------------|
| pve2 | 6.39x / 6.51x / 6.38x | 6.43x | 80 / 81 / 75°C | 119 / 355 / 0 |
| pve5 | 7.07x / 6.58x / 6.69x | 6.78x | 87 / 79 / 68°C | 58 / 0 / 0 |

All six runs passed with 2,152 frames / 89.965 seconds. pve5 averaged about
5.5% higher throughput than pve2 in this comparison. Its firmware profile
remained balanced and CPU pinning remained `2-3`. GPU PL1 flags were clear
in its first two runs and briefly asserted in the third (rounded sample
mean 0.0, maximum 1). Both hosts' MMIO limits stayed at 15 W. pve2's older
GPU did not expose the same throttle flags. Early pve2 fan telemetry included
0/65535 readings and should not be interpreted as reliable fan-speed evidence.
Host-wide thermal counts include other workloads. These short tests do not
isolate the update's effect or establish full-movie sustained throughput.

- **VAAPI (Intel):** 5–20× faster than software, depending on hardware
- **NVENC (NVIDIA):** 10–50× faster, excellent quality
- **Software (libx265):** 0.2–2× realtime speed (depends on CPU cores and file complexity)

For typical 1080p HEVC files on a modern i5 or i7, hardware encoding takes minutes; software encoding takes 1–4 hours.

## Adaptive transcoding release operations

The execution specification is [transcode-product-release-plan.md](transcode-product-release-plan.md).
Use [transcode-product-release-outcome.md](transcode-product-release-outcome.md)
for the installed artifact hashes, completed checks, final low-power qualification,
and measured performance. The earlier benchmark sections above are historical
observations, not release acceptance results.

Build the frontend, Linux release and Linux test executables on the workstation.
Run those test executables inside the target containers; do not compile on the
media server. `WORKFLOW.md` describes the commands. A successful cross-compilation
does not establish that the tests pass on Linux or that GPU access works.

CT261 on pve5 is the production owner. CT260 on pve2 is the retained standby.
They share the application IP/MAC and media library: **never run both containers
at once**. Validate a release on each real container sequentially, then leave
CT261 running with autostart enabled and CT260 stopped with autostart disabled.
Validation in a mounted root filesystem alone does not exercise the container's
GPU permissions or memory limits.

Both containers were allocated 1024 MiB RAM for this release, retaining their
existing swap and CPU assignment. Check effective cgroup memory limits,
`memory.events` and `memory.pressure` during sustained tests. A lack of OOM kills
does not prove that memory reclaim is absent. pve5 retains physical-core pinning
to CPUs 2 and 3, the balanced firmware profile and the cpufreqd/TLP policy matched
to pve2/pve3. Do not re-enable the superseded custom performance-profile service.

Before deployment or switching the active host:

1. Confirm storage/mount and GPU availability. Record queue state, pause the
   queue and drain active jobs.
2. Stop the service before backing up or copying its database and configuration.
   Preserve consistent SQLite state, including any WAL requirements. Store
   binary/config/database and container-config backups privately on the hosts;
   never copy credentials into the repository or publish configuration contents.
3. Install the same locally built artifact on each host and verify SHA-256.
   Exercise health, served frontend, Plex verification and a disposable
   end-to-end transcode under the real container limits.
4. Stop CT261 and disable its autostart before starting CT260. Synchronize
   application state with both services stopped. Use isolated fixtures for
   validation so test jobs do not alter production library state.
5. Stop CT260 and disable its autostart before restoring CT261. Verify mounts,
   service health and sole IP ownership, then restore the original queue state.
   A later failback needs fresh state synchronization; the standby database
   becomes stale as production continues.

For offline copies into an unprivileged container's mounted root filesystem,
inspect its actual `lxc.idmap` before restoring file ownership. Container root
may map to a host UID such as 100000; host-root-owned state can be unreadable
inside the container. Preserve the original backup's numeric ownership and
correct only the explicitly copied paths using the verified mapping. Extracting
workstation artifacts with `--no-same-owner` inside a running container is
different from restoring application state into its filesystem from the host.

Automatic hardware decoding probes the actual eligible H.264 8-bit SDR
decode/filter/encode chain. It is distinct from low-power HEVC encoding.
Low-power VBR must pass its exact bitrate-control probe and output-quality
qualification before selection; CQP throughput is not equivalent evidence.
Keep per-job pipeline/fallback logs available for diagnosis.

The user approved a scoped pve5 HuC maintenance step for this release:
`options i915 enable_guc=2`, current-kernel initramfs rebuild and reboot.
The private proposal, backups and apply/rollback scripts are at
`/var/lib/vz/sqzarr-release-20261020/huc-proposal` on pve5. Consult the outcome
report for whether it completed and qualified low-power VBR. This is a
HuC-loading request, not authorization to enable GuC submission or change power
limits. Future maintenance needs its own impact review and approval.

### Requalifying low-power VBR

Qualification is an administrator's attestation of completed output testing,
not a result inferred from successful encoder initialization. A software,
kernel or GPU identity change invalidates the saved qualification. A routine
reboot with the same stack preserves the attestation but clears runtime probe
results; the production chain must succeed again.

To reproduce qualification, first compare standard and low-power VBR using the
same local source bytes, GPU and production resource limits. Require explicit
`-low_power 1 -rc_mode VBR` with the production bitrate/scaling settings. Confirm
the actual low-power entrypoint and VBR in diagnostics. Check full output decode,
frame count/duration, retained streams/dispositions and color metadata. Compare
bitrate and SSIM against standard encoding on representative motion, dark and
grain samples, then run a sustained encode. This release uses an SSIM loss limit
of 0.002 against the standard output's source-relative score on every fixture;
that numerical gate supports, but does not replace, visual review or establish
quality for untested HDR/10-bit inputs. Retain the results and review them before
creating an attestation.

Build the record generator **locally**:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -o dist/vaapi-qualification ./scripts/vaapi-qualification
```

Transfer it into the container and run it there with the same `PATH` and runtime
as SQZARR. The tool prints candidate JSON to stdout and a reminder to stderr; it
does not execute tests or install anything. Generating JSON is not qualification.
Executable paths are resolved through symlinks before fingerprinting, so
`/bin/ffmpeg` and `/usr/bin/ffmpeg` identify the same executable on merged-/usr
systems. Still use the service's PATH: genuinely different installations must
not share qualification. An interactive-shell check alone is insufficient.
After installation, require
the production service's `/api/v1/config` to report `low_power_status` as
`qualified`, then confirm the selected pipeline in its next eligible job log.
After all gates pass, save its output privately, review the recorded identity,
and install it as root at `/etc/sqzarr/vaapi-low-power-qualified.json` with owner
`root:root` and mode `0600`. The service must be able to read the record.

Do not copy a record from another host or generate it against host libraries
instead of the container's libraries. A matching record permits selection only
for the qualified H.264 8-bit SDR class, and only after the runtime chain probe
succeeds. To disable qualification, remove/privately archive the record or set
`[transcoder] low_power = "off"`, then restart the service or apply the policy
through its settings API. Keep records out of Git.

## Debugging

Enable debug logging for more detail:

      # Proxmox LXC
      pct exec {vmid} /bin/bash -c "SQZARR_LOG_LEVEL=debug /usr/local/bin/sqzarr serve"

Debug logs show:
- All encoders probed and their results
- Device paths tested (e.g., `/dev/dri/renderD128`)
- Why a probe failed (device not available, permission denied, etc.)
- Which encoder was finally selected

## Further Reading

- [VAAPI Documentation](https://github.com/intel/media-driver)
- [NVIDIA Video Codec SDK](https://developer.nvidia.com/video-codec-sdk)
- [FFmpeg Hardware Encoding](https://trac.ffmpeg.org/wiki/Encode/HEVC)
