# Guest base state in OpenTofu

OpenTofu declares guest files, installed packages, and systemd units. The
provider reads each declared resource from the guest during planning. This
design replaces the `prep-guests` playbook.

## Defect this replaces

| Property | Defect |
| --- | --- |
| The preparation playbook does not record each resource's state. | The marker `/var/lib/configs/guest-prep-revision` records that the procedure finished once. A package removed later, or a changed file, is invisible to every deploy. |
| The preparation playbook repeats every step. | Each run creates a new SMTP2GO password for the guest and rewrites `/etc/msmtprc`. Tack mounts that file into a container, and the mount goes stale. |
| The preparation playbook requires guest SSH. | A guest with broken SSH needs a second code path through `pct push`. The sshd files are written twice, once by each path. |
| The preparation playbook uses a completion marker. | The play deletes the marker first and writes it last. Six service deploys read it. |

## Contract

### 1. Transport

The provider calls the Proxmox API of each hypervisor with an API token. For a
container it uses the overlay methods `exec`, `exec-status`, `file-read`, and
`file-write` under `/nodes/{node}/lxc/{vmid}/`, which require the privileges
`VM.Guest.Exec`, `VM.Guest.FileRead`, and `VM.Guest.FileWrite` on
`/vms/{vmid}`. For a VM it uses the guest agent methods `agent/exec` and
`agent/exec-status`. `exec` returns at once, and the provider reads the result
with `exec-status` after the command exits. The provider writes file content
to the guest command's standard input in chunks.

The provider does not require guest SSH, an authorized key in the guest, a
controller network route to the guest, or root access to the hypervisor. One
transport serves a healthy guest and a guest with broken SSH.

The provider acceptance tests passed on poweredge container 9004 on
2026-10-04 through these methods with a token that has only the three guest
privileges.

### 2. Provider

A new provider, `pveguest`, is built on `terraform-plugin-framework` with
protocol 6 in its own repository, `agoodkind/terraform-provider-pveguest`.
OpenTofu installs the provider from a filesystem mirror. The installation does
not require registry publication.

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
reference. Read treats missing files and units as absent resources.
`apt-get update` runs only before installation and reports its own error.

### 3. Drift

Read returns the state of the guest at plan time. An item that is missing in
the guest is removed from state, and the plan proposes to create it. A changed
file hash, a removed package, a disabled unit, and a stopped unit each produce
a plan change.

Read does not modify the guest.

### 4. Restart on change

`pveguest_systemd_unit` has a `restart_on` map. A changed value in the map
restarts the unit during apply. The map values are the `write_id` of each file
that the unit reads. `pveguest_file` changes `write_id` at every write. This
replaces Ansible handlers.

`pveguest_file` writes a temporary file, sets mode and owner, and renames it.
An optional `validate` command runs on the temporary file before the rename.
The sshd drop-ins use `sshd -t`.

### 5. Secrets

`pveguest_file` has `content_wo` and `content_wo_version`. OpenTofu stores
neither the content nor the plan value. Read compares the guest hash with the
hash recorded at the last apply.

### 6. Guest module

One shared module declares the base state of one guest. The guest workspace
creates one module instance for each enrolled guest from the Ansible service
mapping. A guest joins through an explicit enrolled set.

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

The guest module uses the declared timezone instead of the deployment
machine's timezone.

### 7. Mail credential

Each guest has one SMTP2GO user. The password is generated once and stays the
same across applies. A declared version number per guest rotates it: a higher
number creates a new password, updates the SMTP2GO user, and rewrites the
file. A `check` block reads the SMTP2GO user and warns when it is missing or
blocked.

### 8. Readiness gate

The marker file is removed. OpenTofu records each declared resource in state.
The readiness gate requires a plan with zero changes.

`configsctl deploy` runs a plan of the `guest` workspace for the target guest
before a service deploy and refuses to deploy when the plan reports a change
or an error.

During migration an enrolled guest declares the marker as an ordinary file
that depends on every other item in the module. The Ansible check passes for
enrolled and unenrolled guests alike. The marker tasks are deleted after the
last guest is enrolled.

### 9. Unreachable guest

Read fails for a stopped container and for a VM without a running guest agent.
The error message includes the guest. `-exclude` on that guest module instance
allows a plan for the others.

### 10. Workspaces

A workspace is one directory of OpenTofu files with its own state file, as in
HCP Terraform. A plan in one workspace reads and contacts only the systems
that workspace declares.

