# WORKFLOW.md
# SQZARR - Current Agent Workflow

This file is the current execution guide for agents working in this repository.
Legacy planning artifacts have been removed from the active docs set.

## Canonical docs

Use these as source of truth in this order:
1. README.md (product behavior, install, operations)
2. docs/architecture.md (current system shape)
3. docs/container-deployment.md (GPU/container runtime specifics)
4. sqzarr.toml.example (config keys/defaults)

If any doc disagrees with the code, treat code + tests as authoritative and update docs.

## Working norms

- Prefer Linux shell workflows and .sh scripts at repo root.
- Do not add Windows-only helper scripts (.bat/.ps1) unless explicitly requested.
- Keep changes small and focused; avoid speculative refactors.
- Preserve no-Docker runtime constraint.

## Build and verify

- Build the frontend, Linux release binary, and Linux test binaries locally on
  the workstation. **Do not compile inside the media-server container.**
  Compiling the SQLite dependency exhausted the container's memory during the
  Plex verification release; the container is for executing tests, validation,
  and deployment only.
- Build the frontend first (`cd frontend && npm ci && npm run build`) so the Go
  release embeds the updated UI. Cross-compile the release with
  `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o dist/sqzarr-linux-amd64 ./cmd/sqzarr`.
- Cross-compile each package's tests locally using
  `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o dist/linux-tests/<package>.test ./internal/<package>`.
  Repeat for every package with tests; include required build tags for any
  integration tests. Compilation alone does not count as passing tests.
- The production deployment is **container 261 on pve5**, reachable
  via `ssh -l root pve2.throughlinetech.net` followed by `ssh root@pve5`.
  It retains `192.168.29.211:8080`, the original configuration/database, 2 CPU
  cores, and 1024 MiB RAM. CT260 also has 1024 MiB RAM for equivalent validation.
  Container **260 on pve2 is retained stopped with
  autostart disabled**. Never start both containers: they share the IP/MAC and
  media library. Container 261 has autostart enabled.
- Transfer locally built artifacts to pve5 with `scp`, then into the active
  container with `pct push 261`. Run commands using
  `pct exec 261 -- <command>` (`pct enter 261` is the interactive alternative).
  When extracting workstation-created archives, use `tar --no-same-owner` to
  avoid incompatible Windows ownership IDs.
- Execute the compiled test binaries in a separate release directory inside
  container 261 and require them to pass before installing the release.
  Run static checks such as `GOOS=linux GOARCH=amd64 go vet ./...` locally.
- Pause the queue, wait for active transcodes to finish, and back up
  `/usr/local/bin/sqzarr`. Install the locally built release, restart the
  `sqzarr` systemd service, and verify service health, the served frontend, and
  affected features. Restore the previous queue pause state after validation.
  If startup or health checks fail, restore the backup binary and restart.
- `deploy.sh` and its `build_now.sh` wrapper currently compile remotely; do not
  use them for this container unless updated to follow the local-build flow.
  `make all` / `make test` remain useful on a local Linux workstation; on Windows,
  use cross-compilation and run the resulting tests in the Linux container.

### Security verification

- CI and release builds use Go 1.26 and Node 24 on Ubuntu 24.04. GitHub setup
  actions are pinned to reviewed commits; update these deliberately.
- Install the same scanner as CI with
  `go install github.com/securego/gosec/v2/cmd/gosec@v2.29.0`, then run
  `gosec ./cmd/... ./internal/... ./scripts/...`. On Windows set `GOOS=linux`
  and `GOARCH=amd64` for the scan. The explicit package roots exclude local
  generated files in `dist/` and frontend dependencies.
- Keep all scanner rules enabled. Any `#nosec` exception must identify the
  specific rule and explain the trust boundary at that call site. Current
  exceptions cover fixed system/config paths, worker-recorded original paths
  selected by numeric ID, direct FFmpeg argument lists, and library directory
  permissions needed by Plex.
- Runtime config updates and application logs enforce owner-only file access
  (`0600`), including existing files. Held-original directories are created
  with `0750`; restored media directories retain `0755` for Plex access.
