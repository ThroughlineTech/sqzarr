#!/usr/bin/env bash
# Proxmox host comparison collector, v1.0 (2026-10-20).
# Reads host settings and samples existing activity; writes only to stdout/stderr.
# Does not install packages, load modules, change policy, or generate stress load.
# Run on the Proxmox HOST as root, not inside a VM or LXC.
#
# Usage:
#   bash pve-host-check.sh idle 15 | tee "pve-$(hostname)-idle.txt"
#   bash pve-host-check.sh load 30 | tee "pve-$(hostname)-load.txt"
# The label is descriptive only: 'load' does NOT start a workload.
# Start the same representative workload before collecting each load report.
# Compare the same input, application settings, worker count and elapsed time.
#
# Optional actual-frequency measurement, if turbostat is already available:
#   turbostat --quiet --interval 1 --num_iterations 15
# Use Bzy_MHz with Busy%; Avg_MHz includes idle time. Supported sensors vary.
#
# References:
# https://www.kernel.org/doc/html/latest/admin-guide/pm/cpufreq.html
# https://www.kernel.org/doc/html/latest/admin-guide/pm/intel_pstate.html
# https://docs.kernel.org/admin-guide/pm/amd-pstate.html
# https://docs.kernel.org/power/powercap/powercap.html
# https://github.com/proxmox/pve-docs/blob/master/qm.adoc
# https://manpages.debian.org/trixie/linux-cpupower/turbostat.8.en.html

set -u
export LC_ALL=C
shopt -s nullglob

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
    sed -n '2,26p' "$0"
    exit 0
fi
label=${1:-snapshot}
sample_seconds=${2:-15}
if [[ ! $sample_seconds =~ ^[1-9][0-9]?$ ]] || (( sample_seconds > 60 )); then
    printf 'Sample duration must be an integer from 1 to 60 seconds.\n' >&2
    exit 2
fi

