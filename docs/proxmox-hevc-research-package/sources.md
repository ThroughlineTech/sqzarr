# Sources and version boundaries

Research completed 20 October 2026. Source documents are linked, not redistributed.

U: The original user-supplied brief is included as original-research-brief.txt. Host measurements are reported observations from that brief, not new remote measurements.

## S1. Intel media-driver README: platform support, E/Es paths, build variants and HuC prerequisites

[https://github.com/intel/media-driver](https://github.com/intel/media-driver)

Current upstream; verify installed driver/build and firmware state.

## S2. Intel Tiger Lake PRM Volume 11, HEVC/VP9 encoder architecture, PDF page 24

[https://cdrdv2-public.intel.com/705826/intel-gfx-prm-osrc-tgl-vol-11-media-engines.pdf](https://cdrdv2-public.intel.com/705826/intel-gfx-prm-osrc-tgl-vol-11-media-engines.pdf)

Architecture documentation, not a throughput benchmark.

## S3. Linux i915 platform engine definitions

[https://github.com/torvalds/linux/blob/master/drivers/gpu/drm/i915/i915_pci.c](https://github.com/torvalds/linux/blob/master/drivers/gpu/drm/i915/i915_pci.c)

Upstream platform masks; confirm runtime enabled engines.

## S4. Intel Core i7-1185G7 specifications

[https://www.intel.com/content/www/us/en/products/sku/208664/intel-core-i71185g7-processor-12m-cache-up-to-4-80-ghz-with-ipu/specifications.html](https://www.intel.com/content/www/us/en/products/sku/208664/intel-core-i71185g7-processor-12m-cache-up-to-4-80-ghz-with-ipu/specifications.html)

Advertised maxima and two codec engines are not sustained measurements.

## S5. Intel Core i5-8350U specifications

[https://www.intel.com/content/www/us/en/products/sku/124969/intel-core-i58350u-processor-6m-cache-up-to-3-60-ghz/specifications.html](https://www.intel.com/content/www/us/en/products/sku/124969/intel-core-i58350u-processor-6m-cache-up-to-3-60-ghz/specifications.html)

UHD 620 and 1.10 GHz graphics maximum.

## S6. FFmpeg 7 VAAPI common options

[https://ffmpeg.org/doxygen/7.0/vaapi__encode_8h_source.html](https://ffmpeg.org/doxygen/7.0/vaapi__encode_8h_source.html)

low_power and async_depth defaults; compare installed help.

## S7. FFmpeg 7 VAAPI entrypoint, rate-control and quality initialization

[https://ffmpeg.org/doxygen/7.0/libavcodec_2vaapi__encode_8c_source.html](https://ffmpeg.org/doxygen/7.0/libavcodec_2vaapi__encode_8c_source.html)

Normal list includes LP; quality buffer is conditional.

## S8. FFmpeg VAAPI encoder documentation

[https://ffmpeg.org/ffmpeg-codecs.html#VAAPI-encoders](https://ffmpeg.org/ffmpeg-codecs.html#VAAPI-encoders)

Current option semantics; support is driver/version dependent.

## S9. FFmpeg 7 HEVC encoder defaults

[https://ffmpeg.org/doxygen/7.0/vaapi__encode__h265_8c_source.html](https://ffmpeg.org/doxygen/7.0/vaapi__encode__h265_8c_source.html)

GOP/B-frame defaults, not a guarantee of actual driver output.

## S10. FFmpeg 7 scale_vaapi pass-through implementation

[https://ffmpeg.org/doxygen/7.0/vf__scale__vaapi_8c_source.html](https://ffmpeg.org/doxygen/7.0/vf__scale__vaapi_8c_source.html)

No-op behavior conditional on geometry, format and color properties.

## S11. Intel 11th-generation Core datasheet volume 1, thermal management

[https://cdrdv2-public.intel.com/631121/631121_TGL%20Datasheet%20Volume1of2%20rev012_PUBLIC.pdf](https://cdrdv2-public.intel.com/631121/631121_TGL%20Datasheet%20Volume1of2%20rev012_PUBLIC.pdf)

Package constraints, Tau and OEM tuning; not proof of active Lenovo settings.

## S12. Linux intel_pstate versus acpi-cpufreq

[https://docs.kernel.org/admin-guide/pm/intel_pstate.html](https://docs.kernel.org/admin-guide/pm/intel_pstate.html)

ACPI turbo representation and driver differences.

## S13. cpufreqd configuration manual

[https://manpages.debian.org/testing/cpufreqd/cpufreqd.conf.5.en.html](https://manpages.debian.org/testing/cpufreqd/cpufreqd.conf.5.en.html)

Rule inputs and scoring; installed config still needed.

## S14. cpufreqd 2.4.2-3 CPU plugin source

[https://sources.debian.org/src/cpufreqd/2.4.2-3/src/cpufreqd_cpu.c](https://sources.debian.org/src/cpufreqd/2.4.2-3/src/cpufreqd_cpu.c)

Exact supplied version: aggregate accounting and nice weighting.

## S15. cpufreqd 2.4.2-3 ACPI temperature source

[https://sources.debian.org/src/cpufreqd/2.4.2-3/src/cpufreqd_acpi_temperature.c](https://sources.debian.org/src/cpufreqd/2.4.2-3/src/cpufreqd_acpi_temperature.c)

Conditional on use of this plugin/directive.

## S16. cpufreqd 2.4.2-3 rule application and getter source

[https://sources.debian.org/src/cpufreqd/2.4.2-3/src/main.c](https://sources.debian.org/src/cpufreqd/2.4.2-3/src/main.c)

Unchanged-profile skip and cached applied-profile state.

## S17. Linux CPU performance scaling

[https://docs.kernel.org/admin-guide/pm/cpufreq.html](https://docs.kernel.org/admin-guide/pm/cpufreq.html)

Policy/request values versus actual CPU frequency.

## S18. turbostat manual

[https://manpages.debian.org/trixie/linux-cpupower/turbostat.8.en.html](https://manpages.debian.org/trixie/linux-cpupower/turbostat.8.en.html)

Check local options/columns; busy MHz, power and sampling semantics.

## S19. Linux i915 GT power-management sysfs implementation

[https://kernel.googlesource.com/pub/scm/linux/kernel/git/xiang/erofs/+/d0989d01c66fed6a741820a96b8cca6688f183ff/drivers/gpu/drm/i915/gt/intel_gt_sysfs_pm.c](https://kernel.googlesource.com/pub/scm/linux/kernel/git/xiang/erofs/+/d0989d01c66fed6a741820a96b8cca6688f183ff/drivers/gpu/drm/i915/gt/intel_gt_sysfs_pm.c)

Pinned Linux source; paths and availability vary by kernel/platform.

## S20. intel_gpu_top manual

[https://manpages.debian.org/experimental/intel-gpu-tools/intel_gpu_top.1.en.html](https://manpages.debian.org/experimental/intel-gpu-tools/intel_gpu_top.1.en.html)

Device filter, interval and JSON options; verify installed tool.

## S21. Linux DRM client usage statistics

[https://origin.kernel.org/doc/html/v6.1/gpu/drm-usage-stats.html](https://origin.kernel.org/doc/html/v6.1/gpu/drm-usage-stats.html)

Per-client counters when the driver exposes them.

## S22. Linux Intel thermal throttle event reporting

[https://cdn.kernel.org/doc/html/latest/admin-guide/thermal/intel_thermal_throttle.html](https://cdn.kernel.org/doc/html/latest/admin-guide/thermal/intel_thermal_throttle.html)

Event counts, shared package reporting and optional time counters.

## S23. Linux power-capping framework

[https://www.kernel.org/doc/html/latest/power/powercap/powercap.html](https://www.kernel.org/doc/html/latest/power/powercap/powercap.html)

Named zones, constraints, energy and enable state.

## S24. TLP project issue 570: X1 Gen9 turbo/profile interaction

[https://github.com/linrunner/TLP/issues/570](https://github.com/linrunner/TLP/issues/570)

Historical 2021 report; does not diagnose the restored current state.

## S25. Intel media-driver issue 1190: Tiger Lake HEVC corruption

[https://github.com/intel/media-driver/issues/1190](https://github.com/intel/media-driver/issues/1190)

Historical 2021 report; output-validation rationale, not a matched diagnosis.

## S26. Linux platform-profile API

[https://www.kernel.org/doc/html/latest/userspace-api/sysfs-platform_profile.html](https://www.kernel.org/doc/html/latest/userspace-api/sysfs-platform_profile.html)

Firmware policy selection is separate from achieved performance.

## S27. TLP architecture

[https://linrunner.de/tlp/developers/architecture.html](https://linrunner.de/tlp/developers/architecture.html)

Current docs; use event behavior, not newer profile features, for TLP 1.8.

## S28. cpufreqd-get manual

[https://manpages.debian.org/bullseye/cpufreqd/cpufreqd-get.1.en.html](https://manpages.debian.org/bullseye/cpufreqd/cpufreqd-get.1.en.html)

Read-only applied-profile request; existing socket required.

## S29. Linux cgroup v2 documentation

[https://docs.kernel.org/admin-guide/cgroup-v2.html](https://docs.kernel.org/admin-guide/cgroup-v2.html)

Hierarchical resource controls and diagnostic counters.

## S30. FFmpeg command-line documentation

[https://ffmpeg.org/ffmpeg.html](https://ffmpeg.org/ffmpeg.html)

Hardware devices, frame limits, progress, benchmark timing and input control.

## S31. FFmpeg filters documentation

[https://ffmpeg.org/ffmpeg-filters.html](https://ffmpeg.org/ffmpeg-filters.html)

Hardware frame transfer, trim/setpts and SSIM semantics.

## S32. Lenovo BIOS support family DS548819

[https://support.lenovo.com/aw/en/downloads/ds548819](https://support.lenovo.com/aw/en/downloads/ds548819)

Indexed official listing corroborates 1.77 identity; latest/fix not established.
