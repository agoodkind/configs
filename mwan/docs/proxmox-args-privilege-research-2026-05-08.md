# Proxmox `args` Privilege Research

Date: 2026-05-08
Branch: `proxmox-args-privilege-research`
Trigger: Tofu `proxmox_virtual_environment_vm` apply for VM 102 returned HTTP 500 `only root can set 'args' config` while declaring `kvm_arguments = "-device virtio-serial-pci,..."`. Token in use was `root@pam!watchdog-test2` with `privsep=0`.

## TL;DR

The `args` field is gated by a hard-coded `$authuser eq 'root@pam'` string compare in qemu-server. No role and no ACL can bypass it. Even an API token attached to `root@pam` itself with every privilege propagated from `/` is rejected, because the token's authuser is `root@pam!<tokenname>`, not bare `root@pam`. This is documented behavior on the bpg/proxmox provider and confirmed by Proxmox staff on the forum. The recommended workaround is option 3: drop `kvm_arguments` from the Tofu resource and apply the QEMU args via a separate post-step that authenticates with `root@pam` username and password (or by editing the qemu-server `<vmid>.conf` directly through Ansible on the Proxmox node).

## 1. The exact PVE source check

### 1.1 The privilege gate

File: `src/PVE/API2/Qemu.pm` in `qemu-server.git`.
Function: `$check_vm_modify_config_perm` (closure, declared around line 1046).
Repo HEAD at investigation time: `13b423ea9aee8a6f04f92d3f29d347f5505ebabb` ("bump version to 9.1.10", 2 days before 2026-05-08).
Source URL: https://git.proxmox.com/?p=qemu-server.git;a=blob_plain;f=src/PVE/API2/Qemu.pm;hb=HEAD

Verbatim source of the relevant subroutine, fetched 2026-05-08:

```perl
my $check_vm_modify_config_perm = sub {
    my ($rpcenv, $authuser, $vmid, $pool, $key_list) = @_;

    return 1 if $authuser eq 'root@pam';

    foreach my $opt (@$key_list) {
        # some checks (e.g., disk, serial port, usb) need to be done somewhere
        # else, as there the permission can be value dependent
        next if PVE::QemuServer::is_valid_drivename($opt);
        next if $opt eq 'cdrom';
        next if $opt =~ m/^(?:unused|serial|usb|hostpci|virtiofs)\d+$/;
        next if $opt eq 'tags';

        if ($cpuoptions->{$opt} || $opt =~ m/^numa\d+$/) {
            $rpcenv->check_vm_perm($authuser, $vmid, $pool, ['VM.Config.CPU']);
        } elsif ($memoryoptions->{$opt}) {
            $rpcenv->check_vm_perm($authuser, $vmid, $pool, ['VM.Config.Memory']);
        } elsif ($hwtypeoptions->{$opt}) {
            $rpcenv->check_vm_perm($authuser, $vmid, $pool, ['VM.Config.HWType']);
        } elsif ($generaloptions->{$opt}) {
            $rpcenv->check_vm_perm($authuser, $vmid, $pool, ['VM.Config.Options']);
            # special case for startup since it changes host behaviour
            if ($opt eq 'startup') {
                $rpcenv->check_full($authuser, "/", ['Sys.Modify']);
            }
        } elsif ($vmpoweroptions->{$opt}) {
            $rpcenv->check_vm_perm($authuser, $vmid, $pool, ['VM.PowerMgmt']);
        } elsif ($diskoptions->{$opt}) {
            $rpcenv->check_vm_perm($authuser, $vmid, $pool, ['VM.Config.Disk']);
        } elsif ($opt =~ m/^net\d+$/ || $opt eq 'running-nets-host-mtu') {
            $rpcenv->check_vm_perm($authuser, $vmid, $pool, ['VM.Config.Network']);
        } elsif ($cloudinitoptions->{$opt} || $opt =~ m/^ipconfig\d+$/) {
            $rpcenv->check_vm_perm(
                $authuser, $vmid, $pool, ['VM.Config.Cloudinit', 'VM.Config.Network'], 1,
            );
        } elsif ($opt eq 'vmstate') {
            # the user needs Disk and PowerMgmt privileges to change the vmstate
            # also needs privileges on the storage, that will be checked later
            $rpcenv->check_vm_perm($authuser, $vmid, $pool, ['VM.Config.Disk', 'VM.PowerMgmt']);
        } else {
            # catches args, lock, etc.
            # new options will be checked here
            die "only root can set '$opt' config\n";
        }
    }

    return 1;
};
```

