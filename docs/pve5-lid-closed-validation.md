# pve5 lid-closed thermal validation — 2026-10-20

The lid was confirmed closed through ACPI. Three consecutive full-length
low-power HEVC VBR encodes showed no sustained temperature increase or
progressive throughput loss compared with the earlier lid-open measurements.
Brief temperature peaks were higher, and one host-wide package thermal
throttle counter increment occurred during the third pass.

| Measurement | Earlier lid-open LP test | Closed pass 1 | Closed pass 2 | Closed pass 3 |
|---|---:|---:|---:|---:|
| Elapsed encode time, seconds | 130.29 | 125.51 | 124.49 | 124.38 |
| Throughput, × realtime | 27.26 | 28.30 | 28.53 | 28.56 |
| CPU package average, °C | 65.02 | 63.99 | 64.01 | 65.05 |
| CPU package peak, °C | 79 | 82 | 85 | 80 |
| Final 30 samples average, °C | 58.47 | 63.73 | 64.13 | 64.63 |
| Maximum package throttle counter delta | 0 | 0 | 0 | 1 |

The three encoding windows total 374.38 seconds (6.24 minutes). Automatic fan
control remained enabled, with approximately 6,560–6,585 RPM at the end of
each pass. No fan, power, BIOS, encoder qualification, or service configuration
settings were changed.

## Method and limits

- CT261 on pve5; the SQZARR queue was paused and drained for validation.
- Used a disposable local copy of the current Michael Che media file.
  The original H.264 source and earlier test copies were no longer present:
  the library now contains its HEVC conversion. Consequently this is a
  representative cooling test, **not a controlled lid-only A/B comparison**.
  Different decode workloads, ambient conditions, and other host activity can
  affect results.
- Used the earlier raw low-power VBR encoder settings: VAAPI hardware decode,
  HEVC `low_power=1`, VBR 2300k, maxrate 4000k, buffer 4600k, async depth 2,
  and preserved audio/subtitle/attachment streams and metadata.
- Each encoder log confirmed `EncSliceLP` and VBR. CPU package temperatures
  and host-wide throttle counters were sampled approximately once per second.
  Counter values are not summed across duplicate per-CPU package counters.
  Sampling can miss brief temperature spikes; one counter increment does
  not establish that this particular encoder caused the event.
- The final output passed a full strict video decode check. All disposable
  input/output files were removed and the original unpaused queue state was
  restored successfully.
- The initial attempt completed one pass at 28.08×, mean 64.15°C, peak 77°C,
  and no throttle increments. A second output filled the 7.8 GB container root
  filesystem. Only this test's disposable files were removed, restoring
  3.3 GB free. The three-pass retry reused a single output to bound disk usage.

The evidence does not support a considerable sustained cooling penalty from
closing the lid for this workload. Peaks rose by as much as 6°C relative to the
earlier test, while average temperatures stayed within approximately 1°C.
Longer workloads, simultaneous Plex transcodes, and warmer room temperatures
remain outside this test.

Local raw evidence: `dist/lid-validation-result.json` (ignored).
Earlier baseline: `dist/adaptive-evidence/pve5/pve5-sustained.json` (ignored).
