# Testbed MWAN VM qm config

## Target args

The testbed MWAN VM must run with a `vhost-vsock-pci` device whose guest CID equals
its VMID, so the suburban watchdog reaches the in-VM `mwan-agent` over native vsock.
Both values come from `mwan_vmid` in
[test_mwan_servers.yml](../../ansible/inventory/group_vars/test_mwan_servers.yml),
which matches production's MWAN VM id.

```text
args: -device vhost-vsock-pci,guest-cid=113
```

## Ownership

The Proxmox API gates the `args` field with a hard-coded `$authuser eq 'root@pam'`
string compare in `qemu-server`. No role and no ACL can bypass it. Even an API token
attached to `root@pam` itself fails, because the token authuser is
`root@pam!<tokenname>` rather than bare `root@pam`.

So OpenTofu cannot set `args` while the suburban provider alias authenticates with an
API token. The VM resource in
[opentofu/suburban/vms.tf](../../opentofu/suburban/vms.tf) omits `kvm_arguments` and
lists it in `ignore_changes`, which stops a plan from reading the live value as
removed and nulling it on apply.

[ansible/playbooks/deploy-testbed.yml](../../ansible/playbooks/deploy-testbed.yml)
sets the field idempotently from the `Configure suburban testbed extras` play, running
`qm set` only when the live `args` does not already match.

`args` takes effect only at QEMU process start, so a change needs a cold stop and
start rather than a reboot. The playbook prints a notice when it changes the value.

## Verification

Inside the VM the kernel modules `vmw_vsock_virtio_transport` and `vsock` should be
loaded and `/dev/vsock` should be present. Reach the VM at the management address
`test_mwan` owns in
[service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml).

```shell
ssh root@"$MWAN_VM" 'lsmod | grep vsock; ls /dev/vsock'
```

On suburban, after restarting the watchdog, the journal should show
`ops transport succeeded` on `channel=vsock`. That channel is CID-based, so it keeps
working when the management address changes but breaks whenever the CID and the VMID
disagree.

```shell
ssh suburban 'systemctl restart mwan-watchdog-testbed; \
  sleep 10; \
  journalctl -u mwan-watchdog-testbed --since "20 seconds ago" --no-pager | \
    grep vsock'
```