The first line of substance is `return 1 if $authuser eq 'root@pam';`. The keyword `args` is not in any of the branch tables (`$cpuoptions`, `$memoryoptions`, `$hwtypeoptions`, `$generaloptions`, `$vmpoweroptions`, `$diskoptions`, `$cloudinitoptions`), and it does not match any of the regex skips. Control therefore falls through to the final `else` branch, which raises `die "only root can set '$opt' config\n"`. The error message we saw is generated literally there.

### 1.2 Where this gate runs

The same function is called from two places in `src/PVE/API2/Qemu.pm`:

- Line ~2066 inside the `create_vm` POST handler: `&$check_vm_modify_config_perm($rpcenv, $authuser, $vmid, $pool, [keys %$param]);`
- Line ~2259 inside `update_vm_api` (used by `update_vm_async` POST): same call against both deletion list and the param keys.

Source: same file, fetched 2026-05-08, https://git.proxmox.com/?p=qemu-server.git;a=blob_plain;f=src/PVE/API2/Qemu.pm;hb=HEAD

So both VM CREATE and VM UPDATE go through the gate. There is no path that exposes the underlying qemu-server config writer to the API while bypassing `$check_vm_modify_config_perm`.

### 1.3 The schema definition for `args`

File: `src/PVE/QemuServer.pm`, lines 583-591 (approximate, fetched 2026-05-08):

```perl
args => {
    optional => 1,
    type => 'string',
    description => "Arbitrary arguments passed to kvm.",
    verbose_description => <<EODESCR,
Arbitrary arguments passed to kvm, for example:

args: -no-reboot -smbios 'type=0,vendor=FOO'

NOTE: this option is for experts only.
EODESCR
```

Source: https://git.proxmox.com/?p=qemu-server.git;a=blob_plain;f=src/PVE/QemuServer.pm;hb=HEAD

The schema does not declare a privilege. The privilege gate is enforced upstream of the schema, in the API layer.

## 2. Verdict

No. There is no role or ACL combination that lets a non-`root@pam` authuser set `args`. The check is a literal string compare, not a privilege lookup, and an API token's authuser is `<user>!<tokenname>`, never bare `<user>`.

Empirical confirmation on `suburban` (2026-05-08, read-only):

- `pveum user token list root@pam` shows `watchdog-test` and `watchdog-test2`, both with `privsep=0`.
- `pveum user permissions 'root@pam!watchdog-test2' --path /vms/102` returns the full superset including `Sys.Modify (*)`, `VM.Config.Options (*)`, `Permissions.Modify (*)`, every `VM.Config.*`, every `VM.GuestAgent.*`, and `VM.Allocate (*)`. All propagated from `/`.
- Despite this, the same token still hit `only root can set 'args' config` on the original Tofu apply for VM 102.

The combination of `privsep=0` plus full Administrator privilege at `/` is the broadest a token can be. The fact that the call still fails matches what the source says: the gate does not consult privileges at all for this option.

## 3. Confirmation: only path is direct `root@pam` username and password

A Proxmox staff member, Fabian, stated this explicitly on the forum on 2024-03-28:

> "in some places we explicitly check for 'root@pam' and that does not include root@pam's tokens (since the tokens could have less privileges"

Source: https://forum.proxmox.com/threads/root-pam-token-api-restricted.83866/

A second staff member, Dominik, said on 2022-05-03:

> "there is currently some effort done into removing hardcoded 'root only' checks and replacing them with a 'superuser' privilege"

Source: https://forum.proxmox.com/threads/fedora-core-os-ignition-root-pam-api-tokens-restricted-from-using-qemu-args.108886/

Two implications. First, the workaround that does work is to authenticate as `root@pam` with username and password (the API session ticket flow), since the resulting authuser string is exactly `root@pam`. Second, this is acknowledged technical debt in Proxmox, with no shipped replacement as of the HEAD I inspected, so we should not expect this to change without us monitoring Proxmox releases.

## 4. How `kvm_arguments` relates to bpg/proxmox provider docs

The `kvm_arguments` field in `proxmox_virtual_environment_vm` maps directly to the qemu-server `args` config option. When the provider serializes the resource, it sends `args=<value>` to the same `POST /api2/json/nodes/{node}/qemu` (CREATE) or `POST /api2/json/nodes/{node}/qemu/{vmid}/config` (UPDATE) endpoint, where the gate above runs.

The bpg provider docs do flag that not all ops work via `api_token`. The provider's top-level index page says (fetched 2026-05-08):

> "Not all Proxmox API operations are supported via API Token. You may see errors like `error creating container: received an HTTP 403 response - Reason: Permission check failed (changing feature flags for privileged container is only allowed for root@pam)`"

And gives the matching example for our case:

> "`error creating VM: received an HTTP 500 response - Reason: only root can set 'arch' config`"

