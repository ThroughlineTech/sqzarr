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

- **VAAPI (Intel):** 5–20× faster than software, depending on hardware
- **NVENC (NVIDIA):** 10–50× faster, excellent quality
- **Software (libx265):** 0.2–2× realtime speed (depends on CPU cores and file complexity)

For typical 1080p HEVC files on a modern i5 or i7, hardware encoding takes minutes; software encoding takes 1–4 hours.

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
