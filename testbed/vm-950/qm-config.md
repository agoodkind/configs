# VM 950 qm config (suburban testbed)

## Target args

VM 950 must run with the following Proxmox `args` field. The watchdog on
suburban talks to the in-VM `mwan-agent` over native vsock instead of
falling back to TCP when this device is present.

```text
args: -device vhost-vsock-pci,guest-cid=950
```

This mirrors the posture of prod VM 113 (set under MWAN-87), whose args
read `-device vhost-vsock-pci,guest-cid=113`.

## Why Tofu does NOT manage this field (MWAN-154)

The Proxmox API gates the `args` field with a hard-coded
`$authuser eq 'root@pam'` string compare in `qemu-server`. No role and
no ACL can bypass it. Even an API token attached to `root@pam` itself
fails because the token's authuser is `root@pam!<tokenname>`, not bare
`root@pam`. See [docs/proxmox-args-privilege-research-2026-05-08.md](../../docs/proxmox-args-privilege-research-2026-05-08.md)
for the source-level walk-through.

Tofu therefore cannot set `args` via the bpg/proxmox provider when the
suburban provider alias authenticates with an API token. The
[opentofu/suburban/vms.tf](../../opentofu/suburban/vms.tf) resource for VM 950 omits
`kvm_arguments` for that reason. The bpg/proxmox provider leaves undeclared
fields alone, so `tofu plan` does not flag drift on the live `args` string.

## How Ansible owns this field

The Ansible playbook [ansible/playbooks/deploy-testbed.yml](../../ansible/playbooks/deploy-testbed.yml)
carries an idempotent `qm set` task in the `Configure suburban testbed extras`
play.
The task only runs `qm set` when the live `args` does not already match
the target string. Look for the task tagged `args` named
`Set vsock device on VM 950 args (MWAN-143)`.

`args` only takes effect at QEMU process start, so an `args` change
requires a cold reboot of VM 950. The playbook prints a notice when it
changes the value. Run `qm stop 950` then `qm start 950` to pick up the
new args.

## Verification

Inside the VM the kernel modules `vmw_vsock_virtio_transport` and
`vsock` should be loaded and `/dev/vsock` should be present.

```shell
ssh -J suburban root@3d06:bad:b01:200::950 'lsmod | grep vsock; ls /dev/vsock'
```

On suburban, after restarting the watchdog, the journal should show
`ops transport succeeded` on `channel=vsock` rather than
`vsock unavailable, used TCP fallback`.

```shell
ssh suburban 'systemctl restart mwan-watchdog-testbed; \
  sleep 10; \
  journalctl -u mwan-watchdog-testbed --since "20 seconds ago" --no-pager | \
    grep -E "vsock|tcp fallback"'
```
