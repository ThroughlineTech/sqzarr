# SQZARR Container Deployment Guide

Running SQZARR in a containerized environment requires careful configuration to enable hardware acceleration. This guide covers common container runtimes and how to pass through the necessary devices.

## Overview

SQZARR supports three hardware encoders for HEVC transcoding:
- **VAAPI** (Intel/AMD Linux) — requires `/dev/dri/renderD128`
- **VideoToolbox** (Apple Silicon) — native, no device pass-through needed
- **NVENC** (NVIDIA) — requires NVIDIA CUDA runtime
- **Software Fallback** — libx265, works on any system but slower

If hardware devices are not available or passed through to your container, SQZARR automatically falls back to software encoding. Check the startup logs to see which encoder was selected.

## Proxmox LXC Containers (Unprivileged)

### Prerequisites
- Host with Intel or AMD GPU (iGPU or dedicated)
- Proxmox 6.0+ with LXC support

### Configuration

1. **Add device mapping in Proxmox UI:**
   - Open the container configuration
   - Go to **Resources** tab
   - Scroll to bottom; click **Add** → **Device** (if available)
   - OR edit the container config directly:

2. **Edit `/etc/pve/nodes/{nodename}/lxc/{vmid}.conf`:**
   ```
   # Enable nesting and FUSE for unprivileged container
   features: nesting=1,fuse=1
   
   # Pass through GPU device
   dev0: /dev/dri/renderD128,allow_cgroup_access=1
   ```

3. **Fix permissions (host-side):**
   ```bash
   # Ensure render device is readable
   ls -la /dev/dri/renderD128
   # Should be: crw-rw---- 1 root video
   ```

4. **Restart the container:**
   ```bash
   # From Proxmox host
   pct reboot {vmid}
   ```

5. **Verify device is available inside container:**
   ```bash
   # From inside container
   ls -la /dev/dri/renderD128
   # If missing, device pass-through failed
   ```

### Troubleshooting Unprivileged LXC

**Device not visible in container:**
- Ensure `features: nesting=1` is set
- Check that the device path is correct (`renderD128` for Intel, not `renderD0`)
- Verify host permissions: `ls -la /dev/dri/*`
- Try adding `raw.lxc: lxc.mount.auto = proc sys cgroup` to container config

**"Permission denied" errors:**
- Add the container user to the `video` group
- Or add `lxc.cgroup2.devices.allow: c 226:* rwm` to raw LXC config

**VAAPI detection fails but device is present:**
- Install VAAPI driver on host: `apt-get install intel-media-driver i965-va-driver libva-drm2`
- Verify with `vainfo` on host
- Container inherits driver access automatically

## Docker Containers

### Docker with VAAPI (Intel/AMD)

```bash
docker run \
  --device /dev/dri/renderD128 \
  --device /dev/dri/card0 \
  --group-add video \
  -e SQZARR_LOG_LEVEL=debug \
  sqzarr:latest
```

- `--device /dev/dri/renderD128` — render device (required for encoding)
- `--device /dev/dri/card0` — card device (may be needed for some drivers)
- `--group-add video` — allow access to video group

### Docker with NVENC (NVIDIA)

```bash
docker run \
  --gpus all \
  -e SQZARR_LOG_LEVEL=debug \
  sqzarr:latest
```

Or specify GPU:
```bash
docker run \
  --gpus device=0 \
  -e SQZARR_LOG_LEVEL=debug \
  sqzarr:latest
```

Requires NVIDIA Container Runtime. Install:
```bash
# Ubuntu/Debian
curl https://get.docker.com | sh
distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
curl -s -L https://nvidia.github.io/nvidia-docker/gpgkey | apt-key add -
apt-get update && apt-get install -y nvidia-docker2
systemctl restart docker
```

## Verifying Hardware Encoder Selection

Check the startup logs to see which encoder was selected:

```bash
# Tail the container logs
docker logs -f sqzarr_container  # Docker
pct exec {vmid} journalctl -u sqzarr -f  # Proxmox LXC

# Look for lines like:
# "encoder selection complete", "selected": "Intel VAAPI (hevc_vaapi)"
```

If you see "Software (libx265)", then:
1. Hardware device was not detected
2. Check device pass-through configuration (steps above)
3. Verify ffmpeg has hardware encoder support: `ffmpeg -encoders | grep hevc`

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

```bash
# Docker
docker run \
  -e SQZARR_LOG_LEVEL=debug \
  sqzarr:latest

# Proxmox LXC
pct exec {vmid} /bin/bash -c "SQZARR_LOG_LEVEL=debug /usr/local/bin/sqzarr serve"
```

Debug logs show:
- All encoders probed and their results
- Device paths tested (e.g., `/dev/dri/renderD128`)
- Why a probe failed (device not available, permission denied, etc.)
- Which encoder was finally selected

## Further Reading

- [VAAPI Documentation](https://github.com/intel/media-driver)
- [NVIDIA Container Runtime](https://github.com/NVIDIA/nvidia-docker)
- [FFmpeg Hardware Encoding](https://trac.ffmpeg.org/wiki/Encode/HEVC)