- Frontend verification includes `npm audit` with development dependencies.
  Tailwind 4 uses `@tailwindcss/postcss`; the theme and forms plugin live in
  `frontend/src/index.css`. Keep `tailwind-merge` on a Tailwind 4-compatible
  version when updating the styling toolchain.

### Production host settings and rollback

- pve5 uses the firmware `balanced` profile and the CPU policy matched to
  pve2/pve3. The custom `pve-performance-profile.service` is disabled and its
  CT261 startup dependency was removed. Preserve `intel_pstate=disable`,
  `acpi-cpufreq`, cpufreqd and TLP configuration. cpufreqd selects performance
  on AC and can lower its target under the configured heat/load conditions;
  TLP's configured conservative governor is not evidence that cpufreqd failed.
  Do not replace these policies or override raw power limits during deployment.
- CT261 is explicitly pinned with `lxc.cgroup2.cpuset.cpus: 2,3` in
  `/etc/pve/lxc/261.conf`. These are separate physical cores on pve5; CPUs 6 and
  7 are their SMT siblings. The container retains two logical CPUs and 1024 MiB
  RAM. Pinning is not exclusive reservation from other host workloads. Check
  the effective assignment with `pct exec 261 -- cat /sys/fs/cgroup/cpuset.cpus.effective`.
  Proxmox's rebalancer respects this explicit cpuset. Recheck topology before
  moving the container to another host.
- CT260 on pve2 also has explicit `lxc.cgroup2.cpuset.cpus: 2,3` following the
  adaptive release validation. Current topology confirmed these are distinct
  physical cores. Its two-CPU allocation and 1024 MiB RAM are retained; this
  pinning is not an exclusive reservation from other guests.
- pve5 uses the Intel i7-1185G7 GPU through
  `dev0: /dev/dri/renderD128,gid=104,mode=0660`; SQZARR's service includes the
  matching supplementary group. A real 1080p HEVC VAAPI encode, media write
  access, and Plex verification (18 libraries) passed at cutover.
- Media remains at `/media/Videos` inside the container, bound from
  `/mnt/pve/fileserver-storage/Videos` on pve5 (NFS from pve2). The host drop-in
  `/etc/systemd/system/pve-container@261.service.d/media-mount.conf` requires
  that mount before container startup. The original pve2 container accessed
  storage locally; account for NFS when comparing performance.
- A stopped-container backup and original CT260 configuration are retained in
  `/var/lib/vz/sqzarr-pve5-test` on pve2; the restore archive is also retained
  in that directory on pve5. These contain private application state and must
  remain on the hosts, outside Git.
- Before switching back, pause and drain CT261, stop its SQZARR service, and
  synchronize current application data/SQLite state and configuration to CT260
  while both services are stopped, preserving backups. CT260's retained state
  becomes stale as CT261 processes the shared media. Stop CT261 and disable its
  autostart before starting CT260; validate CT260 before resuming its queue and
  restoring its autostart. See docs/container-deployment.md for comparison and
  rollback guidance.
- For the adaptive transcoding release, follow
  `docs/transcode-product-release-plan.md` and review the actual test,
  deployment and qualification evidence in
  `docs/transcode-product-release-outcome.md`. Hardware decoding and low-power
  encoding are separate capabilities. Do not infer working low-power VBR from
  a successful CQP test or from the advertised GPU generation.
- Host HuC/module/initramfs maintenance affects other guests. It requires a
  prepared backup/rollback and explicit maintenance approval. The user approved
  the pve5 HuC maintenance for this release; that is not blanket authorization
  for future host reboots. Keep qualification artifacts and private backups
  outside Git.

## Documentation policy

- Keep docs/ minimal and current.
- Add historical notes only when they are operationally useful.
- Avoid reintroducing large planning or interview transcripts into active docs.

## Change checklist for agents

Before finishing:
1. Confirm scripts/docs references are current.
2. Run targeted verification for touched areas.
3. Update README/docs when behavior or operator workflow changed.
4. Leave the repository cleaner than you found it.
