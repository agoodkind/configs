# Guest state and service deployment in OpenTofu

OpenTofu declares guest files, installed packages, and systemd units. The
provider reads each declared resource from the guest during planning. This
design replaces the `prep-guests` playbook and defines shared provider
requirements for Tack and MWAN deployment.

## Defect this replaces

| Property | Defect |
| --- | --- |
| The preparation playbook does not record each resource's state. | The marker `/var/lib/configs/guest-prep-revision` records that the procedure finished once. A package removed later, or a changed file, is invisible to every deploy. |
| The preparation playbook repeats every step. | Each run creates a new SMTP2GO password for the guest and rewrites `/etc/msmtprc`. Tack mounts that file into a container, and the mount goes stale. |
| The preparation playbook requires guest SSH. | A guest with broken SSH needs a second code path through `pct push`. The sshd files are written twice, once by each path. |
| The preparation playbook uses a completion marker. | The play deletes the marker first and writes it last. Six service deploys read it. |

## Contract

### 1. Transport

The provider calls each hypervisor's Proxmox API with an API token. It uses
the overlay methods `exec`, `exec-status`, and `exec-ack` for both containers
and virtual machines, with `VM.Guest.Exec` on the selected guest. The VM
transport requires the QEMU guest agent. The provider polls the returned
command identifier, reads the complete result, and acknowledges it without
repeating the command. File transfer uses chunked command input. The
[provider transport contract](https://github.com/agoodkind/terraform-provider-pveguest/blob/main/README.md)
defines the API paths, limits, timeouts, and required overlay behavior.

The provider does not require guest SSH, an authorized key in the guest, a
controller network route to the guest, or root access to the hypervisor. One
transport serves a healthy guest and a guest with broken SSH.

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
| `pveguest_apt_packages` | Packages, versions, upgrade restrictions, and declared removal policy | `dpkg-query --show` and package policy reads |
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

One shared module declares the base state of one guest. Each application root
creates one module instance for each existing enrolled guest from the Ansible
service mapping. A guest joins through an explicit enrolled set.

| `prep-guests` item | Declaration |
| --- | --- |
| SSH key file, sshd drop-ins, tmpfiles entry | `pveguest_file`, with `validate` on the drop-ins and `restart_on` for `ssh` |
| Base packages, `qemu-guest-agent` on VMs | `pveguest_apt_packages` |
| Package updater script, unit, and timer | `pveguest_file` and `pveguest_systemd_unit` |
| rsyslog, timeout, getty, login profile, debug helper | `pveguest_file`, with `restart_on` where a unit reads the file |
| Locale and timezone | The module uses `pveguest_locales` for generated locales, `pveguest_file` for `/etc/locale.gen` and `/etc/locale.conf`, and `pveguest_link` for `/etc/default/locale` and `/etc/localtime`. |
| `systemd-networkd-wait-online` disabled | `pveguest_systemd_unit` |
| `/root/.bashrc` line edits | The module uses `pveguest_file` for `/etc/profile.d/98-prep-guests-environment.sh`, which login shells read. |
| `EXTERNALLY-MANAGED` removal | `pveguest_file` for `/etc/pip.conf` |
| `/opt/scripts` installer | The module declares the update script, service, timer, and both PATH files with `pveguest_file`. The module enables and starts `scripts-updater.timer` with `pveguest_systemd_unit`. |
| Mail relay file | The module uses `pveguest_mail_relay` for each declared SMTP2GO user and its guest mail configuration. |
| Hostname | Already declared by the Proxmox provider for containers |

The guest module uses the declared timezone instead of the deployment
machine's timezone.

### 7. Mail credential

Each guest that requires mail declares one SMTP2GO user. The provider generates
its password once and preserves it across applies. A version number rotates it: a higher
number creates a new password, updates the SMTP2GO user, and rewrites the
file. The `pveguest_mail_relay` resource Read warns when the SMTP2GO user is
missing or blocked.

### 8. Readiness gate

The marker file is removed. OpenTofu records each declared resource in state.
The readiness gate requires a plan with zero changes.

`configsctl deploy` plans the selected guest module in its owning application
root before a service deploy. It refuses deployment when that plan reports a
change or an error.

During migration an enrolled guest declares the marker as an ordinary file
that depends on every other item in the module. The Ansible check passes for
enrolled and unenrolled guests alike. The marker tasks are deleted after the
last guest is enrolled.

### 9. Unreachable guest

Read fails for a stopped container and for a VM without a running guest agent.
The error message includes the guest. `-exclude` on that guest module instance
allows a plan for the others.

### 10. Application dependencies

The [workspace separation design](../specs/2026-10-23-workspace-separation-design.md)
defines workspace ownership, deployment checks, host prerequisites, encryption,
and the coordinated state transfer.

The PowerEdge routing root orders its guest operations after its host
prerequisite read.

1. The read-only host module data source checks the required loaded modules.
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

### 11. Approaches

The adopted approach combines existing general providers in Configs and extends
them only for demonstrated gaps. Tack and MWAN providers implement their own
application rules. This preserves shared package, file, service, container, and
database behavior while allowing each service to migrate independently.

A single custom service provider would duplicate general resources and require
separate maintenance for the same operation. The reuse requirement excludes that
approach. Wrapping an Ansible playbook in a command resource would retain Ansible
and would not make each managed object's live state visible in a plan. The
read and planning requirements exclude that approach too.

### 12. Tack and MWAN migration

Configs combines infrastructure, reusable configuration, and application
resources in each declared deployment. Existing providers manage the state
they already support. A missing general capability requires a reusable
extension to the appropriate provider. The shared guest module remains the
replacement for `prep-guests`; Tack and MWAN reuse it.

The reuse review also covers containers, image downloads and digests, registry
access, database administration, credentials, certificates, backups, snapshots,
host configuration, and networking. Use each operation's state and contract to
select its provider. The service that first needs an operation does not
determine its implementation owner. A shared configuration module combines
resources without duplicating their implementation.

Docker management must have a tested bootstrap and recovery path that does
not require a connection to the guest's IP address or a working Docker
network endpoint. Use the existing Proxmox guest API for prerequisite setup
and repair. Verify the provider's actual configuration and refresh behavior
before selecting its connection method. Reuse generic Docker resources and
implement only a verified generic capability gap.

The [approved Tack design](https://github.com/agoodkind/tack/blob/main/docs/superpowers/specs/2026-10-10-tack-provider.md)
governs Tack deployment. Application-specific resources implement rules such
as Tack's schema compatibility and coordinated search replacement, or MWAN's
network intent and deployment health decisions. Every proposed custom resource
must identify the existing providers considered, their exact capability gaps,
and the application rules that require a separate implementation.

Every managed object and field has one writer. Resources that manage deployed
objects read live state and show meaningful proposed changes before apply.
MWAN's configuration-only projections retain the network planning contract
referenced in section 10. Resources that write deployed state own its live readback.
A record that a command ran does not establish the resulting state. Deployed
object contracts must cover import, changes, removal, failed operations, and
recovery after interruption. Import preserves existing cluster identities
and data volumes. A provider starts writing a field only after the previous
Ansible writer stops managing it.

One owner maintains the shared capability list and coordinates generic
provider changes. Tack and MWAN development proceed in parallel. Each task
depends only on the capabilities it uses; unrelated work continues while a
dependency remains unfinished. The [implementation plan](plan.md) requires an
owner, a concrete action, evidence, dependencies, and a completion check for
each remaining item. A genuine decision requires options and a recommendation.

Suburban and Vault use the same reusable modules with explicit environment
inputs. Their adoption records remain separate. Declaring a VM or container
does not declare the application inside it. An implemented provider resource
does not establish that an application root declares, imports, or deploys it.
Acceptance records each of those results separately.

Guest preparation includes the required timezone and mail declarations.
Consumers verify the selected package and Docker transport capabilities before
proposing extensions. Bootstrap and recovery acceptance still apply when a
provider or adapter already implements the required operation.

Tack coverage includes container runtime state, FoundationDB and YugabyteDB
administration, schema migration ownership, coordinated search replacement,
audit configuration, credentials, certificates, backup policy, and backup
mounts. Its approved deployment specification remains the detailed authority.
MWAN coverage includes gateway and failover installation, safe deployment and
recovery, host services, observers, OPNsense, and both ISP and routing simulators.
General installation stays separate from each application's health decisions.

Simulator coverage includes firewall, sysctl, router advertisement, return routes,
Kea configuration and service transitions, management authorization, observer
installation, and routing runtime. Completion verifies actual client traffic
in both directions on every applicable path. Preserve current tertiary policy,
which uses DHCPv6 addresses without prefix delegation.

The controller must select connection settings, credentials, and lock targets
without executing Ansible inventory or vault commands for the migrated services.
Shared YAML values may remain inputs while other services still use Ansible;
renaming their directory does not prove controller independence. Ordered CI
deployment follows its own approved contracts and consumes the same controller
implementation. Local service migration does not wait for unrelated CI tasks.

## Boundaries

- Tack and MWAN must install, configure, update, and recover without invoking
  Ansible. Other services keep their Ansible service deployments as future work.
  Removal of the playbooks alone does not satisfy the completion criteria.
- `deploy-ssh-keys` is deleted after the last guest is enrolled. The transport
  in section 1 repairs a locked-out guest.
- Guest preparation uses apt and systemd on Debian. Required FreeBSD router
  operations use the selected general capability from the reuse review.
  Alpine guest preparation remains outside this scope.
- Use the provider's chunked transfer for large files. Do not bypass its command
  input or captured-output limits.
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
- An application root that declares guest base state requires the overlay
  guest methods and an API token on its own hypervisor.
- Guests marked `inventory: false` are enrolled like any other guest. The
  transport does not require an inventory address.

## Acceptance criteria

- AC1: A plan after an apply on `clyde_suburban` does not report changes.
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
- AC16: Tack and MWAN reuse generic provider resources and the shared guest
  module. Applicable AC1 through AC8 pass. Every custom resource has the
  section 12 justification. A shared capability passes acceptance for each
  consumer that requires it. Docker acceptance includes fresh-guest bootstrap
  and repair when guest management networking is unavailable.
- AC17: Tack passes its approved deployment acceptance requirements on QA
  and production without invoking Ansible. Adoption preserves identities and
  data volumes. Planning does not change deployed state. Drift repair and
  interrupted apply recovery pass. The next plan reports zero changes, and a
  second apply does not perform extra work.
- AC18: MWAN passes installation, configuration, update, recovery, and
  applicable AC11 through AC15 checks without invoking Ansible. Coverage
  includes production, the test environment, ISP and routing simulators,
  failover guests, host services, observers, and required OPNsense operations.
  Simulator acceptance verifies addresses, routes, and client traffic.
  Adoption, drift repair, and interrupted recovery preserve service state.
  The next plan reports zero changes, and a second apply does not perform
  extra work.
- AC19: The completed capability list accounts for every Tack and MWAN
  lifecycle task, including shared inventory, locking, and controller
  dependencies. Execution evidence confirms that their supported entrypoints
  do not invoke Ansible. Other service deployments retain their existing
  behavior. Existing production and dormant LAN authorization limits apply.

## Migration order

Guest enrollment and service migration can overlap. Each task requires only
its own prerequisites. Completing enrollment of every guest does not gate
independent Tack or MWAN work.

1. Preserve the application roots, host roots, and encrypted states established
   by the workspace separation design. Do not recreate a shared guest root.
2. Reuse existing guest preparation resources and complete missing root inputs.
   Enroll the applicable Vault guests without replacing their existing guests.
3. Keep unrelated service preparation and marker consumers until their owners
   complete enrollment. Retire Tack and MWAN dependencies independently.
4. Complete the shared capability review and assign implementation owners.
   Record existing resources, verified gaps, dependencies, actions, and
   completion checks. Confirm coverage of both services before selecting
   application-specific resources.
5. Develop generic capabilities, Tack resources, and MWAN resources in
   parallel. Each adoption requires acceptance of its actual dependencies.
   Import existing state and stop each previous writer before provider
   writes. Complete the applicable AC16 checks for each adopted capability.
6. Prove Tack on QA and MWAN on the test environment independently, then
   adopt their production environments within existing authorization.
   Complete AC17 and AC18. Neither service waits for the other to finish.
7. Remove replaced Tack and MWAN Ansible tasks and dependencies after their
   acceptance checks pass. Complete AC19 while preserving other service
   deployments. Each step orders its actual dependencies, not unrelated work.

Tack epic LAB-65 tracks this work. LAB-71, LAB-72, LAB-74, and LAB-77 track
the guest API transport.

## Future plans

The selected design uses overlay API methods and an API token. The options
below are recorded for reconsideration. None of them is selected or scheduled.

| Option | Current constraint | Tradeoff | Reconsider when |
| --- | --- | --- | --- |
| Root SSH to the hypervisor with `pct exec` and `qm guest exec` | The owner rejects root SSH as a design. | The option does not require an overlay and works on stock Proxmox. It grants root on every hypervisor to the machine that runs OpenTofu. | The owner changes the root SSH rule. |
| SSH into each guest | A guest with broken SSH requires a second repair path. | The option does not require hypervisor privileges. Each guest needs sshd and an authorized key before OpenTofu manages it. | The overlay guest methods stop applying to a Proxmox release and a patch is not feasible. |
| Upstream the guest methods to Proxmox | The overlay patches each Proxmox release. | Upstream methods remove the overlay maintenance. Acceptance and timing depend on the Proxmox maintainers. | The overlay methods run on all three hypervisors without a change for one Proxmox minor release. |
| A Debian package for the overlay | `pve-overlay` installs Perl modules and AppArmor profiles from patch files. | A package can install binaries and units and uses dpkg triggers. It needs a build pipeline and a package source. | The overlay needs a compiled helper, or it installs more than ten host files. |
| A liveness check for exec workers | A pvedaemon restart during a command leaves its result missing, and `exec-status` reports a running command for up to two hours. | A check of the worker process detects the loss at once. It adds a process identity record per command. | A guest workspace apply fails or waits on a lost worker. |
