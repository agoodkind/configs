#!/usr/bin/env bash
# Post-reboot verification for Radeon R7 360 VFIO passthrough on suburban.
# Run with sudo. Exits non-zero on any expected check that fails.

set -u

pass=0
fail=0

check() {
    local label="$1"
    local cmd="$2"
    local expect="$3"
    local out
    out=$(eval "$cmd" 2>&1 || true)
    if echo "$out" | grep -qE "$expect"; then
        echo "PASS: $label"
        pass=$((pass + 1))
    else
        echo "FAIL: $label"
        echo "  expected match: $expect"
        echo "  command       : $cmd"
        echo "  output        :"
        echo "$out" | sed 's/^/    /'
        fail=$((fail + 1))
    fi
}

echo "=== Phase 2 verification ==="
echo

check "cmdline has intel_iommu=on" "cat /proc/cmdline" "intel_iommu=on"
check "cmdline has iommu=pt"       "cat /proc/cmdline" "iommu=pt"
check "cmdline has video=efifb:off" "cat /proc/cmdline" "video=efifb:off"
check "Radeon VGA bound to vfio-pci" "lspci -nnk -d 1002:6658" "Kernel driver in use: vfio-pci"
check "Radeon HDMI audio bound to vfio-pci" "lspci -nnk -d 1002:aac0" "Kernel driver in use: vfio-pci"
check "GT 610 still bound to nouveau" "lspci -nnk -s 02:00.0" "Kernel driver in use: nouveau"
check "vfio_pci module loaded" "lsmod | awk '\$1 == \"vfio_pci\"'" "vfio_pci"
check "vfio_iommu_type1 loaded" "lsmod | awk '\$1 == \"vfio_iommu_type1\"'" "vfio_iommu_type1"
check "IOMMU group 1 contains 01:00.0" "ls /sys/kernel/iommu_groups/1/devices/" "0000:01:00.0"
check "IOMMU group 1 contains 01:00.1" "ls /sys/kernel/iommu_groups/1/devices/" "0000:01:00.1"
check "radeon module NOT loaded" "lsmod | awk '\$1 == \"radeon\" {print \"FOUND\"}'" "^$"
check "amdgpu module NOT loaded" "lsmod | awk '\$1 == \"amdgpu\" {print \"FOUND\"}'" "^$"

echo
echo "=== summary ==="
echo "passed: $pass"
echo "failed: $fail"

echo
echo "=== reference: dmesg vfio/iommu lines ==="
dmesg --notime | grep -iE "vfio|iommu" || true

if [ "$fail" -gt 0 ]; then
    exit 1
fi
