# OPNsense Testbed Baseline VM 102

Created: 2026-05-08
Tracking issue: MWAN-149

This runbook is the operator workflow for bringing the replacement OPNsense
testbed VM (`opnsense-test2`, VMID `102`) on suburban from a freshly applied
OpenTofu shell to a baseline ready for the MWAN-127 config import rehearsal
and the MWAN-13 26.x upgrade validation.

The wedged VM `101` (`opnsense-test`) stays in place as a forensic artifact
of the MWAN-119 v2 rollback. Do not destroy or apply against it.

## Prerequisites

- The MWAN-149 branch is merged and `tofu apply` has been run against the
  suburban Proxmox provider, so the `proxmox_virtual_environment_vm.opnsense_test2`
  resource exists and the VM 102 shell is provisioned. The shell has:
  - One NIC on `vmbrtrunk` (single-port posture per MWAN-148).
  - An 8G boot disk on `local-zfs` (empty).
  - `serial0: socket` and `vga: serial0` for serial-console install.
  - The mwan-opnsense virtio-serial chardev exposed at
    `/var/run/qemu-server/102.mwanrpc` with chardev name
    `io.goodkind.mwan-opnsense.0`.
  - `started = false`, so the operator boots it explicitly when ready.
- An OPNsense serial installer image is staged on suburban at
  `/var/lib/vz/template/iso/OPNsense-25.7-serial-amd64.img` (or the latest
  serial image variant for the rehearsal).
- The operator has SSH access to suburban as `root@10.240.0.148`.

## Step 1: Install OPNsense via serial console

Follow `mwan/docs/runbooks/opnsense-serial-vm-from-scratch.md` end to end,
substituting the rehearsal VMID with `102` and using the
`opnsense-test2` name. Notable substitutions:

- `VMID=102`
- `NAME=opnsense-test2`
- `LAN_IPV4` should be a free address on the testbed management plane;
  the existing rehearsal used `10.240.200.130`, so pick a different
  unused address like `10.240.200.102` to keep VM ID and IP aligned.

Stop after the runbook reaches the basic operational state with SSH and
QEMU Guest Agent reachable. Do not import the prod config.xml yet; that
happens in MWAN-127 after the baseline is captured.

## Step 2: Install the mwan-opnsense daemon

The mwan-opnsense daemon runs inside the OPNsense guest and exposes the
gRPC channel that the in-host watchdog talks to via the virtio-serial
chardev. The rollout recipe lives in the top-level `AGENTS.MD` under the
`Manual rollout of a new mwan binary` heading. Build the freebsd/amd64
binary locally with `make`, then scp it to `opnsense-test2` and install
the rc.d unit per the recipe.

Confirm the daemon is up with the local check inside the guest:

```bash
service mwan_opnsense status
ls -l /dev/ttyV0.0
```

The chardev device should be `/dev/ttyV0.0` and the service should show
running.

## Step 3: Confirm the gRPC channel from the host

From suburban, point the host-side `mwan opnsense-host` upstream at the
new chardev path and confirm round-trip RPC works:

```bash
ssh root@10.240.0.148 \
  'mwan opnsense-probe -upstream unix:///var/run/qemu-server/102.mwanrpc'
```

Expected: a non-error probe response. If the probe fails, check that the
chardev path matches the `kvm_arguments` block in `opentofu/vms.tf` and
that the `mwan_opnsense` service inside the guest has registered the
serial device.

If you intend to run `mwan-opnsense-host.service` against VM 102 instead
of VM 101, update the `upstream.conf` drop-in for the unit so it points
at the VM 102 chardev path. The existing drop-in points at VM 101 and
should not be changed without a follow-up commit, since the wedged VM
101 still surfaces forensic state.

## Step 4: Capture the baseline

Once the gRPC channel is confirmed reachable, the VM 102 baseline is
ready for downstream slices:

