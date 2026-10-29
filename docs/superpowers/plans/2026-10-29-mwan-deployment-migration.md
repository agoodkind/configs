# MWAN Deployment Migration Implementation Plan

> Use `subagent-driven-development` or `executing-plans` to implement the tasks and track steps with checkbox syntax.

**Goal:** Install, configure, update, and recover MWAN on Suburban and Vault without Ansible, including required hosts, failover guests, observers, router operations, and Suburban simulators.

**Architecture:** Reuse the shared MWAN installation module and general resources. Preserve the existing deployment-operation engine for snapshot, readiness, commitment, and recovery decisions. Configuration projections remain separate from live installation and observation.

**Tech Stack:** OpenTofu, Go, configsctl, Proxmox, pveguest, MWAN, systemd, FreeBSD/OPNsense, Kea, and existing simulator tools.

**Spec:** Read the [migration contract](../gueststate/spec.md#12-tack-and-mwan-migration), [network planning specification](https://github.com/agoodkind/mwan/blob/main/docs/superpowers/networkplan/spec.md), and [coordination plan](../gueststate/plan.md).

## Global constraints

Apply every coordination-plan constraint. Preserve current WAN addressing, authentication, selection, failover, and observer policy. Deployment migration does not require new networking features. Keep PowerEdge with its existing owner and preserve tertiary deployment priority.

## Review focus

- A gateway VM uses a container-only module: select and verify QEMU transport explicitly.
- A reboot or lost controller interrupts deployment: resume by exact operation/generation and preserve baseline/lock identity.
- Saved configuration matches but the running network differs: verify routes, services, and packets at the owning boundary.
- Disabled IPv6/DHCP leaves a service running: verify explicit stop/disable state and listeners.
- Tertiary uses address-based IPv6: preserve current policy without re-enabling legacy prefix delegation.

## Task M1: Connect gateway and failover installation to both roots

Track integration under [MWAN-586](https://tack.home.goodkind.io/browse/MWAN-586). Consume the shared S2/S3 resources this task needs.

Modify the [shared module](../../../opentofu/modules/mwan/main.tf), [Suburban root](../../../opentofu/suburban/mwan/main.tf), and [Vault root](../../../opentofu/vault/mwan/main.tf). Read the [gateway playbook](../../../ansible/playbooks/deploy-mwan.yml), [role staging](../../../ansible/playbooks/tasks/stage-mwan-role-inputs.yml), and [failover playbook](../../../ansible/playbooks/deploy-mwan-failover.yml) for current behavior.

The module already declares release downloads, package bundles, role files, sysrepo schemas/data, network projection, runtime files, and restart dependencies. It hardcodes LXC kind and neither root declares an instance. Both roots already declare failover DHCPv6; preserve those resource addresses or use explicit moved blocks when consolidating ownership.

- [ ] Add module input `kind: string`, restricted to `lxc` or `qemu`, with compatibility default `lxc`. Use it for every guest resource and `mwan_role.guest_type`. Declare gateway instances with `qemu` and failover instances with `lxc`.
- [ ] Preserve current role files, release digests, package bundles, network/runtime configuration, protected secret references, sysctls, externally owned networkd files, and service state. Transfer each field only after replacement acceptance.
- [ ] Preserve native network/firewall validation before writes and restart dependencies afterward. The configuration-only network projection does not replace live observation or require a new live-reload feature.
- [ ] Verify both guest kinds through real OpenTofu with released role data: correct target identity, binary digest, configuration readback, active units, invalid-input rejection, drift repair, and clean repeat plans.
- [ ] Commit VM-kind support separately from root declarations/adoption. Pin the accepted provider/release pair before M5.

## Task M2: Preserve deployment operations without Ansible

Consume M1's declared persistent state and shared S4's single-scope runner. General resources own files, packages, and services. MWAN owns application checks and recovery; the controller sequences them around the exact saved plan under one lock set.

Reuse the [existing operation command and engine](https://github.com/agoodkind/mwan/blob/main/gateway/cmd/mwan/deploygate_operation.go). Port the behavior of [operation arming](../../../ansible/playbooks/tasks/arm-mwan-operation.yml) and [reboot verification](../../../ansible/playbooks/tasks/reboot-and-require-new-boot.yml) into that runtime/controller boundary. Keep application protocol logic in MWAN and typed manifest inputs in Configs. Do not add another operation database or snapshot supervisor.

- [ ] Consume the existing manifest/status types for actual host/guest identities, baseline boot identity, candidate/baseline digests, verified observers, checks, deadlines, and recovery settings. Reject incomplete or mismatched identities before mutation.
- [ ] Before provider writes, execute `mwan deploy-gate arm <manifest> --config <absolute-runtime-path>` through the approved host transport. Require mutation readiness for that operation; use its existing lease/watch protocol while applying the saved plan.
- [ ] Perform the required reboot, require changed boot identity, run existing inbound/downstream/failover checks, and commit with `verify` for the same operation ID/generation. Preserve the engine's snapshot, recovery, release, and cleanup semantics.
- [ ] Read `status` before retrying an interrupted deployment. Recognize a committed remote result without reinstalling; recover only the exact existing operation. Preserve ownership when remote completion remains uncertain.
- [ ] Extend existing Go deployment-operation acceptance through real processes/guests for commitment, rejected observer identity, failed readiness, controller loss after installation, failed recovery, and resumed remote commitment. Assert actual snapshots, boot identity, application results, operation status, and lock release on Suburban.
- [ ] Commit runtime/controller integration separately from Configs declarations. Do not wrap the old playbook or use a command-success record as gateway state.

## Task M3: Declare hosts, observers, and OPNsense

Track generic capabilities under shared LAB-93 and MWAN integration under MWAN-586. Consume S1's selected host/FreeBSD/Proxmox contracts. Verify actual API permissions through the approved transport; a guest resource cannot configure a physical host by changing its `vmid`.

Modify the host roots [on Suburban](../../../opentofu/suburban/host/main.tf) and [on Vault](../../../opentofu/vault/host/main.tf), and OPNsense roots [on Suburban](../../../opentofu/suburban/opnsense/main.tf) and [on Vault](../../../opentofu/vault/opnsense/main.tf). Read [host tasks](../../../ansible/playbooks/tasks/proxmox-host.yml), [observer policy](../../../ansible/playbooks/tasks/mwan-host-observer-policy.yml), [host deployment](../../../ansible/playbooks/tasks/mwan-opnsense-host-deploy.yml), [router deployment](../../../ansible/playbooks/deploy-opnsense.yml), and [router daemon tasks](../../../ansible/playbooks/tasks/mwan-opnsense-deploy.yml).

- [ ] Declare host interface-manager/watchdog binaries, configuration, units/drop-ins, observer policy, bridge/drainer state, required sysctl/NAT/routes, and VM communication arguments through general resources. Preserve existing bridge/VLAN declarations and host identity.
- [ ] Declare the released observer executable, required tools, and management authorization on the actual observer guests. Keep machine/host identity checks in the deployment verifier. Preserve observer networking during installation.
- [ ] Declare the FreeBSD daemon, rc.d service files, shim, loader/enable settings, and links. Preserve coordinated stop/replace/start behavior and verify compatible host/guest versions, one daemon, correct pidfile, RPC version, and hostname execution.
- [ ] Replace required OPNsense NAT64 and DNS operations with the selected general interface. Verify real NAT64 traffic and DNS responses. Preserve the administration GUI and limit the optional HTTP mapping fixture to its explicit testbed use.
- [ ] Verify running/persisted state after a Suburban host/router reboot, management recovery without guest SSH, watchdog readiness, observer identity, and clean plans. Commit independently verifiable host, observer, and router capabilities separately; adopt under M5.

## Task M4: Complete ISP and routing simulator ownership

Track under [LAB-94](https://tack.home.goodkind.io/browse/LAB-94). Consume the required shared S2/S3 capabilities. Reuse the [ISP module](../../../opentofu/modules/suburban/simulators/ispsim/main.tf), which already declares packages, nftables, sysctl, radvd, and return-route units. Preserve adopted state and rendered behavior.

Read remaining [ISP tasks](../../../ansible/playbooks/tasks/deploy-testbed-isp-lxc.yml), [management tasks](../../../ansible/playbooks/tasks/testbed-isp-management-guest.yml), and [routing tasks](../../../ansible/playbooks/tasks/deploy-routing-simulators.yml). Configs owns topology/scenario values; generic providers own package/file/unit behavior.

- [ ] Declare Kea DHCPv4/DHCPv6 configuration, rebind helper/unit, required service state, and restart dependencies. Explicitly stop/disable unsupported families and stop/disable/mask obsolete dnsmasq. Verify required failed-state cleanup through the selected service interface.
- [ ] Declare SSH authorization, both required key-file paths, removal of obsolete drop-ins, stopped socket activation, active SSH, and observer tools. Validate SSH configuration before activation and preserve identity checks.
- [ ] Declare routing simulator packages, daemon configuration, and service state, plus testbed host sysctl/NAT/transit/MAC settings that provisioning alone does not manage. Preserve containers, bridges, identities, and independent scenarios.
- [ ] Validate/plan/apply each affected root under its actual locks. Verify DHCP leases, applicable router advertisements and BGP learning/withdrawal, and actual client requests/replies on every selected simulator. Preserve ordinary IPv4 traffic and other scenarios.
- [ ] Exercise disablement and reboot on Suburban. Require expected listeners/routes/services, restoration of temporary rules/addresses, a clean plan, and a second apply without work. Current tertiary uses DHCPv6 addresses and NAT66 without prefix delegation.
- [ ] Commit remaining ISP state separately from routing runtime and host declarations. Remove only accepted matching Ansible writers.

## Task M5: Adopt environments and retire command dependencies

Track testbed adoption under [MWAN-587](https://tack.home.goodkind.io/browse/MWAN-587) and production under [MWAN-588](https://tack.home.goodkind.io/browse/MWAN-588). Consume ready M1 through M4 groups and shared S4. Application selectors are `suburban/mwan` and `vault/mwan`; host/router/simulator roots keep independent selectors.

- [ ] Produce the coordination manifest and refresh actual locks, releases, operation/backend ownership, guest identities, and import addresses. Do not infer readiness from a projection or old checklist.
- [ ] Import/review a ready Suburban group, retire its competing writer, and apply through the controller. Run applicable AC11/AC12/AC13/AC18 checks, including observed primary withdrawal/failover announcement and real client return traffic. Release each reservation when its operation ends.
- [ ] Adopt the corresponding Vault group after Suburban acceptance and within current authorization. Verify installed revisions, services, application traffic, final plans, and lock release. Preserve production failure-test limits.
- [ ] Verify install/configure/update/recover entrypoints with Ansible unavailable. Retire their inventory/vault-command/preparation-marker/playbook dependencies while preserving other services.
- [ ] Commit writer retirement with accepted declarations and evidence. Close MWAN-585 only after both environments and Suburban simulators satisfy AC18/AC19. Preserve the separate PowerEdge owner's AC14/AC15 obligations without blocking independent work.
