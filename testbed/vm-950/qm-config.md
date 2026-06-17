# VM 950 qm config (suburban testbed)

## Target args

VM 950 must run with the following Proxmox `args` field. The watchdog on
suburban talks to the in-VM `mwan-agent` over native vsock when this device is
present.

```text
args: -device vhost-vsock-pci,guest-cid=950
```

## Ownership

The Proxmox API gates the `args` field with a hard-coded
`$authuser eq 'root@pam'` string compare in `qemu-server`. No role and no ACL
can bypass it. Even an API token attached to `root@pam` itself fails because
the token authuser is `root@pam!<tokenname>`, not bare `root@pam`.

Tofu cannot set `args` via the bpg/proxmox provider when the suburban provider
alias authenticates with an API token. The
[opentofu/suburban/vms.tf](../../opentofu/suburban/vms.tf) resource for VM 950
omits `kvm_arguments` for that reason. The provider leaves undeclared fields
alone, so `tofu plan` does not flag drift on the live `args` string.

The Ansible playbook [ansible/playbooks/deploy-testbed.yml](../../ansible/playbooks/deploy-testbed.yml)
sets the field idempotently from the `Configure suburban testbed extras` play.
The task only runs `qm set` when the live `args` does not already match the
target string.

`args` only takes effect at QEMU process start, so an `args` change requires a
cold reboot of VM 950. The playbook prints a notice when it changes the value.
Run `qm stop 950` then `qm start 950` to pick up the new args.

## Verification

Inside the VM the kernel modules `vmw_vsock_virtio_transport` and `vsock` should
be loaded and `/dev/vsock` should be present.

```shell
ssh root@3d06:bad:b01:204::950 'lsmod | grep vsock; ls /dev/vsock'
```

On suburban, after restarting the watchdog, the journal should show
`ops transport succeeded` on `channel=vsock`.

```shell
ssh suburban 'systemctl restart mwan-watchdog-testbed; \
  sleep 10; \
  journalctl -u mwan-watchdog-testbed --since "20 seconds ago" --no-pager | \
    grep vsock'
```