Source: https://github.com/bpg/terraform-provider-proxmox/blob/main/docs/index.md

The exact same error class as the one we hit, just with `arch` instead of `args` (both fall into the same `else` branch in `$check_vm_modify_config_perm`).

The `virtual_environment_vm` resource page does not put a warning on the `kvm_arguments` description itself. The only inline warning of this kind in that resource page is on `hostpci.id`:

> "This parameter is not compatible with `api_token` and requires the root `username` and `password`"

Source: https://github.com/bpg/terraform-provider-proxmox/blob/main/docs/resources/virtual_environment_vm.md

So the provider documents the general limitation at the top level and warns inline only on `hostpci.id`. `kvm_arguments` does not get the inline warning, even though the underlying API behavior is the same. That is a documentation gap on the provider side that we may want to file a PR for.

## 5. Recommendation

I recommend option 3 (drop `kvm_arguments` from Tofu and apply args out of band).

Rationale:

1. Option 2 (manual create plus `terraform import`) leaves the `kvm_arguments` attribute inside the Tofu state, which means any later plan that touches the VM will diff `kvm_arguments` and try to re-apply it, hitting the same 500 every time. We would need to permanently `lifecycle { ignore_changes = [kvm_arguments] }`, which is roughly equivalent in expressiveness to option 3 but adds a hidden trap for the next operator.
2. Option 3 is honest about what is actually happening. The Tofu resource describes only what the API token can express. The QEMU args live in a clearly separate Ansible task that runs as `root@pam` on the Proxmox node and edits `/etc/pve/qemu-server/<vmid>.conf` directly (or hits the API with username/password from a vault entry). The boundary between "what Tofu manages" and "what needs root" stays sharp.
3. The Ansible-on-the-Proxmox-node path also matches how we already apply other root-only Proxmox node config in this repo, which keeps the pattern consistent with existing playbooks.

Concrete shape of the recommended change for VM 102:

- In the Tofu resource, do not set `kvm_arguments`.
- After `tofu apply`, run an Ansible task on the Proxmox node that ensures the line `args: -device vhost-vsock-pci,...` is present in `/etc/pve/qemu-server/102.conf` and triggers a stop/start (or a live `qm set 102 --args ...` invoked locally on the node, which runs as the local root user and so passes the gate).
- Track the args string in the Ansible role, not in Tofu, so there is one source of truth.

If we ever do want this in Tofu directly, the only path is to give the provider a separate `root@pam` username and password (not a token) and use that authentication path explicitly. We should not do this, because it means storing the Proxmox root password in Tofu's runtime credentials, which is a worse posture than the Ansible-on-node path.

## 6. Surprises worth flagging

The token already has `Permissions.Modify (*)` propagated from `/`. That is broader than I expected for `watchdog-test2`. It does not change the verdict on `args`, since that gate ignores privileges entirely, but it is worth a separate review of whether the `watchdog-*` tokens really need `Permissions.Modify` and the full Administrator superset.

## Sources

- Proxmox qemu-server source, `src/PVE/API2/Qemu.pm`, repo HEAD `13b423ea9aee8a6f04f92d3f29d347f5505ebabb`: https://git.proxmox.com/?p=qemu-server.git;a=blob_plain;f=src/PVE/API2/Qemu.pm;hb=HEAD
- Proxmox qemu-server source, `src/PVE/QemuServer.pm`: https://git.proxmox.com/?p=qemu-server.git;a=blob_plain;f=src/PVE/QemuServer.pm;hb=HEAD
- Proxmox forum, "root@pam Token API restricted?", staff post by Fabian 2024-03-28: https://forum.proxmox.com/threads/root-pam-token-api-restricted.83866/
- Proxmox forum, "Fedora Core OS ignition - root@pam API tokens restricted from using qemu args", staff post by Dominik 2022-05-03: https://forum.proxmox.com/threads/fedora-core-os-ignition-root-pam-api-tokens-restricted-from-using-qemu-args.108886/
- bpg/terraform-provider-proxmox provider top-level docs (API token limitations): https://github.com/bpg/terraform-provider-proxmox/blob/main/docs/index.md
- bpg/terraform-provider-proxmox `virtual_environment_vm` resource docs: https://github.com/bpg/terraform-provider-proxmox/blob/main/docs/resources/virtual_environment_vm.md
- Proxmox `qm.1` man page (`args` schema description, no permission note): https://pve.proxmox.com/pve-docs/qm.1.html
- Live read-only inspection on `suburban`, 2026-05-08, via `pveum user token list root@pam`, `pveum acl list`, `pveum user permissions 'root@pam!watchdog-test2' --path /vms/102`.
