# Proxmox VE permission overlays

This directory adds narrow privileges to Proxmox VE. A non-root API token can then do one specific operation on one guest or one ACME account.

## What the overlay adds

Stock Proxmox VE restricts these operations to `root@pam`. The overlay adds a privilege for each one:

- `VM.Config.Nesting` changes the `nesting` flag of a container.
- `VM.Config.Keyctl` changes the `keyctl` flag of a container.
- `VM.Config.Vsock` enables a virtio vsock device on a VM. The guest CID equals the VM ID.
- `Sys.ACME.Account.Audit`, `Create`, `Modify`, and `Remove` read, register, update, and remove one named ACME account.

The [permission reference](permissions.md) lists the ACL path and the API method for each privilege.

## How it works

The `patches` directory has one patch for each of four Proxmox components: `pve-access-control`, `pve-container`, `qemu-server`, and `pve-manager`. Each patch is the signed commit on the `overlays` branch of the `agoodkind` fork of that component. The patch header records the fork commit, and the last lines record the upstream base commit. Together the patches change seven Perl modules.

`pve-overlay` reads each packaged module from `/usr/share/perl5`, applies the patch, and writes the result to `/etc/perl`. Debian Perl searches `/etc/perl` before `/usr/share/perl5`, also in taint mode. Every Proxmox service and command loads the patched copy. `pve-overlay` does not write to packaged files.

A dpkg hook runs `pve-overlay apply` after each package operation. The hook builds new copies from the upgraded modules. When a patch does not apply to an upgraded module, the hook deletes all patched copies and Proxmox runs the packaged modules.

## Use it on a node

Run these commands as `root` from a copy of this directory on the node. The dpkg hook runs the script from that path. Run `./pve-overlay apply` again after moving the copy.

1. Run `./pve-overlay check` to test that every patch applies to the installed packages.
2. Run `./pve-overlay apply`. The command writes the patched copies, installs the dpkg hook, and restarts `pvedaemon`, `pveproxy`, `pvestatd`, `pvescheduler`, and `pve-ha-lrm`.
3. Run `./pve-overlay status`. Each line must show `current`.

To return to packaged modules, run `./pve-overlay remove`. Packaged `qemu-server` deletes the `vsock` line from a VM config when it rewrites that config.

## Create a scoped token

The example grants one token the right to change `nesting` on container 101.

1. Create a role: `pveum role add CtNesting --privs "VM.Config.Nesting VM.Audit"`.
2. Create a user: `pveum user add svc-nesting@pve`.
3. Create a token with privilege separation: `pveum user token add svc-nesting@pve deploy --privsep 1`. Proxmox prints the secret once.
4. Grant the role to the user: `pveum acl modify /vms/101 --users svc-nesting@pve --roles CtNesting`.
5. Grant the role to the token: `pveum acl modify /vms/101 --tokens 'svc-nesting@pve!deploy' --roles CtNesting`.

Both grants are required. The token cannot change another option or another guest.

## Drift check

The `Proxmox overlay drift` workflow runs daily and on each push that changes this directory. It installs the newest published Proxmox VE packages from the stable and test repositories, applies the patches, and compiles the patched modules. A failed run means a patch needs a rebase in its fork.

To update a patch after a rebase, run `git format-patch -1 --base=<upstream commit> --output=<patch file>` on the `overlays` branch of the fork.
