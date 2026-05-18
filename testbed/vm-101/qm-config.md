# VM 101 qm config (suburban testbed)

## Target args

The suburban OPNsense testbed VM `101` runs with the following Proxmox
`args` field. The named `io.goodkind.mwan-opnsense.0` virtio console maps
to `/dev/ttyV0.1` inside the OPNsense guest, and the `mwan_opnsense` rc.d
service writes that path into `/var/lib/mwan/daemon.toml` before the
daemon connects to the host-side bridge over this virtio-serial chardev.

```text
args: -device virtio-serial-pci,id=mwanrpc -chardev socket,id=mwanchr,path=/var/run/qemu-server/101.mwanrpc,server=on,wait=off -device virtserialport,chardev=mwanchr,name=io.goodkind.mwan-opnsense.0
```

## Why Tofu does not manage this field

The Proxmox API gates the `args` field with a hard-coded
`$authuser eq 'root@pam'` string compare in `qemu-server`. No role and
no ACL can bypass it. Even an API token attached to `root@pam` itself
fails because the token's authuser is `root@pam!<tokenname>`, not bare
`root@pam`.

The bpg/proxmox provider therefore omits `kvm_arguments` from
`opentofu/vms.tf` and ownership lives in Ansible. The provider leaves
undeclared fields alone, so `tofu plan` does not flag drift on the live
`args` string.

## How Ansible owns this field

The Ansible playbook `ansible/playbooks/deploy-testbed.yml` carries
an idempotent `qm set` task in the `Configure suburban testbed extras` play.
The task only runs `qm set` when the live `args` does not already match
the target string. Look for the task tagged `args` named
`Set mwanrpc chardev on VM 101 args`.

`args` only takes effect at QEMU process start, so an `args` change
requires a cold reboot of VM 101. The playbook prints a notice when it
changes the value. Run `qm stop 101` then `qm start 101` to pick up the
new args.

## Verification

Inside the OPNsense guest, after `service mwan_opnsense start`, confirm
that the named virtio console resolves to `/dev/ttyV0.1` and that the
rc.d wrapper wrote the daemon contract file:

```bash
ssh root@<vm-101-mgmt-ip> 'service mwan_opnsense start'
ssh root@<vm-101-mgmt-ip> 'service mwan_opnsense status'
ssh root@<vm-101-mgmt-ip> 'ls -l /dev/vtcon/io.goodkind.mwan-opnsense.0 /dev/ttyV0.1'
ssh root@<vm-101-mgmt-ip> 'ls -l /var/lib/mwan/daemon.toml'
ssh root@<vm-101-mgmt-ip> 'sed -n "1,20p" /var/lib/mwan/daemon.toml'
```

Expect `/dev/vtcon/io.goodkind.mwan-opnsense.0` to point at `../ttyV0.1`.
Expect `/var/lib/mwan/daemon.toml` to be owned by `root` with mode
`-rw-------`, and expect the `[daemon]` table to include `serial_path =
"/dev/ttyV0.1"`, `baud`, `config_xml_path`, `backup_dir`, `logfile`, and
`state_dir`. If the named symlink resolves somewhere else, treat that
symlink target as the live truth and update `mwan_opnsense_listen_serial`
to match before re-testing.

On suburban, the host-side socket exists while VM 101 is running.

```bash
ssh suburban 'ls -l /var/run/qemu-server/101.mwanrpc'
```

The host-side mwan-opnsense bridge daemon reads
`/etc/mwan/config.toml` `[opnsense.host].upstream` to find this socket.
The deploy task in `ansible/playbooks/tasks/mwan-opnsense-host-deploy.yml`
sets `mwan_opnsense_host_vmid=101` so the rendered upstream is
`unix:///var/run/qemu-server/101.mwanrpc`.