section() { printf '\n===== %s =====\n' "$*"; }
value() {
    local content
    if [[ -r $1 ]] && content=$(cat -- "$1" 2>/dev/null); then
        printf '%s' "${content//$'\n'/,}"
    else
        printf 'n/a'
    fi
}
show_files() {
    local f
    for f in "$@"; do
        [[ -r $f ]] || continue
        printf '%s = %s\n' "$f" "$(value "$f")"
    done
}
run() {
    printf '\n$'
    printf ' %s' "$@"
    printf '\n'
    if command -v "$1" >/dev/null 2>&1; then
        if command -v timeout >/dev/null 2>&1; then
            timeout 10 "$@" 2>&1 || printf '[Command failed or timed out; continuing.]\n'
        else
            "$@" 2>&1 || printf '[Command failed; continuing.]\n'
        fi
    else
        printf '[Not installed; skipped.]\n'
    fi
}
temperatures() {
    local chip input base sensor found=0
    for chip in /sys/class/hwmon/hwmon*; do
        for input in "$chip"/temp*_input; do
            [[ -r $input ]] || continue
            base=${input%_input}
            sensor=$(value "${base}_label")
            [[ $sensor != n/a ]] || sensor=${base##*/}
            printf '%s | %s | milli-C=%s' "$(value "$chip/name")" "$sensor" "$(value "$input")"
            [[ -r ${base}_crit ]] && printf ' | critical_milli-C=%s' "$(value "${base}_crit")"
            printf '\n'
            found=1
        done
    done
    (( found )) || printf '[No readable hwmon temperature sensors.]\n'
}
frequencies() {
    local p
    printf 'policy | CPUs | scaling_cur_freq_kHz | cpuinfo_cur_freq_kHz\n'
    for p in /sys/devices/system/cpu/cpufreq/policy*; do
        printf '%s | %s | %s | %s\n' "${p##*/}" "$(value "$p/affected_cpus")" \
            "$(value "$p/scaling_cur_freq")" "$(value "$p/cpuinfo_cur_freq")"
    done
    printf 'Snapshots only: scaling_cur_freq may be a requested state; not sustained clocks.\n'
}

section 'HOST / VERSION'
printf 'Collector: 1.0 | Label: %s | Host: %s | UTC: %s\n' \
    "$label" "$(hostname)" "$(date -u +%FT%TZ)"
printf 'Sample duration: %s seconds; no workload is started.\n' "$sample_seconds"
(( EUID == 0 )) || printf 'Not root: some hardware/configuration fields may be unavailable.\n'
run uname -r
run pveversion -v
run uptime
show_files /sys/class/dmi/id/sys_vendor /sys/class/dmi/id/product_name \
    /sys/class/dmi/id/board_name /sys/class/dmi/id/bios_vendor \
    /sys/class/dmi/id/bios_version /sys/class/dmi/id/bios_date

section 'CPU HARDWARE'
lscpu 2>&1 | awk '!/^(Flags:|Vulnerability )/'
printf '\nMicrocode revisions:\n'
awk -F: '/^microcode/ {gsub(/^[ \t]+/, "", $2); print $2}' /proc/cpuinfo | sort -u
run lscpu -e=CPU,ONLINE,SOCKET,CORE,NODE,MAXMHZ,MINMHZ
printf '\nRelevant explicit boot arguments (absence means kernel defaults):\n'
awk '{for(i=1;i<=NF;i++) if ($i ~ /^(intel_pstate|amd_pstate|amd_prefcore|amd_dynamic_epp|intel_idle|processor\.max_cstate|idle|cpufreq|cpuidle|intel_rapl|isolcpus|nohz_full|rcu_nocbs|nosmt|maxcpus|nr_cpus|mitigations|spectre_v2|spec_store_bypass_disable|numa_balancing|intel_iommu|amd_iommu|iommu)(=|\.|$)/) print $i}' /proc/cmdline

section 'CPU FREQUENCY POLICIES (all policies; frequencies in kHz)'
printf 'policy | CPUs | driver | governor | EPP | min | max | hw_min | hw_max | base | BIOS_limit | boost\n'
policies=(/sys/devices/system/cpu/cpufreq/policy*)
(( ${#policies[@]} )) || printf '[No cpufreq policies exposed; not evidence of fixed speed.]\n'
for p in "${policies[@]}"; do
    printf '%s' "${p##*/}"
    for field in affected_cpus scaling_driver scaling_governor energy_performance_preference \
        scaling_min_freq scaling_max_freq cpuinfo_min_freq cpuinfo_max_freq base_frequency bios_limit boost; do
        printf ' | %s' "$(value "$p/$field")"
    done
    printf '\n'
done

section 'GLOBAL POWER / BOOST / IDLE SETTINGS'
show_files /sys/devices/system/cpu/intel_pstate/{status,no_turbo,min_perf_pct,max_perf_pct,hwp_dynamic_boost} \
    /sys/devices/system/cpu/amd_pstate/{status,prefcore} \
    /sys/devices/system/cpu/cpufreq/boost \
    /sys/devices/system/cpu/smt/{active,control} \
    /sys/devices/system/cpu/{online,offline,isolated} \
    /sys/firmware/acpi/platform_profile \
    /sys/class/platform-profile/*/{name,profile} \
    /sys/devices/system/cpu/cpuidle/{current_driver,current_governor_ro} \
    /sys/devices/system/cpu/cpu*/power/energy_perf_bias
printf '\nCPU0 idle-state configuration (reference CPU only):\n'
show_files /sys/devices/system/cpu/cpu0/cpuidle/state*/{name,disable,latency}
printf '\nPower-management services, if present:\n'
if command -v systemctl >/dev/null 2>&1; then
    for service in power-profiles-daemon tuned tlp cpufrequtils cpupower; do
        state=$(systemctl is-active "$service" 2>/dev/null) || :
        printf '%s: %s\n' "$service" "${state:-unknown}"
    done
fi

section 'EXPOSED POWER LIMITS (microwatts / microseconds, as named)'
powerzones=(/sys/class/powercap/*)
for zone in "${powerzones[@]}"; do
    show_files "$zone"/name "$zone"/enabled \
        "$zone"/constraint_*_name "$zone"/constraint_*_power_limit_uw "$zone"/constraint_*_time_window_us
done
printf 'Missing power-limit files mean unavailable here, not unlimited power.\n'

section 'MEMORY / STORAGE INVENTORY'
run free -h
run lsblk -d -o NAME,MODEL,SIZE,ROTA,TRAN
if command -v dmidecode >/dev/null 2>&1; then
    printf '\nDIMM layout and configured speed (SMBIOS reports):\n'
    dmidecode -t 17 2>/dev/null | awk '/^Memory Device$/ || /^[ \t]+(Size|Locator|Bank Locator|Type|Type Detail|Speed|Configured Memory Speed|Configured Clock Speed):/'
fi

section 'LOCAL GUEST CPU / MEMORY SETTINGS'
run qm list
run pct list
printf '\nStored main configuration sections; absent keys use version-specific defaults.\n'
configs=(/etc/pve/local/qemu-server/*.conf /etc/pve/local/lxc/*.conf)
(( ${#configs[@]} )) || printf '[No readable local guest configurations found.]\n'
for cfg in "${configs[@]}"; do
    [[ -r $cfg ]] || continue
    printf '\n%s\n' "$cfg"
    awk '/^\[/ {exit} /^(name|hostname|cpu|cores|sockets|vcpus|cpulimit|cpuunits|affinity|numa|numa[0-9]+|memory|balloon|swap|machine|kvm):/ || /^lxc\.cgroup2?\.(cpu|cpuset)\./' "$cfg"
done

section 'OPTIONAL DIAGNOSTIC TOOLS'
for tool in turbostat cpupower sensors mpstat iostat; do
    printf '%s: ' "$tool"
    command -v "$tool" || printf 'not installed\n'
done

section 'BEFORE SAMPLE: TEMPERATURE / FREQUENCY / PRESSURE'
date -u +%FT%TZ
temperatures
frequencies
show_files /proc/pressure/{cpu,memory,io}
declare -A throttle_before=()
counter_files=(/sys/devices/system/cpu/cpu*/thermal_throttle/{core_throttle_count,package_throttle_count})
for counter in "${counter_files[@]}"; do
    throttle_before["$counter"]=$(value "$counter")
done

section "ACTIVITY SAMPLE: ${sample_seconds}s"
printf 'vmstat: first data row is since-boot average; subsequent rows are 1-second samples.\n'
if command -v vmstat >/dev/null 2>&1; then
    vmstat -w 1 "$((sample_seconds + 1))" 2>&1
else
    printf '[vmstat unavailable; waiting for counter comparison.]\n'
    sleep "$sample_seconds"
fi

section 'THERMAL THROTTLING DURING SAMPLE'
printf 'Only nonzero deltas shown. Repeated package counters must not be summed.\n'
checked=0
changed=0
for counter in "${counter_files[@]}"; do
    previous=${throttle_before[$counter]}
    current=$(value "$counter")
    if [[ $previous =~ ^[0-9]+$ && $current =~ ^[0-9]+$ ]]; then
        checked=$((checked + 1))
        delta=$((current - previous))
        if (( delta != 0 )); then
            printf '%s: before=%s after=%s delta=%s\n' "$counter" "$previous" "$current" "$delta"
            changed=1
        fi
    fi
done
printf 'Readable counters compared: %s\n' "$checked"
(( changed )) || printf 'No nonzero delta observed in readable counters.\n'
printf 'Unavailable/unchanged counters do not rule out other thermal or power limits.\n'

section 'AFTER SAMPLE: TEMPERATURE / FREQUENCY / PRESSURE'
date -u +%FT%TZ
temperatures
frequencies
show_files /proc/pressure/{cpu,memory,io}
printf '\nEND REPORT. Also record workload/input/worker count and completion time.\n'
