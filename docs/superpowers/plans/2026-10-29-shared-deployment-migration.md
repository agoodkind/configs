# Shared Deployment Migration Implementation Plan

> Use `subagent-driven-development` or `executing-plans` to implement the tasks and track steps with checkbox syntax.

**Goal:** Tack and MWAN must use reusable guest resources and controller interfaces without Ansible.

**Architecture:** General providers manage shared state. The OpenTofu configuration combines their resources in the existing application roots. Local commands and CI reuse one execution and locking implementation.

**Tech Stack:** The implementation uses OpenTofu, Go, configsctl, pveguest, Proxmox, and the Docker provider.

**Spec:** The [migration contract](../gueststate/spec.md#12-tack-and-mwan-migration) and the [coordination plan](../gueststate/plan.md) define the required behavior and ownership.

## Global constraints

Apply every coordination-plan constraint. Preserve the guest API bootstrap path, encrypted state ownership, existing lock protocol, and unrelated service deployment. Provider implementation, declaration, import, and runtime acceptance require separate evidence.

## Review focus

- A removal extension changes existing consumers: preserve the old default and require explicit removal policy.
- A package upgrade rewrites locale configuration: establish one authority without suppressing all file drift.
- Docker or guest networking fails: repair prerequisites through the existing guest API.
- A lock renewal fails during remote work: inspect completion and preserve uncertain ownership.
- YAML remains an input: prove the migrated command reads it without executing Ansible.

## Reuse existing implementation

The existing source establishes this starting point:

| Capability | Source coverage | Remaining consumer work |
| --- | --- | --- |
| Guest preparation | The [base module](../../../opentofu/modules/base/main.tf) declares packages, SSH, locales, optional timezone/mail, and maintenance timers. | Complete Suburban inputs and declare the module for the corresponding Vault guests. |
| Package lifecycle | [pveguest apt resources](https://github.com/agoodkind/terraform-provider-pveguest/blob/main/internal/provider/apt_packages_resource.go) support versions, upgrade restrictions, and opt-in removal. | Pin and use the implemented fields. |
| Docker transport | The [pveguest repository](https://github.com/agoodkind/terraform-provider-pveguest) supplies the guest API and Docker socket adapter. | Qualify the selected Docker provider with that adapter. |
| Service lifecycle | [Systemd resources](https://github.com/agoodkind/terraform-provider-pveguest/blob/main/internal/provider/systemd_unit_resource.go) manage active/enabled state and restart dependencies; destroy removes state only. | Add explicit removal behavior where required. |
| Controller execution | [Independent mutation execution](https://github.com/agoodkind/configsctl/blob/main/cmd/configsctl/tofumutation.go) selects locks from the plan. | Replace Ansible inventory/vault execution for migrated entrypoints. |

Source coverage does not establish the selected release, imported state, or deployment.

## Task S1: Complete resource selection and exact gap contracts

Track shared ownership under [LAB-89](https://tack.home.goodkind.io/browse/LAB-89), database/credential/protection gaps under LAB-92, and host/network gaps under LAB-93. Modify the capability table above and the consuming service task when the review establishes a gap.

The input is each current service operation. The output is one record per managed field: consumer, old writer, provider and immutable version, resource type, import identity, live reader, public plan fields, apply/removal behavior, interruption behavior, exact verification command, and owning task. Use one shared record for a capability consumed by both services.

- [ ] Compare the operation with existing Docker, database, OpenSearch, certificate, object-storage, and system providers. Record an exact unsupported operation instead of inferring support from a product name.
- [ ] Qualify the selected public interface against a real disposable dependency. Verify read-only refresh, readable changes, import, update, failure, and declared removal; pin the verified version.
- [ ] For a required extension, specify its typed schema, remote identity/reader, removal policy, and public acceptance assertions in the owning provider PR before implementation. The maintainer selects libraries within the approved behavior. Ask the user only about changes to service behavior, retention, access policy, or approved downtime.
- [ ] Commit capability records with the consuming declaration change. Release independent extensions separately; do not wait for unrelated consumers.

Completion requires a selected implementation and executable verification for every operation consumed by the next task.

## Task S2: Complete guest inputs, enrollment, and service removal

Track preparation under [LAB-90](https://tack.home.goodkind.io/browse/LAB-90) and enrollment under [LAB-95](https://tack.home.goodkind.io/browse/LAB-95).

Modify the [guest inputs](../../../opentofu/modules/guest-inputs/main.tf), Suburban roots [for Tack](../../../opentofu/suburban/tack-qa/main.tf) and [for MWAN](../../../opentofu/suburban/mwan/main.tf), and Vault roots [for Tack](../../../opentofu/vault/tack-prod/main.tf) and [for MWAN](../../../opentofu/vault/mwan/main.tf). Preserve their existing backends and addresses. The base interface consumes `node`, `vmid`, `kind`, key inputs, `timezone`, and optional `mail_relay`.

- [ ] Add missing Vault base declarations and explicit required timezones/mail inputs for both environments. Use `qemu` for gateway VMs and `lxc` for containers. Import existing mail identities and credential references without rotating them during adoption.
- [ ] Resolve locale authority between the package and declared file. Preserve the enabled locale, regenerate only when needed, and verify a clean plan after a real package refresh.
- [ ] Extend the existing systemd resource only where destruction must stop a unit: optional `remove_on_delete`, default `false`, and `RemoveOnDelete types.Bool` in its model. Implement `Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse)` with existing transport/status readers. When true, stop/disable the named unit and verify the result before state removal. Missing units already satisfy removal; transport or service failure returns a diagnostic. A separate file resource owns unit-file deletion.
- [ ] Add `TestAccSystemdRemovalPolicy` in a new `internal/acceptance/systemd_unit_removal_test.go` in the pveguest repository. Through real OpenTofu and a disposable unit, assert that the default preserves the unit, explicit removal stops/disables it, and an unreadable guest fails without falsely completing removal. Run `make testacc TESTARGS="-run TestAccSystemdRemovalPolicy"` with the existing `PVEGUEST_ACC_*` settings for the reserved test guest. Require execution without skipped cases and verify guest cleanup.
- [ ] Verify applicable AC1 through AC8 and AC11, including stopped-SSH repair, invalid SSH rejection, stable mail credentials, and secret-free state. Validate and plan each affected root using the coordination procedure.
- [ ] Commit/release the provider extension separately from Configs enrollment. Record actual Suburban and Vault adoption independently.

## Task S3: Qualify container, host, and protection resources

Track container support under [LAB-91](https://tack.home.goodkind.io/browse/LAB-91). Consume S1 contracts and each required S2 guest prerequisite. This task owns reusable provider implementation; service plans own application values and dependencies.

- [ ] Use existing Docker container/image/network/volume and Compose interfaces where supported. Verify remote field readback and import; project presence or a configuration hash does not prove container state. Preserve external volumes and unowned services.
- [ ] Verify a fresh guest before Docker exists and repair after guest management networking fails. Use pveguest prerequisites and the existing Docker socket adapter. Preserve endpoint identity/TLS verification when extending connection dialing.
- [ ] Use selected general resources for host files/units, sysctls, routes, VM communication arguments, and backup mounts. Verify saved and running state and actual API permissions. Do not silently introduce root SSH or treat a guest resource as a physical-host resource.
- [ ] Use general credential, certificate, database, and storage resources for supported operations. Preserve imported identities and secret-free state; declare rotation through references/version fields. Keep application compatibility and audited transitions with the service owner.
- [ ] Run public-interface acceptance for partial writes, interrupted recovery, and removal of only the declared object. Commit proven extensions/tests in the owning repository and pin immutable releases in Configs.

Completion requires the S1 contract and real evidence for the consuming operation; it does not require another generic framework.

## Task S4: Remove controller dependencies and integrate ordered deployment

Use the existing [queued deployment contracts](https://github.com/agoodkind/tofu-workflows/blob/6c5e5e807881cbf7275ed54fe3562a318b9da77d/docs/superpowers/plans/2026-10-22-deployment-contracts.md) and its runner-extraction task. That contract owns `tofurun.Request`, `tofurun.Result`, `hostlock.Scope`, explicit credential inputs, renewal timing, saved-plan identity, and recovery interfaces. Do not define parallel lease managers or queues.

Modify the existing configsctl callers for [secret preparation](https://github.com/agoodkind/configsctl/blob/main/cmd/configsctl/tofuenv.go) and [inventory resolution](https://github.com/agoodkind/configsctl/blob/main/internal/ansible/hosts.go), along with the mutation caller linked above. Preserve legacy callers for other services. Shared YAML may remain input; migrated commands must not execute Ansible inventory, playbook, or vault programs.

- [ ] Implement the specified public runner with explicit connection/credential inputs. Preserve one lock set across plan, exact-plan apply, checks, recovery, and cleanup. Consume application check/recovery interfaces instead of copying application logic into Configs.
- [ ] Run `TestCIRunsWithoutVaultOrInventory`, `TestCIEnvironmentIsExplicit`, `TestCISavedPlanAndStaleState`, `TestCIRequiresOwnedScope`, and cancellation/progress acceptance with the real dependencies and commands from the queued deployment plan. Verify equivalent local-command behavior.
- [ ] Complete the existing reusable workflow plan under LAB-141. Prove ordered exact-revision deployment, untrusted-PR isolation, compatible recovery, and retained data in its disposable consumer before enabling Tack automation.
- [ ] Trace supported Tack/MWAN install, configure, update, and recover entrypoints with Ansible executables unavailable. Require successful real Suburban lifecycle checks and correct locks/secrets. Source searches alone do not prove independence.
- [ ] Remove replaced preparation markers and command branches only after their service checks pass. Commit controller extraction, application integration, and workflow consumers as separate reviewable changes.

Tack runtime-audit readiness gates automatic Tack applies. Local declaration and adoption tasks continue while unrelated CI work remains unfinished.
