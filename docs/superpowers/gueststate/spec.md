# Guest base state in OpenTofu

OpenTofu declares the base state of every Proxmox guest: files, apt packages,
and systemd units. A plan reads each item from the guest and compares it with
the declaration. This design replaces the `prep-guests` playbook.

## Defect this replaces

`prep-guests` is a procedure. Four properties of a procedure cause the defects
below.

| Property | Defect |
| --- | --- |
| A run leaves no per-item record | The marker `/var/lib/configs/guest-prep-revision` records that the procedure finished once. A package removed later, or a changed file, is invisible to every deploy. |
| A run repeats every step | Each run creates a new SMTP2GO password for the guest and rewrites `/etc/msmtprc`. Tack mounts that file into a container, and the mount goes stale. |
| The in-guest play needs guest SSH | A guest with broken SSH needs a second code path through `pct push`. The sshd files are written twice, once by each path. |
| A half-finished run needs a guard | The play deletes the marker first and writes it last. Six service deploys read it. |

## Contract

### 1. Transport

The provider opens SSH to the hypervisor as root through the SSH agent. It runs
each command inside a container with `pct exec <vmid> --` and inside a VM with
`qm guest exec <vmid> --`. File content goes in on stdin.

The guest needs no sshd, no authorized key, and no network path from the
controller. One transport serves a healthy guest and a guest with broken SSH.

A test on 2026-10-04 passed 256 KiB of random bytes through `pct exec` on the
suburban hypervisor in both directions without change. A failing command
returned its own exit status.

### 2. Provider

A new provider, `pveguest`, is built on `terraform-plugin-framework` with
protocol 6 in its own repository, `agoodkind/terraform-provider-pveguest`.
OpenTofu installs it from a filesystem mirror. No registry publication is
required.

Each resource identifies its guest with three arguments: `node`, `vmid`, and
`kind` (`lxc` or `qemu`). The provider block lists the hypervisors and their
SSH hosts. One provider configuration serves every guest, and `for_each` over
guests works on the resources.

| Resource | Declares | Read command in the guest |
| --- | --- | --- |
| `pveguest_file` | Path, content, mode, owner, group. Also absence. | `stat` and `sha256sum` |
| `pveguest_link` | A symbolic link and its target | `readlink` |
| `pveguest_apt_packages` | A set of installed package names | `dpkg-query --show` |
| `pveguest_systemd_unit` | Enabled state, active state, and restart triggers | `systemctl is-enabled` and `systemctl is-active` |

The apt and systemd command logic follows `neuspaces/system` (MPL-2.0) as a
reference. Its known defects are fixed in the new code: a missing unit is a
normal Read result, a missing file is a normal Read result, and
`apt-get update` runs only before an install and reports its own error.

### 3. Drift

Read returns the state of the guest at plan time. An item that is missing in
the guest is removed from state, and the plan proposes to create it. A changed
file hash, a removed package, a disabled unit, and a stopped unit each produce
a plan change.

Read runs no command that changes the guest.

### 4. Restart on change

`pveguest_systemd_unit` has a `restart_on` map. A changed value in the map
restarts the unit during apply. The map values are file hashes. This replaces
Ansible handlers.

`pveguest_file` writes a temporary file, sets mode and owner, and renames it.
An optional `validate` command runs on the temporary file before the rename.
The sshd drop-ins use `sshd -t`.

### 5. Secrets

`pveguest_file` has `content_wo` and `content_wo_version`. OpenTofu stores
neither the content nor the plan value. Read compares the guest hash with the
hash recorded at the last apply.

### 6. Guest module

One shared module, `opentofu/guest`, declares the base state of one guest. A
root file instantiates it with `for_each` over guests from the Ansible service
mapping, in the same shape as the overlay module. A guest joins through an
explicit enrolled set.

| `prep-guests` item | Declaration |
| --- | --- |
| SSH key file, sshd drop-ins, tmpfiles entry | `pveguest_file`, with `validate` on the drop-ins and `restart_on` for `ssh` |
| Base packages, `qemu-guest-agent` on VMs | `pveguest_apt_packages` |
| Package updater script, unit, and timer | `pveguest_file` and `pveguest_systemd_unit` |
| rsyslog, timeout, getty, login profile, debug helper | `pveguest_file`, with `restart_on` where a unit reads the file |
| Locale and timezone | `pveguest_file` for `/etc/locale.gen` and `/etc/default/locale`, `pveguest_link` for `/etc/localtime` |
| `systemd-networkd-wait-online` disabled | `pveguest_systemd_unit` |
| `/root/.bashrc` line edits | A `/etc/profile.d` file |
| `EXTERNALLY-MANAGED` removal | `pveguest_file` for `/etc/pip.conf` |
| `/opt/scripts` installer | The installer file pinned to a commit, as in the overlay module |
| Mail relay file | `pveguest_file` with `content_wo` |
| Hostname | Already declared by the Proxmox provider for containers |

