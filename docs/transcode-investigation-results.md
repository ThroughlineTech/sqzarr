# Transcoding investigation: pve2 and pve5

Measured 2026-10-20. Implements the
[investigation brief](transcode-investigation-agent-prompt.md). Earlier
[deployment measurements](container-deployment.md) remain historical evidence;
the [research hypotheses](transcode-research-claude.md) were not treated as facts.

## Findings

**The standard HEVC encoder path, particularly its Render/3D work, is the main
short-run limit on both hosts.** Confidence: high for this input and configuration;
lower for the precise internal driver/kernel bottleneck. Standard VBR encoding
kept Render/3D approximately 93–94% busy. Hardware decoding raised that to
95–99%, while greatly reducing CPU work. Decode-only and decode/convert/upload
tests ran much faster than complete encoding. Neither a saturated two-core CPU
front end nor insufficient FFmpeg async depth explains the measured ceiling.
Engine busy time does not prove that every execution unit is fully utilized.

The non-looped baseline medians were **6.47× on pve2 and 7.47× on pve5**, a
15.5% difference. The older 5.5% gap was not reproduced exactly. The new range
contains continuous content rather than repeated 40-second content, so those
historical speeds are not a controlled comparison with these results.

**GPU-resident decoding is a useful efficiency improvement.** Short-run medians
were 7.04× / 7.52×, approximately +8.8% / +0.7% relative to each host's baseline.
Mean FFmpeg CPU use fell from 120% / 105% to 19% / 17% of one logical CPU.
The checked 20-second VBR outputs had identical decoded pixels to their
respective host baselines. This is a verified result for that segment, not a
general guarantee of bit-identical output.

**pve5's low-power encoder has substantial headroom, but production-equivalent
rate control is unavailable in the current configuration.** Its CQP median was
31.76× versus 8.88× for standard CQP, about 3.57 times as fast. Selected
entrypoints were explicitly logged as `EncSliceLP` and `EncSlice`. Low-power
VBR failed with only CQP advertised as compatible. HuC was disabled according
to the kernel debug interface; it was not enabled during this investigation.
The CQP quality/bitrate tradeoff below prevents treating 31.76× as a drop-in
production VBR gain.

**The full-input comparison strengthens the pve2 efficiency case, but does
not show a pve5 speed gain.** Across 7.4–8.9 minutes of actual encoding per
run, hardware decode improved pve2 from 6.66× to 7.99× (+20.0%) and reduced
CPU use from 188% to 23%. On pve5 it reduced CPU use from 148% to 17% but
ran at 7.27× versus 7.47× (-2.7%). These are single long runs per condition,
with host contention and container memory pressure; the small pve5 difference
is not an isolated causal estimate. pve2's longer workload has a mixed
front-end/encoder limit: near-two-core CPU use and 90% Render busy in E0
become 23% CPU and almost 100% Render busy in E4. pve5 remains dominated
by the standard encoder path.

## Environments and controls

| Item | pve2 | pve5 |
|---|---|---|
| CPU | i5-8350U | i7-1185G7 |
| Kernel | `7.0.14-25-pve` | `7.0.14-25-pve` |
| Harness | Stopped CT260 userspace, private mount namespace and chroot | `pct exec 261` |
| CPU allocation | `taskset -c 2,3`; no container quota/memory enforcement | Effective cpuset `2-3`; two logical CPUs |
| Memory enforcement | Host limits apply to offline process | 512 MiB RAM, 512 MiB swap; `memory.high=532676608` |
| CPU policy | `acpi-cpufreq`, performance, boost enabled | Same |
| Observed policy min/max | 1,700,000 kHz | 3,001,000 kHz |
| GPU ceiling / observed short-run peak | 1100 / 1100 MHz | 1350 / 1100 MHz |
| Package MMIO PL1 / MSR PL1 | 15 / 25 W | 15 / 64 W |
| HuC / `enable_guc` | Disabled / `-1` (auto) | Disabled / `-1` (auto) |
| Firmware platform profile | Interface unavailable | Balanced; custom performance service disabled |
| Memory hardware | Two 8 GiB LPDDR3 banks, 2133 MT/s | LPDDR4 reported at 4267 MT/s |