`configsctl.yml` specifies the workspace directory. Each child directory with a
`backend` block defines a workspace. `configsctl tofu <workspace> <arguments>`
runs OpenTofu in that directory. configsctl does not maintain a separate
workspace-name list.

Each workspace sets its own state key in its `backend` block. Every state file
is in the same R2 bucket and uses the same encryption passphrase. The secret
rules apply per workspace: configsctl exports a vault key to a workspace only
when that workspace declares a variable with the same name.

Guest base state is the workspace `guest`. DNS and hypervisor plans do not
open guest sessions. A stopped guest affects the guest workspace plan.

Each workspace reads the Ansible service mapping. It does not read another
workspace's state.

The existing OpenTofu files become one workspace without a state change.

### 11. Host and guest dependencies

A guest that needs a host capability declares that capability as a dependency
of its guest module. OpenTofu applies the host side first.

OpenTofu applies these dependencies in order for the PowerEdge MWAN gateway
container (vmid 313, `mwan-poweredge`) and LAN container (vmid 314,
`lan-poweredge`).

1. `pveguest_host_kernel_modules` declares the 18 poweredge host kernel
   modules. The resource uses the overlay methods
   `GET /nodes/{node}/kernel-modules` and `PUT /nodes/{node}/kernel-modules`.
2. The containers declare `bpfdelegate`, nesting, and `hostnicN` for WAN port
   `nic2` and inter-container virtual functions `nic1v0` and `nic1v1`.
3. OpenTofu installs the mwan binary and package bundle.
4. OpenTofu installs the YANG files and sysrepo modules and data.
5. `pveguest_file` writes `/etc/mwan/network.json` and the firewall files.
   `validate` runs `mwan deploy-gate check-network` and
   `mwan deploy-gate check-firewall`.
6. OpenTofu applies the units with `restart_on` from those file writes.

A plan fails before apply when a guest configuration requires a host or peer
setting that the declared host configuration lacks. In MWAN-564, the
hypervisor configuration omitted `[bgp]`. The watchdog never started a
failover because it required `BGP.Enabled`.

