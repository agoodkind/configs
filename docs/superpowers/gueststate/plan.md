# Tack and MWAN Migration Implementation Plan

> Use `subagent-driven-development` or `executing-plans` to implement each task and record its verification with checkbox syntax.

**Goal:** OpenTofu must install, configure, update, and support recovery of Tack and MWAN on Suburban and Vault without invoking Ansible.

**Architecture:** The OpenTofu configuration in the Configs repository combines existing general providers with the shared guest module. Service providers implement only application rules. Each application owns its configuration root and state; shared host resources keep separate owners.

**Tech Stack:** The implementation uses OpenTofu, configsctl, Proxmox, pveguest, the Docker provider, and the Tack and MWAN providers.

**Spec:** The [migration contract](spec.md#12-tack-and-mwan-migration) and the [workspace separation design](../specs/2026-10-23-workspace-separation-design.md) define the required behavior and ownership.

## Global constraints

- Preserve the existing application roots and encrypted backends. Do not recreate the retired shared guest workspace.
- Reuse existing providers before extending them. Keep reusable implementation outside Configs; Configs declares values and dependencies.
- Separate provider implementation, root declarations, import, deployment, and live acceptance. Evidence for one does not prove another.
- Preserve guest identities, database identities, signing identities, disks, and data volumes during import.
- Assign one writer to each managed field. Retire only the matching Ansible writer after replacement acceptance.
- Develop Tack and MWAN independently. Give tertiary MWAN deployments priority when operations require the same host or guest.
- Preserve current production authorization and other owners' active locks. This plan does not authorize a production outage or change the PowerEdge assignment.
- Keep secrets absent from plans, state, logs, and public reports. Use the existing protected credential interfaces.
- Keep task ledgers and runtime receipts in private harness state. Publish durable requirements, source contracts, and completed acceptance references in the owning issue or PR.
- Preserve Ansible deployment of other services under LAB-96.

## Review focus

1. A feature exists in a provider but the application root does not declare it: track declaration and import as remaining work.
2. A guest loses management networking: bootstrap and repair use the existing Proxmox guest API.
3. A runner stops after a remote write: inspect actual state before retrying; preserve the same deployment lock until completion or deliberate abandonment.
4. A disabled feature leaves a service running: verify explicit stop, disable, and removal behavior, rather than deleting state records alone.
5. A test selects another ISP: require packet evidence on the intended simulator and verify restoration after the test.

## Read the implementation plans

Each workstream has one accountable role and its own task details:

| Workstream | Owner | Plan | Workstream issue |
| --- | --- | --- | --- |
| Shared provider capabilities, guest enrollment, and controller dependencies | Generic provider maintainer | [Shared implementation](../plans/2026-10-29-shared-deployment-migration.md) | [LAB-89](https://tack.home.goodkind.io/browse/LAB-89) |
| Tack service resources and adoption | Tack provider maintainer | [Tack implementation](../plans/2026-10-29-tack-deployment-migration.md) | [TACK-567](https://tack.home.goodkind.io/browse/TACK-567) |
| MWAN service resources, hosts, OPNsense, and simulators | MWAN provider maintainer | [MWAN implementation](../plans/2026-10-29-mwan-deployment-migration.md) | [MWAN-585](https://tack.home.goodkind.io/browse/MWAN-585) |
| Ordered CI deployment | Deployment workflow maintainer | [Existing queued deployment plan](https://github.com/agoodkind/tofu-workflows/blob/6c5e5e807881cbf7275ed54fe3562a318b9da77d/docs/superpowers/plans/2026-10-22-queued-deployment.md) | [LAB-141](https://tack.home.goodkind.io/browse/LAB-141) |

The detailed plans map individual tasks to their existing issues. The deployment coordinator assigns an actual engineer to each role before starting that role's task. Reuse these issues; create a new issue only when the capability review establishes an independent gap. Do not reopen completed package or Docker transport work. Issue status changes require current implementation and acceptance evidence.

## Sequence reviewable changes

1. Complete the shared capability map and use each service's source task coverage to identify exact gaps.
2. Release each necessary provider change in its own repository with its public-interface acceptance tests. Keep independent capabilities on sibling branches.
3. Declare the ready resources in the owning Configs root. A declaration PR consumes an immutable provider release; order dependent PRs after that release.
4. Adopt the corresponding Suburban resources and verify their service behavior. Tack adoption does not wait for MWAN adoption, or vice versa.
5. Adopt the corresponding Vault resources after the relevant Suburban checks pass and within existing production authorization.
6. Retire the replaced service writers and command dependencies. Enable automatic deployment only after its separate consumer prerequisites pass.

Runtime PR boundaries follow the task boundaries in the detailed plans. Keep provider changes separate from Configs adoption and from live deployment receipts. Use a dependent PR stack only when one source change requires another unmerged source change.

## Verify every adopted root

Use the application selector from the detailed plan, not the retired `guest` selector:

1. Run `./configsctl tofu <selector> validate`; require a valid configuration.
2. Run `./configsctl tofu <selector> plan -detailed-exitcode`; read exit 0 as unchanged, 2 as planned changes, and 1 as failure.
3. Review the exact resource identities and proposed changes before the authorized apply. Reject guest replacement or data destruction during adoption.
4. Apply with the existing controller and lock protocol. Run the service checks specified by the task.
5. Require a subsequent plan with exit 0 and a second apply that does not perform extra work.
6. Exercise drift repair and interrupted-operation recovery on Suburban. Preserve existing production failure-test authorization limits.

## Prove completion

The coordinator closes each workstream only when its task checks and the applicable specification criteria pass:

| Requirement | Implementation and evidence owner |
| --- | --- |
| Reuse, guest preparation, bootstrap, secret handling; AC1 through AC11 and AC16 where applicable | Shared tasks S1 through S4 |
| Tack runtime, databases, search, audit, protection, and adoption; AC17 | Tack tasks T1 through T5 and the approved Tack deployment specification |
| MWAN gateway, failover, hosts, observers, OPNsense, and simulators; AC12, AC13, AC18 | MWAN tasks M1 through M5 |
| Existing PowerEdge AC14 and AC15 obligations | Preserve the separate routing owner's evidence and authority; do not schedule that host through this plan |
| Writer retirement, controller independence, and all supported lifecycle commands; AC19 | Shared S4, Tack T5, and MWAN M5 |

Completion requires a manifest for each service and environment with the source revision, provider releases, state root, imported identities, former writer, passing task checks, final plan, recovery result, and reservation release. Keep secret values out of the manifest. A partial simulator migration or a successful application release does not establish full service migration.