The timezone is one declared value. It no longer follows the clock of the
machine that runs the play.

### 7. Mail credential

Each guest has one SMTP2GO user. The password is generated once and stays the
same across applies. A declared version number per guest rotates it: a higher
number creates a new password, updates the SMTP2GO user, and rewrites the
file. A `check` block reads the SMTP2GO user and warns when it is missing or
blocked.

A leaked password affects one guest.

### 8. Readiness gate

The marker file is removed. State is the record of each item, and a plan with
no changes is the proof that a guest matches its declaration.

`configsctl deploy` runs a plan for the target guest before a service deploy
and refuses to deploy when the plan reports a change or an error.

During migration an enrolled guest declares the marker as an ordinary file
that depends on every other item in the module. The Ansible check passes for
enrolled and unenrolled guests alike. The marker tasks are deleted after the
last guest is enrolled.

### 9. Unreachable guest

Read fails for a stopped container and for a VM without a running guest agent.
The error message includes the guest. `-exclude` on that guest module instance
allows a plan for the others.

## Boundaries

- Service deploys remain in Ansible.
- `deploy-ssh-keys` is deleted after the last guest is enrolled. The transport
  in section 1 repairs a locked-out guest.
- The first version covers apt and systemd on Debian. Alpine guests are out of
  scope.
- A VM file larger than 1 MiB exceeds the stdin limit of `qm guest exec`.
- The guest module declares base state only. On the MWAN gateway the daemon
  owns `/etc/systemd/network/10-mwan-*.link` and the `20-<interface>.*` files,
  and the MWAN deploy owns `/etc/mwan/`. The module declares no file under
  those paths.
- The MWAN deploy and the hypervisor watchdog also use the guest agent of the
  gateway VM. An apply for an MWAN guest runs only in a window agreed with
  the MWAN deploy owner. The guest agent is unavailable for about 30 seconds
  after a gateway reboot or a snapshot restore, and Read fails during that
  time.
- A plan needs root SSH to each hypervisor with an enrolled guest.
- Guests marked `inventory: false` are enrolled like any other guest. The
  transport needs no inventory address.

## Acceptance criteria

- AC1: A plan after an apply on `clyde_suburban` reports no changes.
- AC2: After a declared package is removed in the guest by hand, a plan
  proposes to install it.
- AC3: After a declared file is edited in the guest by hand, a plan proposes
  to rewrite it, and the apply restarts each unit that lists the file in
  `restart_on`.
- AC4: After a declared timer is disabled in the guest by hand, a plan
  proposes to enable it.
- AC5: With sshd stopped in the guest, an apply writes the SSH key file and
  the drop-ins and starts sshd.
- AC6: An sshd drop-in that fails `sshd -t` fails the apply, and the previous
  file is unchanged.
- AC7: Two applies in a row do not change the SMTP2GO password or
  `/etc/msmtprc`. A higher version number changes both.
- AC8: The state file and the plan output contain no mail password.
- AC9: `configsctl deploy` refuses a service deploy for a guest with a pending
  plan change and proceeds for a guest with none.
- AC10: A plan with one stopped guest fails with an error that includes the
  guest, and the same plan with `-exclude` for that guest succeeds.
- AC11: The MWAN VM on suburban passes AC1 through AC4 through the guest
  agent.

## Migration order

1. Build `pveguest_file` and `pveguest_systemd_unit`. Enroll `clyde_suburban`
   with the SSH files and the static files. It runs no gated service.
2. Add `pveguest_apt_packages` and `pveguest_link`. Add the remaining items on
   the same guest.
3. Add the mail credential on the same guest.
4. Enroll the other suburban containers, then the suburban MWAN VM.
5. Add the plan gate to `configsctl deploy`.
6. Enroll the poweredge guests, then the vault guests one group at a time. The
   proxy guest is last.
7. Delete `prep-guests`, `deploy-ssh-keys`, the marker tasks, and their specs.

## Open decisions

| Decision | Options |
| --- | --- |
| Where the guest state is planned | A: the existing root module, with the enrolled set as the limit. B: a second root module with its own state file. A DNS change then opens no SSH session. `configsctl` needs a second module setting. |
| Mail delivery | A: one SMTP2GO user per guest, as in section 7. B: one internal relay guest and no secret on any other guest. |
| Scheduled drift report | A: none; drift appears at the next plan. B: a timer runs the plan daily and sends mail on a change. |