- MWAN-150 (config.xml transform import): the transform layer rewrites
  prod-side `iavf0` references to the testbed's matching device name and
  imports the prod config.xml into the new baseline.
- MWAN-151 (26.x changelog): use this baseline as the starting point for
  the OPNsense 26.x upgrade dry-run; record any deltas in the changelog.
- MWAN-127 (config.xml import rehearsal): the actual rehearsal runs
  against this VM with the transform applied.

Snapshot the baseline before any of those slices touch it so the operator
has a clean rollback target:

```bash
ssh root@10.240.0.148 'qm snapshot 102 baseline-clean --description "Fresh OPNsense install + mwan-opnsense daemon, no prod config imported"'
```

## Notes

- The `prevent_destroy = true` lifecycle on the VM 102 resource keeps
  `tofu destroy` from removing the shell. Snapshot rollback on the
  Proxmox side is the supported reset path during rehearsal.
- VM 101 stays untouched throughout this workflow. Any operation that
  references VM 101 belongs in a separate, explicit slice.

## Install run 2026-05-08 (automated, Path B)

The Step 1 install was driven non-interactively from a developer host
using `expect(1)` against the suburban serial socket
`/var/run/qemu-server/102.serial0`. Path B from the runbook prompt: the
serial installer image at
`/var/lib/vz/template/iso/OPNsense-25.7-serial-amd64.img` was attached
as the live system, the existing 8 GiB target disk was grown to 16 GiB
in place, and the installer wrote ZFS stripe onto `da1`.

Tooling notes:

- `expect` was used instead of pexpect because it ships with Proxmox
  and the suburban host already has `/usr/bin/expect`. No `pip install`
  step was needed.
- The serial socket type is a Unix stream socket (not a PTY), so
  `socat - UNIX-CONNECT:/var/run/qemu-server/102.serial0` is the
  correct adapter rather than `qm terminal`.
- Step scripts ran from a local Mac and were `scp`'d to suburban
  `/tmp/`. Final transcript:
  `mwan/docs/runbooks/install-transcript-vm102-2026-05-08.log`
  (passwords redacted).

Disk-size correction:

- Tofu shipped VM 102 with an 8 GiB boot disk. The Path B installer
  fails on 8 GiB with `gpart: autofill: No space left on device`, as
  documented in `opnsense-serial-vm-from-scratch.md`. The disk was
  grown in place with `qm resize 102 scsi0 16G` before the install
  began. Tofu state may want to be reconciled separately so the
  declared size matches the on-disk size.
- During the install the original 16 GiB disk was temporarily moved
  to `scsi1` while the live installer image occupied `scsi0`. Once
  the install finished and the VM halted, scsi0 was reset to the
  installed disk and the installer image was left as `unused1` for
  audit. A follow-up commit can `qm set 102 --delete unused1` to
  reclaim the space.

Pre-install snapshot:

```bash
qm snapshot 102 pre-install-2026-05-08 --vmstate 0
```

Known installer hiccup, confirmed:

- After `Space` on `da1`, press `Enter` directly. Do not `Tab`. The
  rehearsal correction in `opnsense-serial-vm-from-scratch.md` was
  followed and the install proceeded normally.

Result on first boot from disk:

- ZFS root mounted: `Root file system: zroot/ROOT/default`.
- Console banner: `OPNsense 25.7 (amd64)`, hostname
  `OPNsense.internal`, LAN `vtnet0 -> v4: 192.168.1.1/24` (the
  installer default; not adjusted in this slice per the prompt's
  "do not change network beyond `vmbrtrunk`" rule).
- Root login with the install-default password works.
- `sysrc openssh_enable=YES` and `sysrc openssh_skipportscheck=YES`
  were set, then `service openssh start` brought up `sshd` listening
  on `*:22` for both tcp4 and tcp6, confirmed via `sockstat` and
  `service openssh status`.

What this slice did not do (deferred):