CPU2 and CPU3 are separate physical cores on both hosts; CPU6 and CPU7 are
their respective SMT siblings. Pinning is not exclusive reservation. Both
hosts had other running guests. Initial host inventories showed background
VM work, and pve5 also had database activity. GPU telemetry reported `ffmpeg`
clients during the measured windows; no competing named GPU application was
observed. This was not a fully isolated-host experiment.

Both actual test userspaces had FFmpeg `7.1.3-0+deb13u1` (Debian package
`7:7.1.3-0+deb13u1`), libva `2.22.0-3`, non-free Intel media driver
`25.2.3+ds1-1`, and libigdgmm `22.7.2+ds1-1`. Both GPUs were bound to i915.
`vainfo` exposed HEVCMain `EncSlice` on both and `EncSliceLP` only on pve5.
Actual encode logs, independently of `vainfo`, confirmed the selected entrypoint.

Installed encoder help reports async depth 2, low-power false, RC auto,
QP 0, B-depth 1, and IDR interval 0 as defaults. Production bitrate arguments
resolved to VBR. Generic help lists refs 1 and compression level -1
(unspecified); no compression override was introduced. Generic `-g`/`-bf`
help is not the codec-specific default: FFmpeg's HEVC VAAPI defaults are
GOP 120 and two B frames, corroborated by the measured output structures.
[FFmpeg 7.1 HEVC VAAPI implementation](https://ffmpeg.org/doxygen/7.1/vaapi__encode__h265_8c_source.html).

On pve2 the driver reported reference support 3/1 and used I/P/B frames. On
pve5 the standard path reported 4/4 and replaced P frames with B frames;
low-power reported 3/3. Both pve5 CQP paths logged fixed QPs 26/26/31 for
IDR/P/B. The same unset GOP/quality controls were retained throughout; these
capability differences and internal driver choices remain relevant to comparisons.

## Short-run results

Every row contains three successful runs. Speeds are output timestamp duration
divided by FFmpeg's `-benchmark` real time; wall seconds include harness startup
and the polling interval. CPU percentages are means across runs, where 100%
means one logical CPU. GPU percentages are mean monitored engine activity,
including brief startup/tail intervals. VideoEnhance measured 0% throughout.
Peak temperatures are package-sensor maxima. Thermal increments are host-wide
package counter deltas for runs 1/2/3, not events attributed solely to FFmpeg.

| Host | Condition | Median × (range) | Wall s | CPU % | Render / Video % | Peak °C | Thermal increments | GPU flags seen |
|---|---|---:|---:|---:|---:|---:|---|---|
| pve2 | E0 | 6.47 (6.26–6.58) | 14.16 | 120 | 94 / 10 | 82 | 52/27/95 | unavailable |
| pve2 | E1 | 16.06 (16.04–16.10) | 5.89 | 178 | 0 / 0 | 82 | 275/276/284 | unavailable |
| pve2 | E2 | 14.65 (14.36–14.87) | 6.40 | 186 | 0 / 0 | 81 | 425/437/419 | unavailable |
| pve2 | E3 | 6.30 (6.23–6.37) | 14.51 | 110 | 95 / 11 | 81 | 95/172/57 | unavailable |
| pve2 | E4 | 7.04 (7.01–7.05) | 13.10 | 19 | 99 / 22 | 75 | 14/16/0 | unavailable |
| pve2 | E5sw1 | 5.57 (5.56–5.65) | 16.36 | 95 | 79 / 10 | 82 | 275/511/5 | unavailable |
| pve2 | E5sw4 | 6.36 (6.27–6.36) | 14.46 | 113 | 95 / 11 | 80 | 119/220/40 | unavailable |
| pve2 | E5sw8 | 6.26 (6.25–6.30) | 14.73 | 112 | 92 / 11 | 80 | 243/89/79 | unavailable |
| pve2 | E5hw1 | 6.27 (6.24–6.31) | 14.51 | 17 | 84 / 20 | 79 | 16/2/0 | unavailable |
| pve2 | E5hw4 | 6.98 (6.98–7.12) | 13.12 | 20 | 99 / 21 | 71 | 0/1/0 | unavailable |
| pve2 | E5hw8 | 7.06 (7.00–7.12) | 12.92 | 21 | 99 / 21 | 69 | 0/0/0 | unavailable |
| pve2 | E6std | 7.98 (7.92–8.00) | 11.55 | 20 | 98 / 64 | 73 | 0/0/0 | unavailable |
| pve5 | E0 | 7.47 (7.44–7.51) | 12.67 | 105 | 93 / 6 | 85 | 0/3/0 | PL4 |
| pve5 | E1 | 20.64 (18.79–21.17) | 4.93 | 173 | 0 / 0 | 87 | 4/13/18 | none |
| pve5 | E2 | 17.12 (17.00–19.97) | 6.05 | 179 | 0 / 0 | 91 | 0/5/15 | none |
| pve5 | E3 | 7.45 (7.44–7.54) | 12.67 | 103 | 94 / 6 | 89 | 3/1/0 | PL1, PL4 |
| pve5 | E4 | 7.52 (7.47–7.59) | 12.75 | 17 | 95 / 12 | 79 | 0/0/0 | PL4 |
| pve5 | E5sw1 | 6.26 (6.24–6.29) | 15.02 | 75 | 75 / 6 | 84 | 1/0/0 | PL1, PL4 |
| pve5 | E5sw4 | 7.48 (7.41–7.57) | 12.65 | 101 | 94 / 6 | 85 | 1/6/0 | PL1, PL4 |
| pve5 | E5sw8 | 7.44 (7.38–7.55) | 12.80 | 91 | 94 / 7 | 87 | 0/0/0 | PL1, PL4 |
| pve5 | E5hw1 | 6.52 (6.51–6.53) | 14.43 | 14 | 78 / 11 | 77 | 0/0/0 | PL4 |
| pve5 | E5hw4 | 7.56 (7.54–7.76) | 12.62 | 18 | 95 / 12 | 78 | 0/0/0 | PL4 |
| pve5 | E5hw8 | 7.53 (7.49–7.70) | 12.75 | 17 | 95 / 12 | 77 | 0/0/0 | PL4 |
| pve5 | E6std | 8.88 (8.76–8.94) | 10.72 | 17 | 95 / 12 | 82 | 0/0/0 | PL4 |
| pve5 | E6lp | 31.76 (31.59–31.89) | 3.48 | 22 | 0 / 59 | 78 | 0/0/0 | none |

Conditions:

- E0: software decode, NV12 conversion/upload, scale, standard VBR encode.
- E1: software decode to null, with actual decoded frames and no stream copy.
- E2: software decode, NV12 conversion and upload, `wrapped_avframe` to null.
  The sink accepted VAAPI frames; no download fallback was necessary.
- E3: E0 with only `scale_vaapi` removed.
- E4: VAAPI H.264 decode to hardware surfaces, scale, standard VBR encode.
- E5sw1/4/8 and E5hw1/4/8: E0 or E4 with that explicit async depth.
- E6std / E6lp: GPU-resident pipeline, explicit standard / low-power CQP,
  QP 26, with all three bitrate/buffer arguments removed from both conditions.

Every short encode completed 2,158 frames and reported 89.964875 seconds.
Decode-only sinks report their own final decoded-frame timestamp; raw results
retain those values. Failed capability probes are excluded from the table.
The historical looped harness produced 2,152 frames, another reason not to
silently combine the two datasets.

Removing scale did not improve throughput. FFmpeg explicitly logged VAAPI
filter passthrough for this already-fitting input. The three-second
pre-encode scale/no-scale frame hashes also matched. Async depth 1 reduced
throughput; depths 4 and 8 did not provide a compelling consistent improvement
over default 2. Frame hashes at depth 8 matched the corresponding baseline
in the checked output segment.

### Power, temperature, CPU and memory

Short-run CPU min/max requests, performance governor and package limits did
not change during the samples. There was no sampled evidence of cpufreqd
switching to its hot-CPU profile. APERF/MPERF measurements from the installed
`turbostat` were usable and showed boost; policy requests and
`scaling_cur_freq` snapshots were not substituted for effective frequency.

pve5's E0 and E4 had no sampled GPU PL1 or thermal assertion. PL4 appeared
briefly: approximately 2–11% of E0 samples and 2–9% of E4 samples. Some other
software-decode variants had brief PL1 assertions. The remaining exposed
reasons—PL2, prochot, ratl, VR TDC and VR thermal alert—were clear in the
short matrix. pve2 lacks these newer reason files. Thus the 1100 MHz pve5
peak cannot be explained simply as its configured ceiling, but these
measurements do not establish a single cause for the gap below 1350 MHz.

Per-thread accounting did not reveal a continuously saturated FFmpeg thread
in the baseline: the busiest sampled thread means were about 26–38% on pve2
and 24–29% on pve5. E4's busiest thread averaged about 11–12%.
Render/3D utilization, front-end ceiling tests and controlled path changes
together support the encoder-path interpretation more strongly than CPU
frequency sensitivity alone.

RAPL package energy was sampled with per-domain wrap handling. E0 used
approximately 234–315 J on pve2 and 227–268 J on pve5; E4 used 209–216 J
and 158–227 J over the monitored intervals. These are whole-package
measurements including other guests, not exclusive transcoder energy
or a causal efficiency estimate. Valid sampled fan ranges were about
3112–5033 RPM and 5449–6711 RPM; zero and 65535 readings were excluded.

There were no container CPU quota-throttle increments or OOM/OOM-kill events.
Memory pressure was nevertheless present: pve5's E5hw1 run 2 and E5hw4
run 2 each incremented `memory.events:max` by 2; sustained E4 added 4.
Across runs with enforcing-parent snapshots, `high` increased by 4,157
and `max` by 8. Sustained E4 started with 531,283,968 bytes charged to
the cgroup after input preparation and added 1,646 high events; the later
E0 started at 353,873,920 bytes and added 1,331 high events, with no new
max event. Charged memory includes page cache and other container memory,
not just FFmpeg. This is a meaningful harness/order limitation, rather
than evidence that the container was comfortably below its limit throughout.
Largest recorded FFmpeg RSS was 145,104 KiB on pve2 and 162,324 KiB on pve5.
All depth-8 runs succeeded, but these data do not justify increasing depth
or ignoring memory-pressure handling.

## Correctness and quality

All 13 saved outputs decoded successfully. Each contained 480 frames,
20.020 seconds of container duration, 1280×720 `yuv420p`, limited-range BT.709
color space/transfer/primaries, and increasing presentation timestamps from
0 to 19.978 seconds. Source and output were aligned with `setpts=PTS-STARTPTS`
before SSIM. VMAF was not present; nothing was compiled in either container.

| Host | Checked conditions | Size bytes | Container kb/s | SSIM all | Frame types |
|---|---|---:|---:|---:|---|
| pve2 | E0, E3, E4, E5sw8, E5hw8 | 4,057,375 each | 1621.3 | 0.989632 | 4 I / 160 P / 316 B |
| pve2 | E6std | 1,492,254 | 596.3 | 0.986805 | 4 I / 160 P / 316 B |
| pve5 | E0, E3, E4, E5sw8, E5hw8 | 4,171,306 each | 1666.9 | 0.991468 | 4 I / 476 B |
| pve5 | E6std | 1,107,254 | 442.5 | 0.987954 | 4 I / 476 B |
| pve5 | E6lp | 1,075,170 | 429.6 | 0.987735 | 4 I / 476 B |

Within each host, all five checked VBR conditions had the same sequence of
decoded-frame hashes. The CQP conditions did not. On pve5, low-power CQP
was 2.9% smaller than standard CQP, with SSIM lower by 0.000219. Relative
to production VBR, its measured bitrate was about 74% lower and SSIM was
lower by 0.003733. No equal-bitrate or perceptual-quality equivalence is
claimed. This is one 20-second source segment, not a representative quality
corpus or full-film quality measurement.

## Sustained follow-up

The full video-only source was copied locally on both hosts, with identical
hashes. Each condition encoded the same first 3,551 seconds, without
`-stream_loop` or `-re`, to null output. All four returned zero with
85,139 frames, 3,550.963417 seconds of final output timestamp duration,
and no dropped or duplicated frames.

The first pair was pve2 E0 plus pve5 E4; the second was pve2 E4 plus pve5
E0. Different hosts ran concurrently, each using local input and only one
benchmark at a time. This reduced elapsed investigation time without
concurrent test streams on the same GPU. The pve2 namespace and the queue
restoration guard remained in place until both pairs completed.

| Host / condition | FFmpeg wall s | Launcher wall s | Speed × | CPU % | Render / Video % | Peak °C | Thermal increment |
|---|---:|---:|---:|---:|---:|---:|---:|
| pve2 E0 | 533.53 | 533.72 | 6.66 | 188 | 90 / 10 | 81 | 184 |
| pve2 E4 | 444.42 | 444.82 | 7.99 | 23 | 100 / 21 | 79 | 20 |
| pve5 E0 | 475.36 | 475.99 | 7.47 | 148 | 99 / 6 | 81 | 2 |
| pve5 E4 | 488.66 | 489.40 | 7.27 | 17 | 97 / 11 | 87 | 0 |

Timestamped progress and temperature samples give the following time windows.
Window speeds use output-duration differences divided by elapsed-time
differences, rather than comparing cumulative speed strings.

| Host / condition | First minute: speed / peak °C | After minute 5: speed / peak °C | GPU MHz, first / late mean |
|---|---|---|---|
| pve2 E0 | 6.51× / 81 | 6.76× / 69 | 945 / 981 |
| pve2 E4 | 7.92× / 79 | 8.10× / 69 | 1081 / 1083 |
| pve5 E0 | 7.44× / 81 | 7.65× / 68 | 1090 / 1087 |
| pve5 E4 | 7.42× / 85 | 7.33× / 87 | 1098 / 1063 |

No run reproduced the earlier severe sustained collapse. On pve5, late
GPU PL1 assertions occupied about 5.3% of E0 samples and 9.3% of E4 samples.
The hotter E4 package despite far less FFmpeg CPU work illustrates why
shared-host temperature/power cannot be attributed exclusively to the
tested pipeline. The different source content over time also prevents
interpreting every speed fluctuation as throttling.

Low-power CQP remains a short-run capability result; it was not promoted
to a production-equivalent sustained test. The validated VBR follow-up
was E0 versus E4. Raw minute windows are retained in
`dist/transcode-investigation/sustained-analysis.json`.

## Reproduction and measurement details

The source is the file named in the brief:

```text
/media/Videos/Just Downloaded/michael.che.shot.in.minnesota.2026.720p.web.h264-jff.mkv
```

A single video-only sample was stream-copied for 120 seconds, then copied
byte-for-byte between hosts. Its probed duration was 120.161 seconds and
start timestamp 0.083 seconds. SHA-256:

```text
c9ddcd163f8ac061e98fe005765dc063b78e40d4b170fda0bdaf1ebb78377db0
```

The full video-only local input for sustained tests had SHA-256:

```text
b06ebf3ab7977918a57f1e3acd530eae38b0b6dd5a68ed12bdf2130d24da7ee1
```

Sample preparation, inside the offline userspace:

```sh
ffmpeg -hide_banner -v error -nostdin -i "$source" \
  -t 120 -map 0:v:0 -c copy \
  /var/tmp/sqzarr-investigation-20261020-sample.mkv
```

Canonical E0 command, inside the selected userspace:

```sh
env LIBVA_DRIVER_NAME=iHD ffmpeg -hide_banner -nostdin -nostats \
  -loglevel verbose -benchmark \
  -init_hw_device vaapi=va:/dev/dri/renderD128 -filter_hw_device va \
  -i /var/tmp/sqzarr-investigation-20261020-sample.mkv \
  -t 90 -map 0:v:0 \
  -vf 'format=nv12,hwupload,scale_vaapi=w=min(1920\,iw):h=-2' \
  -c:v hevc_vaapi -b:v 2300k -maxrate 4000k -bufsize 4600k \
  -an -sn -progress pipe:1 -f null -
```

Canonical E4:

```sh
env LIBVA_DRIVER_NAME=iHD ffmpeg -hide_banner -nostdin -nostats \
  -loglevel verbose -benchmark \
  -hwaccel vaapi -hwaccel_device /dev/dri/renderD128 \
  -hwaccel_output_format vaapi \
  -i /var/tmp/sqzarr-investigation-20261020-sample.mkv \
  -t 90 -map 0:v:0 -vf 'scale_vaapi=w=min(1920\,iw):h=-2' \
  -c:v hevc_vaapi -b:v 2300k -maxrate 4000k -bufsize 4600k \
  -an -sn -progress pipe:1 -f null -
```

E1 removes device/filter/encoder options from E0. E2 uses
`-vf format=nv12,hwupload -c:v wrapped_avframe`. E3 removes only scale.
E5 adds `-async_depth 1`, `4`, or `8`. E6 uses E4 and replaces
`-b:v 2300k -maxrate 4000k -bufsize 4600k` with
`-low_power 0 -rc_mode CQP -qp 26` or
`-low_power 1 -rc_mode CQP -qp 26`.

pve2 wraps these commands with:

```sh
chroot /var/lib/lxc/260/rootfs taskset -c 2,3 timeout -s TERM -k 5 180 ...
```

pve5 uses:

```sh
pct exec 261 -- timeout -s TERM -k 5 180 ...
```

Each raw `*.result.json` contains the actual complete argument array,
exit code, benchmark CPU/real times, launcher wall time and final progress.
The sustained runs use the full input and `-t 3551`, with a 900-second
remote FFmpeg timeout. These timeouts enclose the actual encoder, rather
than only an SSH client. FFmpeg stdout/stderr go directly to scratch files,
so unread subprocess pipes cannot block encoding.

Runs were paired across hosts and the condition/host order reversed in
round two. Before each run the harness waited for package temperature
at or below 60°C, bounded at 40 seconds; actual starting temperatures were
recorded. Baseline starts were 51–58°C on pve2 and 56–59°C on pve5.
E4 starts were 52–57°C and 54–58°C. Cooling and background load were
observed, not forcibly controlled.

The harness sampled sysfs, `/proc/<pid>/task/*/stat`, thermal counters,
RAPL and load about every 250 ms. `intel_gpu_top -J -s 250 -o -` supplied
engine activity, and `turbostat --quiet --interval 1 --show
CPU,Busy%,Bzy_MHz,PkgWatt,GFXWatt,CoreTmp,PkgTmp` supplied effective
frequencies/power. Its telemetry output was checked before the matrix.
The container's enforcing host cgroup is `/sys/fs/cgroup/lxc/261`; its
inner namespace reports unlimited memory and is not the correct parent
limit. Early probes and first E0/E1/E2 pve5 runs used that inner view;
subsequent records use the enforcing parent.

## Hypotheses and limitations

| Hypothesis | Outcome |
|---|---|
| Full-boost CPU decode/conversion/upload saturation | Rejected as the primary short-run ceiling; those stages exceed complete-encode throughput and E4 removes most CPU work without a large pve5 speed gain. |
| Default async depth prevents useful overlap | Depth 1 hurts; increasing 2 to 4/8 adds little. Internal driver synchronization is not separately isolated. |
| Both hosts use the standard encoder path | Confirmed by actual initialization logs. Low-power selects a different path on pve5 and removes measured Render/3D activity. |
| Power/thermal policy explains all parity | Not established. Fixed sampled policies coexist with thermal events and brief flags. Sustained evidence is reported separately. |
| cpufreqd hot rule switches policy mid-run | No sampled switch; polling cannot rule out an extremely brief unsampled event. |
| Userspace version mismatch | Rejected; versions match. Driver capability/output differences remain. |
| Competing GPU workloads | No other named GPU client observed. CPU/package contention from other guests remains a limitation. |
| Memory/upload bandwidth is the sole bottleneck | Not supported by E2. Internal GPU memory behavior was not isolated. |

The earlier 1.2 GHz experiment establishes sensitivity at low CPU clocks,
not saturation at full boost. Stage times are pipelined and were not
subtracted as additive costs. Equal MMIO limits do not imply equal actual
package power. No BIOS, power-policy, frequency-limit, firmware or
container-resource change was used to explain these results.

All 75 matrix encodes succeeded; there were no loop/reinitialization
failures with the continuous sample. The separate low-power VBR probes
failed and are not benchmarks. pve5 selected `EncSliceLP` but reported:
`Driver does not support any RC mode compatible with selected options
(supported modes: CQP).` pve2 lacked a usable HEVC low-power entrypoint.
No fallback encode was silently included in a successful-condition row.

## Recommendation for SQZARR

Propose an opt-in GPU-resident path in `internal/transcoder/encoder.go`
for the tested H.264 8-bit input class. Put the VAAPI hardware-decode
options before `-i`, retain the scale filter, HEVC VBR parameters and
default async depth 2. Its strongest demonstrated benefit is CPU
headroom; pve2 also gained 20% in the full-input comparison. No reliable
pve5 speed improvement was demonstrated. The scale is
already passed through for this input, so removing it is unnecessary.

Detect support using an actual decode/filter/encode probe for the input
codec/profile/depth and device, not the CPU name or `vainfo` alone.
Cache capabilities with the FFmpeg/driver/device identity. On hardware
decode or surface incompatibility, retry the existing software-decode
plus hardware-encode path using a new scratch output; retain the existing
software-encoder fallback if hardware encoding itself fails. Keep retries
per job and never replace source media with incomplete output.

Before making it default, validate additional motion/grain/dark content,
other input codecs/depths, seeking, subtitles/audio preservation, color
metadata, memory pressure and end-to-end media replacement. Preserve the
same output validation gates. Do not set low-power CQP as the automatic
replacement for bitrate-controlled production encoding, and do not raise
async depth merely because a larger value is accepted.

### HuC maintenance-window proposal — not applied

The successful low-power CQP result and failed low-power VBR probe justify
a separate approved pve5 maintenance experiment. Intel's matching driver
release documents HuC as necessary for low-power bitrate control and
lists `enable_guc=2` for Tiger Lake.
[Intel media-driver 25.2.3 documentation](https://github.com/intel/media-driver/blob/intel-media-25.2.3/README.md).

Prerequisites: console/rollback access, a guest/Plex maintenance window,
drained SQZARR queue with its original state recorded, backups of the
current boot/module settings and a known-good boot entry. Recheck the
then-running kernel. This kernel's `modinfo` describes bit 2 as HuC load;
`tgl_huc.bin`, `tgl_huc_7.9.3.bin` and Tiger Lake GuC files were present.
Do not infer successful authentication from file presence.

The proposed single configuration change is an isolated i915 module
option requesting `enable_guc=2`, preserving existing i915 settings, then
regenerating the applicable initramfs and rebooting in the window. Do not
add GuC submission, change power policy, or blindly overwrite `i915.conf`.
Validate actual HuC authentication through debugfs and narrow firmware
logs, then test explicit low-power VBR at the existing bitrate/buffer
settings against standard VBR. Require aligned quality/bitrate checks,
sustained measurements, working Plex, healthy guests/storage and restored
queue state before considering a production change.

Risks include reboot downtime, changed shared-GPU behavior and firmware/
driver regressions. Roll back by removing only the added HuC option
(or restoring its backed-up prior value), rebuilding the relevant
initramfs and rebooting to the known-good configuration. Verify the
original module setting, service health and queue state. No raw power
limit override or thermal-protection change is proposed.

## Operational closure and raw evidence

The live queue pause state was captured before each session, then the queue
was paused and confirmed drained. It was originally **unpaused**, and the
finally paths restored **unpaused** after the probes, matrix, quality tests
and sustained tests. Final API verification returned `paused: false` and
the Intel VAAPI encoder. Active production media jobs were not killed.

CT260 stayed stopped throughout. Its filesystem was unmounted after every
offline session and after scratch cleanup; the final mountpoint check
confirmed it was unmounted. Private bind mounts disappeared with their
mount namespaces. CT261 remained running and SQZARR was active. pve2's
pools reported healthy, NFS was active, and CT261's media mount was readable.
No owned FFmpeg process or `intel_gpu_top`/`turbostat` process remained.

Only this investigation's named sample, full-input and encoded-output
scratch files were deleted, including the two staging media copies on
pve5. Scripts and measurement logs were retained. Production media,
application binaries/configuration and encoder defaults were unchanged.
No guest/service shutdown, reboot, power-policy change, HuC/GuC enablement,
application deployment, commit or push occurred. Existing and concurrent
workspace changes were preserved. The configuration containing credentials
was never printed or copied; queue state came from the local API.

Telemetry-only packages installed after clean apt simulations:

- Both hosts: `intel-gpu-tools 2.0-1`, `libdrm-nouveau2 2.4.124-2`,
  `libxv1 2:1.0.11-1.1+b3`.
- pve5 additionally: `linux-cpupower` and `libcpupower1`
  `6.12.113-1`. On pve2, `dpkg-query -S /usr/sbin/turbostat`
  confirmed that its already-installed `linux-cpupower` owned the tool.

There were no package removals, upgrades, network changes or service
restarts in those installs. Tools and libraries were left installed.

Raw evidence and reproducible harnesses are retained in ignored
`dist/transcode-investigation/` and `dist/investigation-*.py` /
`dist/investigation-*.sh`. Host copies are under
`/var/tmp/sqzarr-investigation-20261020/` on pve2 and pve5. They include
userspace inventories, exact commands, progress/stderr, GPU JSON,
thread/sysfs/RAPL samples, turbostat records, quality/frame hashes,
capability errors and closure logs. Media scratch inputs/outputs are
not required to retain those measurements.
