#!/usr/bin/env bash
set -euo pipefail

readonly VM_CONFIGURATION=/etc/pve/qemu-server/113.conf
readonly WAN_DEVICE=/sys/class/net/enp2s0f0np0/device
readonly EXPECTED_VF=/sys/bus/pci/devices/0000:02:02.0
readonly SERVICES=(mwan-ifmgr.service mwan-watchdog.service x710-vf-setup.service
	mwan-opnsense-host.service mwan-opnsense-drain.service sriov-vf-lan.service)

if [[ -e "$VM_CONFIGURATION" ]]; then
	printf 'VM113 still exists. Apply the vault/mwan retirement plan before retiring its host services and WAN virtual function.\n' >&2
	exit 1
fi

if [[ ! -r "$WAN_DEVICE/sriov_numvfs" ]]; then
	printf 'The WAN port enp2s0f0np0 does not expose its virtual function count. Verify the current interface and PCI assignment before retirement.\n' >&2
	exit 1
fi
read -r VF_COUNT <"$WAN_DEVICE/sriov_numvfs"
case "$VF_COUNT" in
0) ;;
1)
	if [[ ! "$WAN_DEVICE/virtfn0" -ef "$EXPECTED_VF" ]]; then
		printf 'The WAN virtual function does not match 0000:02:02.0. Verify its current PCI assignment before retirement.\n' >&2
		exit 1
	fi
	;;
*)
	printf 'The WAN port has %s virtual functions. Verify their consumers before retiring the VM113 allocation.\n' "$VF_COUNT" >&2
	exit 1
	;;
esac

main() {
	local configuration
	local grep_status
	local bridge_ports
	local unit_name
	local load_state
	shopt -s nullglob
	bridge_ports=(/sys/class/net/mwanbr/brif/*)
	if [[ "${#bridge_ports[@]}" -ne 0 ]]; then
		printf 'mwanbr still has guest interfaces. Move the retained clients to vmbr0 before retiring the transit bridge.\n' >&2
		exit 1
	fi
	for configuration in /etc/pve/qemu-server/*.conf; do
		if grep -Eq '^hostpci[0-9]+:[[:space:]]*(host=)?(0000:)?02:02(\.0)?([,;[:space:]]|$)' "$configuration"; then
			printf 'The WAN virtual function remains assigned in %s. Remove that assignment before retiring the allocation.\n' "$configuration" >&2
			exit 1
		else
			grep_status=$?
			if [[ "$grep_status" -ne 1 ]]; then
				printf 'The retirement check could not read %s.\n' "$configuration" >&2
				exit "$grep_status"
			fi
		fi
	done

	for unit_name in "${SERVICES[@]}"; do
		load_state=$(systemctl show --property=LoadState --value "$unit_name")
		# A previous retirement can already have removed the unit file.
		if [[ "$load_state" == "not-found" ]]; then
			continue
		fi
		systemctl disable --now "$unit_name"
	done
	read -r VF_COUNT <"$WAN_DEVICE/sriov_numvfs"
	if [[ "$VF_COUNT" -eq 1 ]]; then
		printf '0\n' >"$WAN_DEVICE/sriov_numvfs"
	fi
	read -r VF_COUNT <"$WAN_DEVICE/sriov_numvfs"
	if [[ "$VF_COUNT" -ne 0 ]]; then
		printf 'The WAN virtual function allocation remains active.\n' >&2
		exit 1
	fi

	if [[ -e /sys/class/net/mwanbr ]]; then
		ip link delete dev mwanbr
	fi
	rm -f /etc/systemd/system/mwan-ifmgr.service \
		/etc/systemd/system/mwan-watchdog.service \
		/etc/systemd/system/x710-vf-setup.service \
		/etc/systemd/system/mwan-opnsense-host.service \
		/etc/systemd/system/mwan-opnsense-drain.service \
		/etc/systemd/system/sriov-vf-lan.service \
		/etc/mwan/config.toml \
        /etc/mwan/config.toml.bak-* \
        /etc/mwan/config.toml.before-* \
        /etc/mwan/config.toml.backup* \
		/etc/mwan/secrets.env \
		/etc/mwan-watchdog/network.toml \
		/etc/opnsensectl/config.toml \
		/usr/local/bin/mwan \
        /usr/local/bin/mwan.* \
		/usr/local/sbin/sriov-vf-lan.sh \
		/etc/sysctl.d/60-mbrains.conf \
		/etc/sysctl.d/99-ha-forwarding.conf \
		/etc/modules-load.d/sit6.conf
	rm -rf /etc/systemd/system/mwan-ifmgr.service.d
	systemctl daemon-reload
}

main
