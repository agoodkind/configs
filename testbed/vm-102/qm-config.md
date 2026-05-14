# VM 102 qm config (suburban testbed, MWAN-149)

## Target args

VM 102 must run with the following Proxmox `args` field. In the current
testbed docs, the named `io.goodkind.mwan-opnsense.0` virtio console maps
to `/dev/ttyV0.1` inside the OPNsense guest, and the `mwan_opnsense` rc.d
service writes that path into `/var/lib/mwan/daemon.toml` before the
daemon connects to the host-side bridge over this virtio-serial chardev.

```text
args: -device virtio-serial-pci,id=mwanrpc -chardev socket,id=mwanchr,path=/var/run/qemu-server/102.mwanrpc,server=on,wait=off -device virtserialport,chardev=mwanchr,name=io.goodkind.mwan-opnsense.0
```

The chardev path `/var/run/qemu-server/102.mwanrpc` does not collide
with VM 101's chardev at `/var/run/qemu-server/101.mwanrpc`. The
chardev name `io.goodkind.mwan-opnsense.0` matches the guest-side named
virtio console that the rc.d service should render as
`serial_path = "/dev/ttyV0.1"` in `/var/lib/mwan/daemon.toml`.

## Why Tofu does NOT manage this field (MWAN-154)

The Proxmox API gates the `args` field with a hard-coded
`$authuser eq 'root@pam'` string compare in `qemu-server`. No role and
no ACL can bypass it. Even an API token attached to `root@pam` itself
fails because the token's authuser is `root@pam!<tokenname>`, not bare
`root@pam`. See `mwan/docs/proxmox-args-privilege-research-2026-05-08.md`
for the source-level walk-through.

The original MWAN-149 attempt to declare `kvm_arguments` on the VM 102
resource hit the gate during `tofu apply` and returned HTTP 500
`only root can set 'args' config`. The MWAN-154 cleanup removes the
field from `opentofu/vms.tf` and shifts ownership to Ansible. The
bpg/proxmox provider leaves undeclared fields alone, so `tofu plan`
does not flag drift on the live `args` string.

## How Ansible owns this field

The Ansible playbook `ansible/playbooks/deploy-mwan-testbed.yml` carries
an idempotent `qm set` task in the `Configure suburban hypervisor` play.
The task only runs `qm set` when the live `args` does not already match
the target string. Look for the task tagged `args` named
`Set mwanrpc chardev on VM 102 args (MWAN-149)`.

`args` only takes effect at QEMU process start, so an `args` change
requires a cold reboot of VM 102. The playbook prints a notice when it
changes the value. Run `qm stop 102` then `qm start 102` to pick up the
new args.

## Verification

Inside the OPNsense guest, after `service mwan_opnsense start`, confirm
that the named virtio console resolves to `/dev/ttyV0.1` and that the
rc.d wrapper wrote the daemon contract file:

```bash
ssh root@<vm-102-mgmt-ip> 'service mwan_opnsense start'
ssh root@<vm-102-mgmt-ip> 'service mwan_opnsense status'
ssh root@<vm-102-mgmt-ip> 'ls -l /dev/vtcon/io.goodkind.mwan-opnsense.0 /dev/ttyV0.1'
ssh root@<vm-102-mgmt-ip> 'ls -l /var/lib/mwan/daemon.toml'
ssh root@<vm-102-mgmt-ip> 'sed -n "1,20p" /var/lib/mwan/daemon.toml'
```

Expect `/dev/vtcon/io.goodkind.mwan-opnsense.0` to point at `../ttyV0.1`.
Expect `/var/lib/mwan/daemon.toml` to be owned by `root` with mode
`-rw-------`, and expect the `[daemon]` table to include `serial_path =
"/dev/ttyV0.1"`, `baud`, `config_xml_path`, `backup_dir`, `logfile`, and
`state_dir`. If the named symlink resolves somewhere else, treat that
symlink target as the live truth and update `mwan_opnsense_listen_serial`
to match before re-testing.

On suburban, the host-side socket should exist while VM 102 is running.

```bash
ssh suburban 'ls -l /var/run/qemu-server/102.mwanrpc'
```

The host-side mwan-opnsense bridge daemon connects to that socket via
the systemd drop-in at
`/etc/systemd/system/mwan-opnsense-host.service.d/upstream.conf` once
the deploy task in `ansible/playbooks/tasks/mwan-opnsense-host-deploy.yml`
runs against suburban with `mwan_opnsense_host_vmid=102`.
