# Proxmox HEVC Transcoding Research

pve2 i5-8350U versus pve5 i7-1185G7

20 October 2026 | Research and proposed experiments

## Contents

- [1 Findings and the first three tests](#1-findings-and-the-first-three-tests)
- [2 What changed in the encoding hardware](#2-what-changed-in-the-encoding-hardware)
- [3 FFmpeg defaults and comparability](#3-ffmpeg-defaults-and-comparability)
- [4 Ranked hypotheses](#4-ranked-hypotheses)
- [5 How to read the telemetry](#5-how-to-read-the-telemetry)
- [6 Policy precedence and observed decisions](#6-policy-precedence-and-observed-decisions)
- [7 Read-only inventory and telemetry](#7-read-only-inventory-and-telemetry)
- [8 Common benchmark protocol and exact commands](#8-common-benchmark-protocol-and-exact-commands)
- [9 Prioritized experiment matrix](#9-prioritized-experiment-matrix)
- [10 Sustained-test design](#10-sustained-test-design)
- [11 Output quality and correctness](#11-output-quality-and-correctness)
- [12 Documented issues and limits of the conclusion](#12-documented-issues-and-limits-of-the-conclusion)
- [13 Sources and version boundaries](#13-sources-and-version-boundaries)

> The measured 5.5% advantage is a result for one short, CPU-fed pipeline. It does not establish the maximum HEVC capability of either GPU. First identify the encoder path, then test GPU-resident decoding and queue depth. Preserve the current host policy while resolving those questions.

Scope: research and a proposed test plan. Host measurements below come from the supplied brief, not new remote measurements. No host tests, package installations, policy changes, firmware changes, reboots, or media edits were performed. Commands are for a later operator-run session inside the existing safe benchmark environments.

## 1 Findings and the first three tests

| Reported latest result | pve2 | pve5 |
| --- | --- | --- |
| CPU | Core i5-8350U | Core i7-1185G7 |
| Three reported speeds | 6.39x, 6.51x, 6.38x | 7.07x, 6.58x, 6.69x |
| Arithmetic mean speed | 6.4267x | 6.7800x |
| Mean external elapsed time | 14.0067 seconds | 13.2833 seconds |
| Pooled frames / external second | 153.64 fps | 162.01 fps |
| Peak package temperatures | 75-81 C | 68-87 C |
| Reported MMIO package PL1 | 15 W | 15 W |
| Benchmark execution | Offline CT260 userspace in chroot | Running CT261 |

Calculated from the supplied results: pve5 has 5.50% higher mean reported speed, and its mean elapsed time is 5.16% shorter. Those percentages differ because speed and duration are reciprocals. The average wall-time difference is only 0.723 seconds. Wrapper overhead, workload variation, and three non-isolated samples therefore matter. Neither confidence intervals nor a causal hardware-generation effect can be established from these runs alone. [U]

- First: characterize one original run on each host. Capture FFmpeg/libva/iHD versions, selected HEVC entrypoint, rate-control mode, effective CPU affinity, and simultaneous CPU/GPU engine telemetry. This resolves whether the comparison is even exercising equivalent paths.
- Second: compare software decode/upload with VAAPI decode and GPU-resident frames using the same continuous local input. Eliminate runtime looping from both arms. This is the highest-value throughput experiment.
- Third: on the working GPU-resident path, compare async_depth 2, 4, and 8, keeping the extra hardware-frame pool fixed. This tests whether submission/synchronization depth hides the newer GPU's capacity.

If the first characterization already proves a specific limiting resource, prioritize the matching experiment instead of mechanically running the whole matrix. Before adopting any faster path, check sustained performance and saved-output quality.

## 2 What changed in the encoding hardware

| Property | Kaby Lake Refresh / UHD 620 | Tiger Lake / Iris Xe |
| --- | --- | --- |
| HEVC 8-bit path in Intel's driver matrix | PAK plus shader/media-kernel/VME encoding (Es) | Both that path (Es) and low-power VDEnc/HuC encoding (E) |
| Media parallelism | KBL GT2 platform definition has one VCS engine | Intel lists two codec engines; i915 defines VCS0 and VCS2 |
| Graphics maximum advertised frequency | 1.10 GHz | 1.35 GHz |
| Meaning for this test | Hardware encode can still involve shader execution | Additional media capacity is useful only if the selected pipeline can use it |

Intel documents two HEVC architectures: VDEnc plus PAK, and an alternative using media kernels on execution units with VME plus PAK. Execution units can therefore matter for the shader-assisted path. They do not determine the throughput of the complete decode, transfer, encode and synchronization chain. [S1-S3]

The two Tiger Lake media engines justify testing concurrent jobs. They do not promise twofold speed for one session. Verify actual engine enumeration and use on the hosts. The CPU specification and Linux platform table describe capability, not an observed scheduling decision. [S3-S5]

The 720p NV12 command exercises an 8-bit 4:2:0 workload common to both generations. It does not test most newer format capabilities. A fourfold increase in shader resources, a newer Core generation label, or a higher advertised graphics clock would not by itself establish a fourfold encoding gain.

The exact clock-domain relationship of every media block to the observed i915 MHz attributes has not been established for these two machines. Treat those samples as GT clock observations, not a universal clock for all media hardware.

## 3 FFmpeg defaults and comparability

- Record the selected entrypoint. In inspected FFmpeg 7 source, low_power defaults to 0, but its acceptable-entrypoint list still includes EncSliceLP. Setting low_power=1 restricts selection to LP. A missing flag, or low_power=0, is not proof that EncSlice was chosen. [S6-S7]
- Record the speed/quality target. The VAAPI control is compression_level; higher supported values favor speed over coding efficiency. When it is omitted, the inspected code does not send an explicit quality-level buffer. Driver defaults can differ. Do not substitute an assumed HEVC -quality option. [S7-S8]
- Record rate control and frame structure. The supplied bitrate/maxrate combination is intended for variable bitrate, but the actual selected mode must be logged. Inspected FFmpeg 7 HEVC defaults include GOP 120 and two B-frames; installed versions and driver restrictions remain authoritative. [S8-S9]
- Do not equate equal bitrate, compression_level, QP, or API entrypoint with equal perceptual quality. Record output video bytes and validate decoded output against the same source frames.
- Do not assume the no-op scaler is costly. Inspected FFmpeg 7 code passes through a frame when geometry, format and color properties need no change. The present 720p expression is a likely pass-through on such a build. Removal is a cheap check, not a leading diagnosis. [S10]
- The two iHD labels do not establish identical driver builds. Intel distinguishes full-feature and free-kernel builds with different codec support. Inventory the actual loaded binary and dependent library versions in each encoding userspace. [S1]

These source inspections describe FFmpeg 7 and current upstream documentation. They are not assertions about the uninventoried binaries in CT260/CT261. Local encoder help, verbose initialization output, package versions and loaded-library paths resolve the version boundary.

HuC is required for low-power bitrate control such as CBR/VBR in Intel's documented driver paths. The reported CQP-only success with HuC disabled is consistent with that prerequisite. It does not establish that missing HuC slows a successful non-LP encode. First observe actual firmware authentication and advertised capabilities. [S1]

## 4 Ranked hypotheses

Confidence below concerns whether a hypothesis is worth testing as a cause of the CURRENT narrow gap. It is not a probability estimate. Several explanations can operate together.

| Rank | Hypothesis | Current confidence |
| --- | --- | --- |
| 1 | CPU frame preparation and submission limit the two-CPU pipeline | Medium to high |
| 2 | Selected encoder path or hidden quality defaults conceal an advantage | Medium to high |
| 3 | Shared package power, cooling and other host work constrain pve5 | Medium now; strong historical evidence |
| 4 | One small stream and shallow queues underuse extra media capacity | Medium |
| 5 | Short-run measurement and unmatched execution environments obscure the gap | High as a confound; size unknown |
| 6 | cpufreqd/TLP decisions alter effective policy during a run | Low to medium |
| 7 | A specific current kernel, BIOS or media-driver defect dominates | Low without matching evidence |

### 1 CPU frame preparation or submission

Support: software H.264 decoding, pixel-format preparation and hwupload precede the encoder; only two logical CPUs are available. The historical 1.2 GHz state produced roughly 3.8x with about 177% FFmpeg CPU, while restoring turbo produced about 7.1x. Contradiction: that episode demonstrates CPU sensitivity under an artificial low ceiling; it does not prove current CPU saturation. [U]

Would weaken or falsify it as the dominant current limit: the GPU-resident path gives no meaningful FPS gain, one CPU versus two barely matters, per-thread CPU use is below saturation, and encode engines remain continuously busy. A lower CPU cost without higher FPS still has operational value.

### 2 Encoder path and quality defaults

Support: Tiger Lake adds an HEVC LP path not explicitly requested here, driver versions/builds are unknown, and compression_level is unspecified. Contradiction: the current command could already select LP or equivalent cost settings; only logs settle that. [S1, S6-S8]

Would weaken it: matching userspace and supported explicit settings preserve the narrow gap, logs prove the intended paths are already used, and quality-matched output comparisons reveal no hidden efficiency difference. A faster low-quality output does not count as a quality-preserving hardware gain.

### 3 Shared power, cooling and host work

Support: historical 8 W and 15 W limits, low actual clocks with PL1 asserted, and 99-100 C thermal episodes correlated with major slowdowns. CPU and graphics compete within mobile package constraints. Contradiction: the latest pve5 runs are much faster, have lower peaks, and have no new thermal events in two runs; PL1 was mostly clear. Historical throttling cannot be assumed to explain the remaining gap. [U, S11]

Would weaken it: long isolated runs show stable throughput, effective CPU clocks, package power and policy, no correlated reason flags, and the same gap through matched thermal states. A cool CPU snapshot alone does not falsify firmware power limiting.

### 4 Single-stream scheduling and synchronization

Support: one low-resolution stream may not fill the available hardware, and inspected FFmpeg defaults to async_depth 2. Tiger Lake has more media capacity. Contradiction: a single stream can still saturate its chosen encode path; engine utilization is not yet measured. [S3, S6]

Would weaken it: increasing working async depth does nothing, concurrent streams add little aggregate throughput, and the limiting engines remain saturated. If two jobs improve total throughput while individual speed falls, the result supports capacity that the single-stream benchmark did not expose.

### 5 Measurement and execution differences

Support: only three runs, roughly 13-14 seconds each, include different chroot/container launch overhead. Other guests were active. A taskset-limited chroot is not automatically equivalent to a cgroup-limited container. Contradiction: consistent successful frame counts and local input already remove important sources of noise. [U]

Would weaken it: alternating repetitions with identical local bytes, matched userspace/resource allowances and quiet host intervals yield the same internal FFmpeg and external-wall gap. Compare the two timing scopes before assigning 0.723 seconds to silicon.

### 6 Policy manager decisions

Support: cpufreqd, TLP and cpufrequtils overlap. An active 80% policy can remove turbo eligibility under acpi-cpufreq. Limitation: no contemporaneous active-profile or min/max transition has been demonstrated. Both the 100% and 80% profiles use performance, so that governor cannot distinguish them. A plain 50% aggregate CPU threshold is not normally met by this two-logical-CPU benchmark alone. [U, S12-S16]

Would weaken it: governor/min/max values and firmware profile stay unchanged throughout slow and fast intervals, with daemon logs showing no relevant transition. Do not infer changes from configuration text, sensor names, or cached getter output alone.

### 7 A specific bug

Support: historical project reports document Tiger Lake HEVC issues and an X1 Gen9 turbo/profile interaction. Contradiction: the versions, symptoms and active CPU driver do not match a demonstrated current fault; throughput recovered after prior changes. [S24-S25]

Would support it decisively: a minimal reproducer, matching known affected versions or a verified fix, and a controlled benchmark-userspace or maintenance A/B. A newer release number or a forum anecdote is not sufficient.

## 5 How to read the telemetry

| Observation | Valid interpretation | Needed discriminator |
| --- | --- | --- |
| 3.001 GHz acpi-cpufreq maximum | Can represent the entire turbo range: highest non-turbo state plus 1 MHz. | turbostat Bzy_MHz during the encode, plus effective limits. |
| Requested GT ~1340 MHz; actual ~1000 MHz | Request and sampled achieved clock differ. Neither is engine utilization. | Video/Render/VideoEnhance busy counters, client attribution and all limit reasons. |
| GPU PL1 flag | Indicates a reason active at the sampled instant; clear samples can miss brief events. | Time-correlated FPS, power, actual clocks and reason duty fraction. |
| 25 W MSR versus 15 W MMIO | The 25 W number alone does not establish the effective budget. Enabled interfaces and windows matter. | Named RAPL constraints, enable state, averaging windows and measured watts. |
| Thermal counter increments | Events occurred somewhere on that package; count is not time or lost throughput. | One package counter, per-core counters, total throttled time if exposed, idle/control intervals. |
| No reason attributes on pve2 | The inspected i915 code gates this group by graphics generation. | Use available power/thermal/engine metrics; absence is not zero throttling. |
| 65 W USB-PD; battery near full | Adapter capability does not determine the sustained CPU/GPU package limit. | Recheck AC/charging/PD state and the active firmware policy during the test. |
| Fans around 6570 RPM | Fan speed alone cannot establish thermal transfer capacity or the limiting policy. | Sustained temperature, power and throughput in repeatable placement/ambient conditions. |

The kernel documents the ACPI turbo sentinel and warns that policy/request frequencies need not equal running frequency. Bzy_MHz measures average clock while a CPU is busy. [S12, S17-S18]

i915 exposes requested and actual frequency through different reads and reports multiple possible limit reasons. Missing older-generation attributes must not be treated as negative evidence. Whole-host engine activity includes Plex; per-client DRM counters can help attribute activity, when exposed. Deduplicate drm-client-id across duplicated file descriptors. [S19-S21]

Do not sum package_throttle_count from every logical CPU: the same package event is broadcast. Prefer package_throttle_total_time_ms deltas where available, alongside event counts. [S22]

Read powercap zone/constraint names and enable states instead of assuming constraint_0 always means the desired limit. Package joules are obtained from energy deltas with wrap handling. A precise Tiger Lake MMIO-versus-MSR arbitration formula was not verified; the supported conclusion is that the MSR value alone is insufficient. [S11, S23]

## 6 Policy precedence and observed decisions

Balanced platform profile and performance CPU governor are separate, compatible selections. The platform-profile API selects firmware behavior involving power, temperature and fans; it does not certify an achieved speed. [S26]

- cpufreqd ranks matching rules by score/specificity and uses tie behavior; it is not simply a last-rule-wins configuration. [S13]
- In exact cpufreqd 2.4.2-3 source, unqualified cpu_interval=50-100 reads aggregate CPU accounting. Two fully busy logical CPUs are roughly 25% of eight; 177% process CPU is roughly 22%. Explicit CPU or ANY rules behave differently. Inspect the actual directive. [S14]
- If the temperature directive is unqualified acpi_temperature, that version averages discovered matching ACPI thermal zones. It does not necessarily read the hottest coretemp/package sensor. A sensors-plugin rule would need its own mapping. [S15]
- cpufreqd skips writing an unchanged profile. Its two-second polling does not prove it overwrites TLP every two seconds. The getter reports cached applied-profile objects; compare those with live sysfs. [S16]
- TLP applies settings on events such as boot, resume and power-source changes. Observe existing logs and effective values. Do not import newer TLP profile-daemon behavior into the supplied 1.8.0 installation. [S27]
- An 80% maximum based on a 3.001 GHz ACPI frequency table is roughly 2.4 GHz before rounding; it is not 80% of the advertised 4.8 GHz turbo maximum. Confirm the actual selected table entry before assigning a loss. [S12]

## 7 Read-only inventory and telemetry

Use tools already installed. A missing tool is an inventory gap, not a request to install it. Run host commands on the host; run FFmpeg/libva inventory inside the exact benchmark userspace. Keep CT260 stopped and reuse its established private-mount chroot. CT261 remains the only owner of its production IP.

### Host inventory

```bash
uname -a
cat /proc/cmdline
lscpu -e=CPU,CORE,SOCKET,ONLINE
lspci -nnk -s 00:02.0
readlink -f /sys/class/drm/renderD128/device
pveversion -v
pct status 260
pct status 261
pct config 261

systemctl status cpufreqd tlp cpufrequtils thermald \
  power-profiles-daemon auto-cpufreq --no-pager
systemctl cat cpufreqd tlp cpufrequtils
journalctl -b -u cpufreqd -u tlp -u cpufrequtils --no-pager
cat /etc/cpufreqd.conf
tlp-stat -s -p
cpufreqd-get -l
cat /sys/firmware/acpi/platform_profile
cat /sys/firmware/acpi/platform_profile_choices
```

Use only the relevant pct IDs present on that host. cpufreqd-get requires its control socket already enabled; if it fails, use existing logs and live policy values. Do not restart or enable a control interface for this inventory. Existing debug logs may identify the rule; the getter does not reliably expose an active-rule name. [S13, S16, S28]

```bash
for p in /sys/devices/system/cpu/cpufreq/policy*; do
  for a in scaling_driver scaling_governor scaling_min_freq \
           scaling_max_freq scaling_available_frequencies affected_cpus; do
    if [ -r "$p/$a" ]; then
      printf '%s=' "$p/$a"
      cat "$p/$a"
    fi
  done
done

for z in /sys/class/thermal/thermal_zone*; do
  printf '%s\n' "$z"
  cat "$z/type" "$z/temp"
done

for f in /sys/devices/system/cpu/cpu0/thermal_throttle/*; do
  [ -r "$f" ] || continue
  printf '%s=' "$f"
  cat "$f"
done

for f in /sys/class/power_supply/*/online \
         /sys/class/power_supply/*/status \
         /sys/class/power_supply/*/capacity; do
  [ -r "$f" ] || continue
  printf '%s=' "$f"
  cat "$f"
done

journalctl -k -b --no-pager |
  rg -i 'i915|huc|guc|drm|thrott|thermal|firmware|gpu.*(hang|reset)'
cat /sys/module/i915/parameters/enable_guc
```

Also enumerate existing powercap zones, recording name, enabled, constraint_*_name, constraint_*_power_limit_uw, constraint_*_time_window_us, energy_uj and max_energy_range_uj. Record existing GT act_freq_mhz/cur_freq_mhz and throttle_reason_* files for the render device's actual card/GT path. If debugfs is already available, read HuC status there; do not mount debugfs or change module parameters merely to collect this inventory. A firmware file on disk or enable_guc value is not proof of authenticated HuC.

### Encoder userspace inventory

```bash
ffmpeg -version
ffmpeg -buildconf
ffmpeg -hide_banner -h encoder=hevc_vaapi
ffmpeg -hide_banner -h filter=scale_vaapi
LIBVA_DRIVER_NAME=iHD vainfo --display drm --device /dev/dri/renderD128
dpkg-query -W -f='${Package}\t${Version}\n' \
  ffmpeg 'libavcodec*' 'libavfilter*' 'libva*' \
  'intel-media*' 'libigdgmm*'
ffprobe -v error -select_streams v:0 -show_streams -show_format \
  /var/tmp/sqzarr-perf-sample.mkv
sha256sum /var/tmp/sqzarr-perf-sample.mkv
```

If vainfo --help advertises --all, capture all attributes to identify rate-control and quality-range support for the specific HEVC entrypoint. Plain vainfo output alone may not list those details. Use LIBVA_DRIVER_NAME=iHD for capability checks, as for the benchmark. During a diagnostic process, inspect /proc/PID/maps for the loaded iHD/libva/libigdgmm paths and hash the relevant binaries. Package labels alone can miss custom or statically linked FFmpeg builds.

### Synchronized collection during the benchmark

Start these in separate terminals or as tracked background collectors before the encode. Output goes only into a new scratch directory. Do not run the collectors sequentially before the benchmark and mistake that for simultaneous telemetry.

```bash
TEL=$(mktemp -d /var/tmp/sqzarr-telemetry.XXXXXX)
intel_gpu_top -L

# Terminal A, on the host:
timeout --signal=INT 180s \
  intel_gpu_top -d drm:/dev/dri/renderD128 -s 1000 -J \
  -o "$TEL/gpu.json"

# Terminal B, on the host, using the same TEL path:
turbostat --interval 1 --num_iterations 180 \
  --out "$TEL/cpu.txt"
```

Check the installed tools' help first. Capture Bzy_MHz, Busy%, package watts, CPU/core temperatures, and per-engine Video, Render/Compute and VideoEnhance activity. One-second sampling is a reasonable starting point; keep it identical across hosts. For a 20-minute run extend both collectors beyond the benchmark duration. [S18, S20]

Use the host-visible FFmpeg PID to record its allowed CPUs, cgroup and optional DRM fdinfo. In cgroup v2, inspect the process cgroup and constraining ancestors for cpu.max, cpu.stat, cpuset.cpus.effective, memory.current, memory.max, memory.events, memory.swap.current and pressure files. Compare before/after counters and during-run pressure. A wider taskset mask cannot escape a parent cpuset. [S21, S29]

```bash
# Replace this with the verified host-visible FFmpeg PID.
FFPID=12345
cat "/proc/$FFPID/status"
cat "/proc/$FFPID/cgroup"
rg -H 'drm-(driver|client-id|engine|memory)' "/proc/$FFPID/fdinfo/"*
# If already installed:
pidstat -u -r -w -t -p "$FFPID" 1
mpstat -P ALL 1
```

Log effective policy limits and platform profile at the same interval. Timestamp each record, and record queue/guest activity. For finite tests, keep successful frame count, internal FFmpeg rtime, external wall seconds, user/system CPU seconds, maximum RSS, and any error. For resource attribution, engine-busy percentages and package power alone remain host-wide observations.

## 8 Common benchmark protocol and exact commands

All following commands are proposed benchmark operations, not read-only inventory. They create scratch files and run load, but make no persistent host-policy change. They run inside the existing safe chroot/container wrapper; they do not replace it. Preserve and restore SQZARR's exact prior queue state using its established operator procedure. No pause/resume API was supplied, so no endpoint is invented here.

- Record original queue state, pause/drain, and make restoration part of cleanup even on failure. Do not resume a queue that was already paused before the session.
- Keep CT260 stopped. Do not clone or start a second networked container with CT261's production IP.
- Use local input and null output for timings. Never overwrite source media. Use unique scratch output with -n for correctness checks.
- Verify CPUs 2 and 3 are distinct physical cores after reboot and inspect the actual effective mask. Pinning is not isolation from other guests.
- Alternate A/B and B/A order, use at least three successful pairs, retain every result and thermal start state. Use a predetermined repeat count; do not discard slow successful runs.
- Keep normal telemetry on both arms, but use verbose tracing only for characterization. Do not compare heavily traced runs with untraced timings.

### Prepare a continuous local clip

```bash
# Supply the actual read-only source path.
SRC='/actual/read-only/path/michael.che.shot.in.minnesota.2026.720p.web.h264-jff.mkv'
BENCH_ROOT=$(mktemp -d /var/tmp/sqzarr-research.XXXXXX)
CLIP="$BENCH_ROOT/continuous-180s.mkv"

ffmpeg -hide_banner -nostdin -v warning -n \
  -i "$SRC" -map 0:v:0 -t 180 -c:v copy -an -sn -dn "$CLIP"
sha256sum "$CLIP"
ffprobe -v error -select_streams v:0 -show_streams -show_format "$CLIP"
```

Prepare once and place the same bytes on both hosts through the established transfer workflow; verify SHA-256. A stream-copy operation is outside the timed test. The continuous clip has different content after 40 seconds from the old looped test, so establish a fresh SW baseline before attributing any gain. Use -frames:v 2152 for equal work within new pairs; do not assume its duration equals the old reported 89.964875 seconds. [S30]

### Reusable Bash runner

This definition preserves the original encoder options. It leaves GOP, profile and rate-control defaults untouched until inventory records them. If versions differ, create a separately labeled normalized control with common supported explicit settings; comparisons within an experiment must then use that same control. Run the definition in Bash inside the existing benchmark environment.

```bash
export LIBVA_DRIVER_NAME=iHD
PIN=2,3
LOGLEVEL=info
LIMIT=(-frames:v 2152)
SCALE='scale_vaapi=w=min(1920\,iw):h=-2'
RATE=(-b:v 2300k -maxrate 4000k -bufsize 4600k)
STRUCT=()
DECODE_THREADS=()
FILTER_THREADS=()
OUT=(-f null -)

run_case() {
  local label=$1 mode=$2 graph=''
  shift 2
  local -a input_opts=() filters=()
  case "$mode" in
    sw) graph='format=nv12,hwupload' ;;
    hw)
      input_opts=(-hwaccel vaapi -hwaccel_device va
                  -hwaccel_output_format vaapi -extra_hw_frames 16)
      ;;
    roundtrip)
      input_opts=(-hwaccel vaapi -hwaccel_device va
                  -hwaccel_output_format vaapi -extra_hw_frames 16)
      graph='hwdownload,format=nv12,hwupload'
      ;;
    *) return 2 ;;
  esac
  if [ -n "$SCALE" ]; then
    [ -z "$graph" ] || graph="$graph,"
    graph="$graph$SCALE"
  fi
  [ -z "$graph" ] || filters=(-vf "$graph")
  if [ -e "$BENCH_ROOT/$label.log" ]; then
    printf '%s\n' 'Use a new run label; existing logs are preserved.' >&2
    return 2
  fi
  /usr/bin/time -f 'wall_s=%e user_s=%U sys_s=%S max_rss_kib=%M' \
    -o "$BENCH_ROOT/$label.time" \
    taskset -c "$PIN" ffmpeg -hide_banner -nostdin \
      -v "$LOGLEVEL" -nostats -benchmark "${FILTER_THREADS[@]}" \
      -init_hw_device vaapi=va:/dev/dri/renderD128 -filter_hw_device va \
      "${input_opts[@]}" "${DECODE_THREADS[@]}" -i "$CLIP" \
      -map 0:v:0 "${LIMIT[@]}" \
      "${filters[@]}" -c:v hevc_vaapi "${RATE[@]}" "${STRUCT[@]}" \
      "$@" -an -sn -dn -stats_period 1 -progress pipe:1 "${OUT[@]}" \
      >"$BENCH_ROOT/$label.progress" 2>"$BENCH_ROOT/$label.log"
}
```

Prerequisites: the installed build exposes the shown hardware decode and progress options; /usr/bin/time is already present; CLIP and BENCH_ROOT point to the prepared local input and new log directory. Otherwise use the existing timing wrapper or skip an unsupported arm. No package installation is implied.

For an explicit normalized structure after capability checks, STRUCT=(-profile:v main -g 120 -bf 2) is a candidate, and RATE can include -rc_mode VBR if supported on both selected entrypoints. This is a new comparison baseline, not evidence that the original defaults were identical. Do not add these controls to only one arm.

## 9 Prioritized experiment matrix

Common metrics for every performance experiment: successful frame count, frames/internal rtime, frames/external wall time, CPU user/system seconds, RSS and cgroup pressure, per-engine activity, package watts/energy, busy CPU clocks, GT clocks, limit reasons and thermal/policy transitions. Validate a scratch output for any change that could affect encoding decisions. Times below are estimates based on the current 13-14-second finite run, excluding preparation and cooldown.

### E0 Characterize the existing path and timing overhead

```bash
# On the original benchmark command, change only logging/instrumentation:
# replace -v error with:
-v verbose -nostats -benchmark

# For the new continuous control:
( LOGLEVEL=verbose; run_case e0-sw sw )
rg -i 'entrypoint|profile|rate.control|quality|format|driver|bench:' \
  "$BENCH_ROOT/e0-sw.log"
```

Variable: instrumentation, not an optimization. Prerequisites: existing safe wrapper and telemetry. Metrics: all common metrics, selected entrypoint, encoder settings, loaded versions, internal rtime versus outer wrapper wall. Interpretation: EncSlice on pve5 means LP remains an untested opportunity; EncSliceLP means that explanation is already weakened. A large wrapper component changes the interpretation of the 0.723-second difference. If quality/default information is absent, mark it unknown; do not infer it. Quality: original command semantics unchanged. Runtime/risk: one short run per host, low additional logging load. Rollback: return to identical info logging for timed repetitions.

### E1 Software frame production versus GPU-resident frames

```bash
run_case e1-sw-a sw
run_case e1-hw-a hw
run_case e1-hw-b hw
run_case e1-sw-b sw
run_case e1-sw-c sw
run_case e1-hw-c hw
```

Variable: frame-production strategy. Hardware decoding necessarily removes software format/upload filters; this experiment tests that strategy as a unit, not the isolated cost of H.264 decoding. Prerequisites: H.264 VAAPI decode support, correct device, continuous input, enough surfaces, no fallback. Metrics: common metrics plus CPU seconds/frame and Video-engine distribution. Interpretation: faster with much less CPU supports CPU/transfer starvation or improved package headroom; same FPS with less CPU is a resource-saving result; slower can indicate shared decode/encode resource competition or scheduling; errors are invalid runs. Quality: same encoded frame selection and encoder settings, but verify metadata and decoded pixels. Runtime/risk: roughly 2-5 minutes per host including bookkeeping, low load risk. Rollback: use sw mode.

### E2 Encoder queue depth

```bash
run_case e2-depth2 hw -async_depth 2
run_case e2-depth4 hw -async_depth 4
run_case e2-depth8 hw -async_depth 8
# Repeat in reverse order with unique labels.
```

Variable: async_depth only. Prerequisites: the option and effective vaSyncBuffer support; keep extra_hw_frames=16 fixed in all arms, with the same frame structure. Metrics: common metrics, memory, latency and engine utilization. Interpretation: a repeatable gain supports insufficient queueing; no gain can mean adequate queueing, saturation elsewhere or ineffective async support. Allocation errors or stalls invalidate the arm. Quality: intended compression settings are unchanged, but validate output. Runtime/risk: about 1-3 minutes for a small sweep; low at 720p, higher memory risk at 4K. Rollback: restore the recorded original depth. [S6, S8]

### E3 Optional transfer and synchronization isolation

```bash
run_case e3-resident hw -async_depth 4
run_case e3-roundtrip roundtrip -async_depth 4
```

Variable: a system-memory round trip after the same hardware decoder. Prerequisites: working E1, NV12 download support and the same validated async value in both arms. Metrics: common metrics, CPU and memory traffic proxies. Interpretation: roundtrip penalty supports transfer/synchronization cost; a flat result weakens that component and points toward decode/encode or submission. Quality: no intentional lossy processing, verify format/color and source alignment. Runtime/risk: under 2 minutes for pairs; low at 720p. Rollback: hw mode. This is optional if E1 and telemetry already identify the constraint.

### E4 CPU allocation sensitivity

```bash
(
  DECODE_THREADS=(-threads:v 2)
  FILTER_THREADS=(-filter_threads 2)
  ( PIN=2; run_case e4-one-sw sw )
  ( PIN=2,3; run_case e4-two-sw sw )
  ( PIN=2; run_case e4-one-hw hw )
  ( PIN=2,3; run_case e4-two-hw hw )
)
```

Variable: allowed CPU mask within each SW or HW pair. Prerequisites: both IDs allowed and separate physical cores; same decoder/filter thread settings and unchanged quota. Metrics: common metrics and per-thread saturation. Interpretation: large SW sensitivity with smaller HW sensitivity strengthens the front-end explanation. One-to-two improvement does not prove two-to-four would improve. Flat results weaken CPU scheduling as the main current limit. Quality: encoder intent unchanged. Runtime/risk: a few minutes; slower test process only. Rollback: subprocess scope restores PIN automatically.

Optional later configuration delta: two to four distinct physical cores, with identical explicit thread settings and unchanged nonbinding CPU quota. A cgroup that permits only CPUs 2,3 cannot be widened by taskset. Record and temporarily adjust only the benchmark allocation through the established wrapper; restore it afterward. If the quota also needs changing, test that as a separate factor. This is a medium-risk production resource experiment, not part of read-only inventory.

### E5 Remove a likely no-op scaler

```bash
run_case e5-scale sw
( SCALE=''; run_case e5-no-scale sw )
```

Variable: scaler presence. Prerequisites: verified 1280x720 NV12 path, unchanged geometry/color requirements. Metrics: common metrics and actual output geometry. Interpretation: little change supports the documented pass-through behavior; a large repeated gain warrants checking the installed implementation or unrecognized conversion. Quality: expected unchanged, verify. Runtime/risk: under 2 minutes, low. Rollback: subprocess restores SCALE. Do not spend many repetitions on a negligible result. [S10]

### E6 Explicit speed and quality target

```bash
# Run only values advertised/supported by BOTH compared paths.
run_case e6-level1 hw -compression_level 1
run_case e6-level4 hw -compression_level 4
run_case e6-level7 hw -compression_level 7
```

Variable: compression_level. Prerequisites: actual quality range; no silent clamping; keep rate control, GOP, B-frames and entrypoint fixed. Candidate levels 1,4,7 are not a promise of support. Metrics: common metrics plus saved video bitrate, SSIM/PSNR or VMAF and visual inspection. Interpretation: a gain can be a quality tradeoff; if the gap changes, hidden target usage was important. No change may reflect implementation mapping or another bottleneck. Quality: same integer is not equal quality across GPUs; measure a rate-distortion/speed curve. Runtime/risk: several minutes of encoding plus offline quality analysis, low production load. Rollback: omit the option or restore the recorded value. [S7-S8]

### E7 Tiger Lake low-power encoding with bitrate control

```bash
# First characterize both paths on pve5; time later pairs at equal logging.
(
  LOGLEVEL=verbose
  run_case e7-normal-vbr hw -rc_mode VBR -low_power 0
  run_case e7-lp-vbr hw -rc_mode VBR -low_power 1
)
```

Variable: requested LP path. Prerequisites: HEVC EncSliceLP, working HuC/authentication for bitrate control, supported structure, and a control log proving the normal arm selected a different entrypoint. low_power=0 alone does not guarantee that. Metrics: common metrics and validated video size/quality. Interpretation: gain supports LP as an opportunity; lower watts at equal FPS can still be useful; failure records a capability/prerequisite problem; if both select LP there is no path comparison. Quality: bitrate intent is preserved, not guaranteed visual equivalence. Runtime/risk: a few minutes, low, no firmware alteration. Rollback: remove low_power. KBL HEVC LP is not a valid symmetric expectation. [S1, S7]

### E8 CQP diagnostic when LP bitrate control is unavailable

```bash
(
  LOGLEVEL=verbose
  RATE=(-rc_mode CQP -qp 24)
  run_case e8-normal-cqp hw -low_power 0
  run_case e8-lp-cqp hw -low_power 1
)
```

Variable: LP selection within a newly established CQP pair. Prerequisites: both actual paths support CQP and the same frame structure. Remove bitrate/maxrate/bufsize from BOTH arms as shown. If LP needs a different B-frame structure, establish that same structure in both arms before comparing; do not silently simplify only LP. Metrics: common metrics and measured output bytes/quality. Interpretation: shows architectural/path potential under CQP; it cannot prove a drop-in SQZARR VBR improvement. Equal QP is not equal size or quality. If both arms select LP, mark the test non-discriminating. Runtime/risk: a few minutes, low. Rollback: subshell restores original RATE.

### E9 Concurrent streams at a fixed aggregate CPU budget

```bash
# Same PIN, frame limit and unchanged quota for every child.
run_group() {
  local label=$1 jobs=$2 start end p j count total=0 failed=0
  local -a pids=()
  start=$(date +%s.%N)
  for ((j=1; j<=jobs; j++)); do
    run_case "$label-$j" hw -async_depth 4 &
    pids+=($!)
  done
  for p in "${pids[@]}"; do
    if ! wait "$p"; then failed=1; fi
  done
  end=$(date +%s.%N)
  for ((j=1; j<=jobs; j++)); do
    count=$(awk -F= '$1=="frame"{n=$2} END{print n+0}' \
      "$BENCH_ROOT/$label-$j.progress")
    [ "$count" -eq 2152 ] || failed=1
    total=$((total+count))
  done
  if [ "$failed" -ne 0 ]; then
    printf '%s\n' 'Incomplete group: exclude from speed comparison.' >&2
    return 1
  fi
  awk -v s="$start" -v e="$end" -v f="$total" \
    'BEGIN {print "wall_s=" e-s, "aggregate_fps=" f/(e-s)}' \
    >"$BENCH_ROOT/$label.group"
}
run_group e9-single 1
run_group e9-dual 2
```

Variable: job count. Prerequisites: validated settings, quiet comparison window, sufficient memory, separate logs. Confirm all children share the same aggregate CPU mask/quota; do not give each job two new cores and call it a concurrency-only result. Metrics: successful frames divided by group makespan, per-job FPS, engine busy and memory/guest latency. The finite group timer includes starts and tail completion; use the sustained design for overlapping steady throughput. Interpretation: rising aggregate throughput supports additional capacity/latency hiding; plateau or decline identifies a shared resource or pressure. Quality: identical settings. Runtime/risk: roughly 2-5 minutes, medium at 512 MiB. Start with two, and avoid combining 4K and concurrency without headroom. Rollback: jobs exit; interrupt only owned benchmark processes if needed.

### E10 Higher-resolution and content tests

```bash
# Set LADDER to prebuilt local, same-content inputs from the workstation.
LADDER='/actual/local/benchmark-ladder'
(
  SCALE=''
  for res in 720 1080 2160; do
    CLIP="$LADDER/h264-$res.mkv"
    run_case "e10-$res" hw -async_depth 4
  done
)
```

Variable: input resolution within a prepared source ladder. Prerequisites: same underlying time interval, frame count/rate, 8-bit 4:2:0 format, similar source encoding controls and local identical bytes on both hosts. Prepare on the workstation, not the 512 MiB media container. Use a native high-resolution master, not an upscaled 720p image as a claim about native 4K content. SCALE is empty for ALL arms so the old min(1920,iw) cap cannot convert 4K into a 1080p-output test.

Metrics: common metrics plus pixels/second, output resolution and memory. Interpretation: a larger pve5 advantage at higher resolution supports amortized overhead or media-capacity effects; a flat ratio with high busy suggests similar throughput for this selected path/budget. Quality: fixed bitrate isolates the resolution change but is not quality-normalized. A separate practical bitrate ladder changes an additional factor and must be labeled separately. Runtime/risk: minutes per arm and potentially much longer for 4K; medium memory/load risk, single job only. Rollback: local variables revert. Stop on allocation failure or pressure.

A separate content-complexity test changes only CLIP among matched-resolution dialogue, motion, and grain/detail clips. Keep encoding settings fixed, collect the same metrics and output quality, and interpret changed rankings as workload dependence. It needs no persistent change and usually takes a few minutes per source; it is preferable to drawing a universal conclusion from the stand-up clip.

### E11 Sustained duration

Variable: wall-clock run duration, using the same repeated content and the same settings for each compared mode. See Section 10 for the exact continuous-file preparation and 20-minute command. Prerequisites: proven finite correctness and enough scratch disk. Metrics: interval FPS, energy/frame and the full policy/thermal time series. Interpretation: falling speed aligned with watts/clocks/reason transitions supports adaptation; stable final throughput establishes sustainability for that environment. Quality: null output does not validate quality; retain the separate finite output check. Runtime/risk: 20 minutes per mode/host, medium shared-host load. Rollback: timeout stops the owned benchmark; restore the prior queue state.

### E12 Host workload comparison

Variable: competing workload state. Exact configuration delta: compare the same benchmark in a documented naturally quiet window versus an ordinary active window, with queue state and benchmark allocation unchanged. Prefer naturally quiet intervals; do not stop production guests as an unannounced test. Prerequisites: workload attribution and comparable thermal start state. Metrics: common metrics, other guest CPU and GPU clients, package watts and guest latency. Interpretation: repeatable movement with other work supports contention; a flat difference weakens it. Quality: unchanged. Runtime/risk: one finite paired block or one sustained pair; low in naturally quiet windows, potentially disruptive if guest shutdowns are later requested. Rollback: no host configuration is changed in the quiet-window version.

### E13 Match software to isolate host differences

Variable: benchmark userspace bundle as a controlled factor. Exact configuration delta: run both hosts with the same pinned FFmpeg, libva, full-feature iHD and gmmlib bundle in the existing offline execution model, preserving CPU/memory controls and kernel. First identify installed versions; no defensible package-version command can be specified before that inventory. Build/package any needed test bundle on the workstation. Do not install into production containers and do not start CT260.

Metrics: common metrics, loaded-binary hashes, actual path and saved output. Interpretation: a changed gap shows software-stack sensitivity; an unchanged gap permits a cleaner host comparison. This is a bundle effect, not attribution to an individual library. For individual software causality, change one compatible component at a time. Quality: verify again because driver changes can alter output. Runtime/risk: preparation may take substantial workstation time; runtime minutes, medium compatibility risk in the disposable benchmark only. Rollback: return to the original offline userspace bundle.

### Maintenance experiments are a separate phase

| Potential experiment | Required evidence and exact scope | Risk and restoration |
| --- | --- | --- |
| Firmware platform profile | Only after sustained evidence warrants it. Compare balanced versus performance by changing /sys/firmware/acpi/platform_profile alone; keep CPU policy fixed and log the effective value. | Host-global heat/power change. The earlier performance profile overheated. Save the original string and restore it; abort for production impact. |
| HuC enabling | Only if LP VBR remains a useful target and authenticated HuC is absent. Intel documents i915.enable_guc=2 for relevant older platforms, but first resolve the running kernel's semantics, existing GuC submission mode and firmware availability. | Usually a planned reboot, not a runtime GPU-driver unload. Preserve existing boot/module configuration and restore it on rollback. Do not blindly replace a different enable_guc policy. |
| CPU driver / BIOS / kernel changes | No specific current fault or matching firmware fix was established. Do not include these in the initial test block. A later test needs one justified change and preserved baseline. | Host reboot or broader behavior changes; separate maintenance window and rollback image/settings. No raw power-limit increase or thermal-protection disablement. |

No maintenance action above was performed or is needed to execute E0-E8. Proposed hardware-resource, workload or software changes require their own controlled phase; they are not prerequisites for the first useful results.

## 10 Sustained-test design

Ninety seconds of media currently represents only about thirteen seconds of wall time. Three such runs cannot establish twenty-minute thermal or firmware behavior. Intel defines Tau as a power-averaging parameter, not a simple fixed turbo countdown. [U, S11]

Use one uninterrupted encode process for 20 minutes of wall time. Reserve the first 5 minutes for adaptation and compare the final 10-15 minutes as the steady interval. Extend the run only if the metrics are still drifting. Record initial package temperature, room conditions, chassis placement/lid state, AC/charging state, firmware profile and other work. Test baseline and the best valid candidate, then repeat in reversed order if the effect is small or variable.

### Prepare repeated compressed input outside timing

```bash
# Set this to the FASTEST observed finite-test speed, rounded upward.
FASTEST_X=20
MEDIA_SECONDS=$(awk -v r="$FASTEST_X" \
  'BEGIN {printf "%.0f", 1200*r*1.5}')
LONG="$BENCH_ROOT/continuous-sustained.mkv"

ffmpeg -hide_banner -nostdin -v warning -n \
  -stream_loop -1 -i /var/tmp/sqzarr-perf-sample.mkv \
  -t "$MEDIA_SECONDS" -map 0:v:0 -c:v copy -an -sn -dn "$LONG"

# Cross several former 40-second joins with SOFTWARE decoding.
ffmpeg -hide_banner -nostdin -v warning -i "$LONG" \
  -map 0:v:0 -t 180 -an -sn -dn -f null -
```

The loop occurs only during stream-copy preparation; the timed decoder opens a single ordinary file. Verify timestamps, fixed format and decode correctness across joins before trusting the input. Allow disk space and avoid a RAM-backed filesystem; this is compressed scratch data, not a multi-gigabyte raw-frame cache inside CT261. If the measured fastest speed exceeds FASTEST_X, regenerate a sufficiently long input. If input EOF arrives before 20 minutes, the sustained run is incomplete.

### Twenty-minute GPU-resident example

```bash
set -o pipefail
if timeout --signal=INT --kill-after=15s 1200s \
  taskset -c 2,3 ffmpeg -hide_banner -nostdin -v info -nostats \
  -benchmark -init_hw_device vaapi=va:/dev/dri/renderD128 \
  -filter_hw_device va \
  -hwaccel vaapi -hwaccel_device va -hwaccel_output_format vaapi \
  -extra_hw_frames 16 -i "$LONG" -map 0:v:0 \
  -vf 'scale_vaapi=w=min(1920\,iw):h=-2' \
  -c:v hevc_vaapi -async_depth 4 \
  -b:v 2300k -maxrate 4000k -bufsize 4600k \
  -an -sn -dn -stats_period 1 -progress pipe:1 -f null - \
  2>"$BENCH_ROOT/sustained-hw.log" |
while IFS= read -r line; do
  printf '%(%s)T %s\n' -1 "$line"
done >"$BENCH_ROOT/sustained-hw.progress"; then
  rc=${PIPESTATUS[0]}
else
  rc=${PIPESTATUS[0]}
fi
printf 'encode_or_timeout_exit=%s\n' "$rc"
```

Use the tested async value, not 4 by assumption. No -re or -readrate restriction: those would change the throughput question. The Bash relay timestamps progress records to one-second resolution; compute FPS from frame-count differences over several-second bins, not rounded cumulative speed. A timeout status of 124 is expected only when the time limit ended an otherwise healthy run; error logs and progress must still be checked. A premature encoder failure is not a valid timeout result. [S30]

- For the SW duration control, change the frame-production strategy as in E1 while retaining the same LONG file and output options.
- Compare 0-1 minute, 1-5 minutes and final 10 minutes. Plot interval FPS with watts, busy CPU MHz, GPU engine activity, actual/requested GT clocks, reason flags, policy limits and temperature on one shared time axis.
- Compute total package energy per encoded frame with counter wrap handling. This includes other host work; report that scope and an idle/control interval rather than calling it isolated encoder energy.
- For concurrency, use a common 20-minute wall window and aggregate frame deltas while all jobs overlap. Keep the aggregate CPU allowance fixed.
- Abort for new OOM/memory.events failures, swap/pressure growth that affects production, repeated GPU errors, or unacceptable guest latency. Do not disable thermal protections to complete a run.
- Restore the exact starting queue state and any separately changed benchmark resource settings after success or failure.

## 11 Output quality and correctness

Null output confirms processing throughput, not a usable saved bitstream. Before adopting any gain, save one short output per candidate with the same encoding settings and compare it to the same source frames. Perform heavier quality scoring on the workstation.

```bash
(
  OUT=(-n "$BENCH_ROOT/quality-candidate.mkv")
  run_case quality-candidate hw -async_depth 4
)
ffprobe -v error -select_streams v:0 -count_frames \
  -show_streams -show_format "$BENCH_ROOT/quality-candidate.mkv"
ffmpeg -hide_banner -nostdin -v error \
  -i "$BENCH_ROOT/quality-candidate.mkv" -map 0:v:0 \
  -an -sn -dn -f null -
```

Check frame count, duration/timestamps, resolution, bit depth/chroma, color metadata, frame structure, video packet bytes and decode errors. A successful encoder exit alone can miss a generation/driver correctness defect. Use the same output-muxing conditions in quality comparisons; do not compare whole-file bitrate with differing audio or subtitles.

```bash
# On the workstation, with the identical source clip and candidate output:
ffmpeg -hide_banner -nostdin \
  -i quality-candidate.mkv -i continuous-180s.mkv \
  -filter_complex \
  '[0:v]setpts=PTS-STARTPTS,format=yuv420p[d];[1:v]trim=end_frame=2152,setpts=PTS-STARTPTS,format=yuv420p[r];[d][r]ssim=stats_file=ssim.log' \
  -an -sn -f null -
```

That SSIM example assumes the same starting frame, 8-bit 4:2:0, frame cadence and no resolution change. Verify alignment first. For a resolution ladder, compare each encoded result to its matching-resolution source. VMAF is an optional additional measure only if already available; use identical model/version and preprocessing. Inspect motion, faces, dark areas and grain as well as numerical scores. Equal bitrate or QP is not a sufficient quality control. [S31]

For generation conclusions, report two separate comparisons: (1) common supported path/settings and matched software, and (2) each host's best validated configuration at an agreed output-quality/size target. The second is the practical SQZARR decision; it need not use identical encoder internals.

## 12 Documented issues and limits of the conclusion

| Evidence | What it supports | What it does not establish |
| --- | --- | --- |
| TLP issue 570, X1 Gen9, 2021 | A documented turbo-disabled/platform-profile interaction produced a 1.2 GHz ceiling; a workaround was discussed/committed. | A present fault under acpi-cpufreq with turbo restored, or a reason to disable CPU power management. |
| Intel media-driver issue 1190, 2021 | Historical Tiger Lake HEVC output corruption was reported; saved-output validation is necessary. | That this version has the defect, or that corruption explains the present FPS. |
| Lenovo BIOS support family DS548819 | The official support listing identifies N32ETA1W as BIOS 1.77 for this model family. | A verified later BIOS fix for these symptoms, or that 1.77 is necessarily the latest release now. |
| Supplied hardware-decode/loop failure | The old failed benchmark is not usable performance evidence. A continuous input removes the loop/reinit confound. | A general inability to decode H.264 with VAAPI on Tiger Lake. |

The first two rows are historical project issue records, not vendor guarantees about present behavior. The Lenovo changelog needed to connect a specific new release to this HEVC/power case was not verified; a speculative firmware update is not recommended. [S24-S25, S32]

> A large single-stream HEVC gain should not be promised from the generation gap alone. Tiger Lake offers real additional paths and media capacity, but this command has not established that it uses them effectively. The defensible next step is to identify the active path and limiting resource, then measure sustained throughput at acceptable output quality.

## 13 Sources and version boundaries

U: User-supplied Pasted text(3).txt, research brief available in this conversation. All host identities, configurations, measured speeds/temperatures/counters and experiment history are reported observations from that brief. Arithmetic derived from those values is identified above.

### S1. Intel media-driver README: platform support, E/Es paths, build variants and HuC prerequisites

[https://github.com/intel/media-driver](https://github.com/intel/media-driver)

Current upstream; verify installed driver/build and firmware state.

### S2. Intel Tiger Lake PRM Volume 11, HEVC/VP9 encoder architecture, PDF page 24

[https://cdrdv2-public.intel.com/705826/intel-gfx-prm-osrc-tgl-vol-11-media-engines.pdf](https://cdrdv2-public.intel.com/705826/intel-gfx-prm-osrc-tgl-vol-11-media-engines.pdf)

Architecture documentation, not a throughput benchmark.

### S3. Linux i915 platform engine definitions

[https://github.com/torvalds/linux/blob/master/drivers/gpu/drm/i915/i915_pci.c](https://github.com/torvalds/linux/blob/master/drivers/gpu/drm/i915/i915_pci.c)

Upstream platform masks; confirm runtime enabled engines.

### S4. Intel Core i7-1185G7 specifications

[https://www.intel.com/content/www/us/en/products/sku/208664/intel-core-i71185g7-processor-12m-cache-up-to-4-80-ghz-with-ipu/specifications.html](https://www.intel.com/content/www/us/en/products/sku/208664/intel-core-i71185g7-processor-12m-cache-up-to-4-80-ghz-with-ipu/specifications.html)

Advertised maxima and two codec engines are not sustained measurements.

### S5. Intel Core i5-8350U specifications

[https://www.intel.com/content/www/us/en/products/sku/124969/intel-core-i58350u-processor-6m-cache-up-to-3-60-ghz/specifications.html](https://www.intel.com/content/www/us/en/products/sku/124969/intel-core-i58350u-processor-6m-cache-up-to-3-60-ghz/specifications.html)

UHD 620 and 1.10 GHz graphics maximum.

### S6. FFmpeg 7 VAAPI common options

[https://ffmpeg.org/doxygen/7.0/vaapi__encode_8h_source.html](https://ffmpeg.org/doxygen/7.0/vaapi__encode_8h_source.html)

low_power and async_depth defaults; compare installed help.

### S7. FFmpeg 7 VAAPI entrypoint, rate-control and quality initialization

[https://ffmpeg.org/doxygen/7.0/libavcodec_2vaapi__encode_8c_source.html](https://ffmpeg.org/doxygen/7.0/libavcodec_2vaapi__encode_8c_source.html)

Normal list includes LP; quality buffer is conditional.

### S8. FFmpeg VAAPI encoder documentation

[https://ffmpeg.org/ffmpeg-codecs.html#VAAPI-encoders](https://ffmpeg.org/ffmpeg-codecs.html#VAAPI-encoders)

Current option semantics; support is driver/version dependent.

### S9. FFmpeg 7 HEVC encoder defaults

[https://ffmpeg.org/doxygen/7.0/vaapi__encode__h265_8c_source.html](https://ffmpeg.org/doxygen/7.0/vaapi__encode__h265_8c_source.html)

GOP/B-frame defaults, not a guarantee of actual driver output.

### S10. FFmpeg 7 scale_vaapi pass-through implementation

[https://ffmpeg.org/doxygen/7.0/vf__scale__vaapi_8c_source.html](https://ffmpeg.org/doxygen/7.0/vf__scale__vaapi_8c_source.html)

No-op behavior conditional on geometry, format and color properties.

### S11. Intel 11th-generation Core datasheet volume 1, thermal management

[https://cdrdv2-public.intel.com/631121/631121_TGL%20Datasheet%20Volume1of2%20rev012_PUBLIC.pdf](https://cdrdv2-public.intel.com/631121/631121_TGL%20Datasheet%20Volume1of2%20rev012_PUBLIC.pdf)

Package constraints, Tau and OEM tuning; not proof of active Lenovo settings.

### S12. Linux intel_pstate versus acpi-cpufreq

[https://docs.kernel.org/admin-guide/pm/intel_pstate.html](https://docs.kernel.org/admin-guide/pm/intel_pstate.html)

ACPI turbo representation and driver differences.

### S13. cpufreqd configuration manual

[https://manpages.debian.org/testing/cpufreqd/cpufreqd.conf.5.en.html](https://manpages.debian.org/testing/cpufreqd/cpufreqd.conf.5.en.html)

Rule inputs and scoring; installed config still needed.

### S14. cpufreqd 2.4.2-3 CPU plugin source

[https://sources.debian.org/src/cpufreqd/2.4.2-3/src/cpufreqd_cpu.c](https://sources.debian.org/src/cpufreqd/2.4.2-3/src/cpufreqd_cpu.c)

Exact supplied version: aggregate accounting and nice weighting.

### S15. cpufreqd 2.4.2-3 ACPI temperature source

[https://sources.debian.org/src/cpufreqd/2.4.2-3/src/cpufreqd_acpi_temperature.c](https://sources.debian.org/src/cpufreqd/2.4.2-3/src/cpufreqd_acpi_temperature.c)

Conditional on use of this plugin/directive.

### S16. cpufreqd 2.4.2-3 rule application and getter source

[https://sources.debian.org/src/cpufreqd/2.4.2-3/src/main.c](https://sources.debian.org/src/cpufreqd/2.4.2-3/src/main.c)

Unchanged-profile skip and cached applied-profile state.

### S17. Linux CPU performance scaling

[https://docs.kernel.org/admin-guide/pm/cpufreq.html](https://docs.kernel.org/admin-guide/pm/cpufreq.html)

Policy/request values versus actual CPU frequency.

### S18. turbostat manual

[https://manpages.debian.org/trixie/linux-cpupower/turbostat.8.en.html](https://manpages.debian.org/trixie/linux-cpupower/turbostat.8.en.html)

Check local options/columns; busy MHz, power and sampling semantics.

### S19. Linux i915 GT power-management sysfs implementation

[https://kernel.googlesource.com/pub/scm/linux/kernel/git/xiang/erofs/+/d0989d01c66fed6a741820a96b8cca6688f183ff/drivers/gpu/drm/i915/gt/intel_gt_sysfs_pm.c](https://kernel.googlesource.com/pub/scm/linux/kernel/git/xiang/erofs/+/d0989d01c66fed6a741820a96b8cca6688f183ff/drivers/gpu/drm/i915/gt/intel_gt_sysfs_pm.c)

Pinned Linux source; paths and availability vary by kernel/platform.

### S20. intel_gpu_top manual

[https://manpages.debian.org/experimental/intel-gpu-tools/intel_gpu_top.1.en.html](https://manpages.debian.org/experimental/intel-gpu-tools/intel_gpu_top.1.en.html)

Device filter, interval and JSON options; verify installed tool.

### S21. Linux DRM client usage statistics

[https://origin.kernel.org/doc/html/v6.1/gpu/drm-usage-stats.html](https://origin.kernel.org/doc/html/v6.1/gpu/drm-usage-stats.html)

Per-client counters when the driver exposes them.

### S22. Linux Intel thermal throttle event reporting

[https://cdn.kernel.org/doc/html/latest/admin-guide/thermal/intel_thermal_throttle.html](https://cdn.kernel.org/doc/html/latest/admin-guide/thermal/intel_thermal_throttle.html)

Event counts, shared package reporting and optional time counters.

### S23. Linux power-capping framework

[https://www.kernel.org/doc/html/latest/power/powercap/powercap.html](https://www.kernel.org/doc/html/latest/power/powercap/powercap.html)

Named zones, constraints, energy and enable state.

### S24. TLP project issue 570: X1 Gen9 turbo/profile interaction

[https://github.com/linrunner/TLP/issues/570](https://github.com/linrunner/TLP/issues/570)

Historical 2021 report; does not diagnose the restored current state.

### S25. Intel media-driver issue 1190: Tiger Lake HEVC corruption

[https://github.com/intel/media-driver/issues/1190](https://github.com/intel/media-driver/issues/1190)

Historical 2021 report; output-validation rationale, not a matched diagnosis.

### S26. Linux platform-profile API

[https://www.kernel.org/doc/html/latest/userspace-api/sysfs-platform_profile.html](https://www.kernel.org/doc/html/latest/userspace-api/sysfs-platform_profile.html)

Firmware policy selection is separate from achieved performance.

### S27. TLP architecture

[https://linrunner.de/tlp/developers/architecture.html](https://linrunner.de/tlp/developers/architecture.html)

Current docs; use event behavior, not newer profile features, for TLP 1.8.

### S28. cpufreqd-get manual

[https://manpages.debian.org/bullseye/cpufreqd/cpufreqd-get.1.en.html](https://manpages.debian.org/bullseye/cpufreqd/cpufreqd-get.1.en.html)

Read-only applied-profile request; existing socket required.

### S29. Linux cgroup v2 documentation

[https://docs.kernel.org/admin-guide/cgroup-v2.html](https://docs.kernel.org/admin-guide/cgroup-v2.html)

Hierarchical resource controls and diagnostic counters.

### S30. FFmpeg command-line documentation

[https://ffmpeg.org/ffmpeg.html](https://ffmpeg.org/ffmpeg.html)

Hardware devices, frame limits, progress, benchmark timing and input control.

### S31. FFmpeg filters documentation

[https://ffmpeg.org/ffmpeg-filters.html](https://ffmpeg.org/ffmpeg-filters.html)

Hardware frame transfer, trim/setpts and SSIM semantics.

### S32. Lenovo BIOS support family DS548819

[https://support.lenovo.com/aw/en/downloads/ds548819](https://support.lenovo.com/aw/en/downloads/ds548819)

Indexed official listing corroborates 1.77 identity; latest/fix not established.
