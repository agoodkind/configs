# Testbed MWAN VM qm config

## Target args

The testbed MWAN VM must run with a `vhost-vsock-pci` device whose guest CID equals
its VMID. The suburban watchdog uses that CID to contact `mwan-agent` over vsock.
The `mwan_suburban` service mapping in
[configuration/main.tf](../../opentofu/modules/suburban/mwan/configuration/main.tf)
sets VMID 213 and its management IPv6 address.
The id deliberately differs from production's MWAN VM, so renumbering one guest
does not silently point the other's tooling at it.

A renumber therefore has to change the CID too. The example below shows the
current value rather than defining it.

```text
args: -device vhost-vsock-pci,guest-cid=213
```

## Ownership

The Proxmox API gates the `args` field with a hard-coded `$authuser eq 'root@pam'`
string compare in `qemu-server`. No role and no ACL can bypass it. Even an API token
attached to `root@pam` itself fails, because the token authuser is
`root@pam!<tokenname>` rather than bare `root@pam`.

So OpenTofu cannot set `args` while the suburban provider alias authenticates with an
API token. The VM resource in
[main.tf](../../opentofu/modules/suburban/mwan/main.tf) omits `kvm_arguments` and
lists it in `ignore_changes`, which stops a plan from reading the live value as
removed and nulling it on apply.

`args` takes effect only at QEMU process start, so a change needs a cold stop and
start rather than a reboot.

## Verification

Inside the VM the kernel modules `vmw_vsock_virtio_transport` and `vsock` should be
loaded and `/dev/vsock` should be present. The `mwan_suburban` service mapping
sets `mwan.suburban.goodkind.io` as the management hostname.

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