- Did not push the generated `mwan/testbed/opnsense/generated/config-testbed.xml`.
- Did not install the `mwan-opnsense` daemon, the rc.d unit, or the
  loader.conf drop-in.
- Did not change the LAN IPv4 from the `192.168.1.1/24` default.
- Did not enable QEMU Guest Agent inside the guest.
- Did not delete `unused1` (the installer image disk).

## Addendum: Option A topology (2026-05-08)

The MWAN-149 cycle picked Option A for VM 102's MANAGEMENT plane. The
LAN side of OPNsense (`vtnet0`) lives on `vmbrtrunk` untagged so the
single-port posture from MWAN-148 is preserved, and suburban itself
joins the same untagged side as a stub L3 client so it can reach
OPNsense over SSH and the in-guest mwan-opnsense gRPC channel from a
single host. This mirrors how prod vault joins the OPNsense LAN bridge
to act as the host-side watchdog peer.

Address allocation (mirrors the MWAN-150 substitutions; testbed prefix
`2N` mirrors the prod prefix `N`):

| Endpoint                  | IPv4              | IPv6                       |
| ------------------------- | ----------------- | -------------------------- |
| VM 102 OPNsense MANAGEMENT (`vtnet0`, untagged on `vmbrtrunk`) | `10.240.4.1/24`  | `3d06:bad:b01:204::1/64`  |
| Suburban stub on `vmbrtrunk`                                    | `10.240.4.5/24`  | `3d06:bad:b01:204::5/64`  |

Persistent state lives in three coordinated places:

1. `opentofu/networks.tf` declares the suburban stub addresses on the
   `proxmox_network_linux_bridge.trunk` resource via the bpg/proxmox
   provider's `address` and `address6` attributes.
2. Suburban's `/etc/network/interfaces` file carries the matching
   `iface vmbrtrunk inet static` and `iface vmbrtrunk inet6 static`
   blocks. A pre-change backup is preserved at
   `/etc/network/interfaces.before-vmbrtrunk-stub-2026-05-08`.
3. OPNsense's `/conf/config.xml` has the LAN block rewritten from
   the installer default `192.168.1.1/track6` to the static testbed
   addresses. A pre-change backup is preserved at
   `/conf/config.xml.before-vm102-lan-2026-05-08`.

The OPNsense LAN move was driven over the QEMU serial socket
(`/var/run/qemu-server/102.serial0`) using `expect`-driven `socat`,
since the installer-default OPNsense did not have SSH reachable yet.
The same console session also flipped the SSH service to `enabled=1`
and `permitrootlogin=1`, then `passwordauth=1`, and ran
`/usr/local/etc/rc.sshd` to regenerate `/usr/local/etc/ssh/sshd_config`
so password auth actually took effect in the rendered file. Once SSH
worked, the suburban operator pubkey was installed in
`/root/.ssh/authorized_keys` so the ongoing daemon-install step did
not require `sshpass`.

The mwan-opnsense daemon listens on the named virtio-console port
`io.goodkind.mwan-opnsense.0`, which the FreeBSD `virtio_console`
driver exposes as `/dev/ttyV0.1` via the symlink in `/dev/vtcon/`.
The matching `mwan_opnsense_listen_serial` `sysrc` value is
`/dev/ttyV0.1`, which is also the rc.d default. Cross-check the named
symlink before assuming the device path:

```sh
ls -l /dev/vtcon/io.goodkind.mwan-opnsense.0
```

If the symlink points at `/dev/ttyV1.1` instead, the host-side QEMU
arguments may have re-ordered the `virtio-serial-pci` slots; update
`mwan_opnsense_listen_serial` accordingly.

Verified gRPC version probe from suburban after daemon install:

```sh
mwan opnsense-probe -target unix:///var/run/qemu-server/102.mwanrpc -op version
```

The probe returns the daemon's build banner
(`commit=<sha> dirty=clean binhash=<hash>`).