The mwan [network plan specification](https://github.com/agoodkind/mwan/pull/202)
defines the network document and route plan semantics, schema authority, and
validation boundary.

For PowerEdge with the LAN dormant, a `lifecycle` precondition fails the plan
when the gateway network document declares an interface outside the
provider, parent, internal, and management roles, or a route on such an
interface. A precondition on the container fails the plan when any gateway
interface binds to `nic0`, `nic3`, or `ens1f1`, or to a bridge with one of
those LAN ports. LAN client traffic, LAN forwarding, and LAN-facing services
stay disabled until a separate user instruction.

## Boundaries

- Service deploys remain in Ansible.
- `deploy-ssh-keys` is deleted after the last guest is enrolled. The transport
  in section 1 repairs a locked-out guest.
- The first version covers apt and systemd on Debian. Alpine guests are out of
  scope.
- A VM file larger than 1 MiB exceeds the stdin limit of `qm guest exec`.
- For the PowerEdge gateway, OpenTofu writes `/etc/mwan/network.json` and
  the firewall file. The first apply requires deletion of the gateway's
  Ansible `network.json` render and install task. The daemon owns
  `/etc/systemd/network/10-mwan-*.link` and the `20-<interface>.*` files.
  A configuration change restarts the unit through `restart_on`. Live reload
  is deferred and is not part of this design.
- The MWAN deploy and the hypervisor watchdog also use the guest agent of the
  gateway VM. An apply for an MWAN guest runs only in a window agreed with
  the MWAN deploy owner. The guest agent is unavailable for about 30 seconds
  after a gateway reboot or a snapshot restore, and Read fails during that
  time.
- A plan of the `guest` workspace requires the overlay guest methods and an API
  token on each hypervisor with an enrolled guest.
- A split of the existing workspace into DNS and one workspace per hypervisor
  is separate work. Each split moves resources between state files.
- Guests marked `inventory: false` are enrolled like any other guest. The
  transport does not require an inventory address.

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
- AC8: The state file and the plan output do not contain a mail password.
- AC9: `configsctl deploy` rejects a guest with pending plan changes and
  permits a guest when its plan reports zero changes.
- AC10: A plan with one stopped guest fails with an error that includes the
  guest, and the same plan with `-exclude` for that guest succeeds.
- AC11: The MWAN VM on suburban passes AC1 through AC4 through the guest
  agent.
- AC12: A gateway plan fails before apply when the hypervisor configuration
  lacks a setting required by the gateway configuration (MWAN-564). The
  error states the missing setting.
- AC13: A failover drill on the suburban testbed moves the BGP announcement
  from the primary gateway to the failover container. The check reads the
  failover container's announce state and the primary gateway's withdrawn
  state instead of the watchdog log. The regression case is
  `mwan watchdog failover` sending both withdraw and announce to the primary
  agent without sending a request to container 216.
- AC14: On PowerEdge with the LAN dormant, the preflight passes checks of
  LAN forward drop counters, LAN forwarding sysctls of 0 in both containers,
  absent DHCP, DNS, and router advertisement listeners on LAN, and a packet
  capture on a LAN port. `mwan` starts with a ruleset equal to the rendered
  configuration. BGP between the gateway and LAN container adds, replaces,
  and withdraws `198.51.100.0/24` and `2001:db8:5::/48`.
  `ip route show proto bgp table all` checks both documentation prefixes
  after each operation. One WAN link down and up replaces and deletes the
  provider-table default route. A firewall rule change restarts the unit
  with the new ruleset and unchanged routes. The postflight equals the
  preflight.
- AC15: Before the gateway container starts, a plan reports the 18 poweredge
  modules as loaded. `PUT /nodes/{node}/kernel-modules` rejects a token with
  only `Sys.KernelModules.Audit` with HTTP 403. The HTTP 403 check passed on
  poweredge on 2026-10-06 in pveguest pull request 7.

## Migration order

1. Add workspace selection to configsctl and create the `guest` workspace.
   Build `pveguest_file` and `pveguest_systemd_unit`. Enroll `clyde_suburban`
   with the SSH files and the static files. clyde_suburban does not run a
   service subject to the readiness gate.
2. Add `pveguest_apt_packages` and `pveguest_link`. Add the remaining items on
   the same guest.
3. Add the mail credential on the same guest.
4. Enroll the other suburban containers, then the suburban MWAN VM.
5. Add the plan gate to `configsctl deploy`.
6. Enroll the poweredge guests, then the vault guests one group at a time. The
   proxy guest is last.
7. Enroll the PowerEdge gateway and LAN containers after the mwan release
   with the `guest-type` change, the `terraform-provider-mwan` release with
   pull request 202, and the overlay release with the `hostnic` option.
8. Delete `prep-guests`, `deploy-ssh-keys`, the marker tasks, and their specs.

Tack epic LAB-65 tracks this work. LAB-71, LAB-72, LAB-74, and LAB-77 track
the guest API transport.

## Future plans

The selected design uses overlay API methods and an API token. The options
below are recorded for reconsideration. None of them is selected or scheduled.

| Option | Current constraint | Tradeoff | Reconsider when |
| --- | --- | --- | --- |
| Root SSH to the hypervisor with `pct exec` and `qm guest exec` | The owner rejects root SSH as a design. | The option requires no overlay and works on stock Proxmox. It grants root on every hypervisor to the machine that runs OpenTofu. | The owner changes the root SSH rule. |
| SSH into each guest | A guest with broken SSH requires a second repair path. | The option requires no hypervisor privilege. Each guest needs sshd and an authorized key before OpenTofu manages it. | The overlay guest methods stop applying to a Proxmox release and a patch is not feasible. |
| Upstream the guest methods to Proxmox | The overlay patches each Proxmox release. | Upstream methods remove the overlay maintenance. Acceptance and timing depend on the Proxmox maintainers. | The overlay methods run on all three hypervisors without a change for one Proxmox minor release. |
| A Debian package for the overlay | `pve-overlay` installs Perl modules and AppArmor profiles from patch files. | A package can install binaries and units and uses dpkg triggers. It needs a build pipeline and a package source. | The overlay needs a compiled helper, or it installs more than ten host files. |
| An overlay privilege for VM command execution | The stock `agent/exec` method requires `VM.GuestAgent.Unrestricted`. | A narrow privilege limits a VM token to command execution. It adds one more overlay method. | The first VM guest is enrolled (AC11). |
| A liveness check for exec workers | A pvedaemon restart during a command leaves its result missing, and `exec-status` reports a running command for up to two hours. | A check of the worker process detects the loss at once. It adds a process identity record per command. | A guest workspace apply fails or waits on a lost worker. |
| A controller download with chunked `file-write` | `pveguest_download` runs `curl` in the guest and requires guest network access and `curl`. | A controller download serves a guest without egress. It sends the whole file through the API in 96 KiB chunks. | A guest without egress needs a pinned binary. |
