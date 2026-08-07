# OPNsense testbed guest: qm config

## Target args

The suburban OPNsense testbed guest runs with the Proxmox `args` field below.
Every VMID in it comes from `mwan_opnsense_vmid`, so the example shows the
current shape rather than defining it. The named `io.goodkind.mwan-opnsense.0`
virtio console maps
to `/dev/ttyV0.1` inside the OPNsense guest, and the `mwan_opnsense` rc.d
service writes that path into `/var/lib/mwan/daemon.toml` before the
daemon connects to the host-side bridge over this virtio-serial chardev.

```text
args: -device virtio-serial-pci,id=mwanrpc -chardev socket,id=mwanchr,path=/var/run/qemu-server/201.mwanrpc,server=on,wait=off -device virtserialport,chardev=mwanchr,name=io.goodkind.mwan-opnsense.0
```

## Why Tofu does not manage this field

The Proxmox API gates the `args` field with a hard-coded
`$authuser eq 'root@pam'` string compare in `qemu-server`. No role and
no ACL can bypass it. Even an API token attached to `root@pam` itself
fails because the token's authuser is `root@pam!<tokenname>`, not bare
`root@pam`.

The bpg/proxmox provider therefore omits `kvm_arguments` from
[opentofu/suburban/vms.tf](../../opentofu/suburban/vms.tf) and ownership lives in Ansible. The
provider leaves undeclared fields alone, so `tofu plan` does not flag drift on
the live `args` string.

## How Ansible owns this field

The Ansible playbook [ansible/playbooks/deploy-testbed.yml](../../ansible/playbooks/deploy-testbed.yml)
carries an idempotent `qm set` task in the `Configure suburban testbed extras`
play.
The task only runs `qm set` when the live `args` does not already match
the target string. Look for the task tagged `args` named
`Set mwanrpc chardev on OPNsense VM args`.

`args` only takes effect at QEMU process start, so an `args` change requires a
cold stop and start rather than a reboot. The playbook prints a notice naming
the guest when it changes the value.

## Verification

Inside the OPNsense guest, after `service mwan_opnsense start`, confirm
that the named virtio console resolves to `/dev/ttyV0.1` and that the
rc.d wrapper wrote the daemon contract file:

```bash
OPNSENSE_SUBURBAN=10.240.240.2

ssh -J 'root@[3d06:bad:b01:200::1]' "agoodkind@$OPNSENSE_SUBURBAN" \
    'sudo service mwan_opnsense start'
ssh -J 'root@[3d06:bad:b01:200::1]' "agoodkind@$OPNSENSE_SUBURBAN" \
    'sudo service mwan_opnsense status'
ssh -J 'root@[3d06:bad:b01:200::1]' "agoodkind@$OPNSENSE_SUBURBAN" \
    'sudo ls -l /dev/vtcon/io.goodkind.mwan-opnsense.0 /dev/ttyV0.1'
ssh -J 'root@[3d06:bad:b01:200::1]' "agoodkind@$OPNSENSE_SUBURBAN" \
    'sudo ls -l /var/lib/mwan/daemon.toml'
ssh -J 'root@[3d06:bad:b01:200::1]' "agoodkind@$OPNSENSE_SUBURBAN" \
    'sudo sed -n "1,20p" /var/lib/mwan/daemon.toml'
```

`OPNSENSE_SUBURBAN` is the current
`service_mapping.opnsense_suburban.ansible_host` value.

Expect `/dev/vtcon/io.goodkind.mwan-opnsense.0` to point at `../ttyV0.1`.
Expect `/var/lib/mwan/daemon.toml` to be owned by `root` with mode
`-rw-------`, and expect the `[daemon]` table to include `serial_path =
"/dev/ttyV0.1"`, `baud`, `config_xml_path`, `backup_dir`, `logfile`, and
`state_dir`. If the named symlink resolves somewhere else, treat that
symlink target as the live truth and update `mwan_opnsense_listen_serial`
to match before re-testing.

On suburban, the host-side socket exists while the guest is running. Read its
path from the rendered config rather than typing a VMID:

```bash
ssh suburban 'grep chardev /etc/mwan/config.toml'
```

The host-side mwan-opnsense bridge daemon reads
`/etc/mwan/config.toml` `[opnsense.host].upstream` to find this socket.
The deploy task in
[ansible/playbooks/tasks/mwan-opnsense-host-deploy.yml](../../ansible/playbooks/tasks/mwan-opnsense-host-deploy.yml)
reads `mwan_opnsense_vmid` from group_vars, so the rendered upstream
tracks the guest's id rather than a literal in this page.
