# Tack Deployment Migration Implementation Plan

> Use `subagent-driven-development` or `executing-plans` to implement the tasks and track steps with checkbox syntax.

**Goal:** OpenTofu must manage the complete Tack QA and production deployment and preserve existing data and identities.

**Architecture:** The OpenTofu configuration declares generic runtime resources and dependencies in the existing Tack roots. The Tack provider manages application-specific state and audited transitions. Its existing schema resource manages migration versions.

**Tech Stack:** The implementation uses OpenTofu, Proxmox, pveguest, Docker/Compose, FoundationDB, YugabyteDB, OpenSearch, Kafka, and the Tack provider.

**Spec:** The [approved Tack deployment specification](https://github.com/agoodkind/tack/blob/main/docs/superpowers/specs/2026-10-10-tack-provider.md) and the [coordination plan](../gueststate/plan.md) define the required behavior and ownership.

## Global constraints

Apply every coordination-plan constraint and the specification's audit, backup, search, and one-writer requirements. Keep general database administration generic. Production import preserves cluster identities and data volumes without fresh-cluster initialization. Automatic deployment retains its runtime-audit prerequisite.

## Review focus

- An imported container uses an existing volume: preserve its identity and data during image updates.
- Startup also writes provider-owned schema/index state: retire that writer before adoption.
- One database member fails readiness: stop replacement before changing another member.
- An interrupted migration completes remotely: read its actual version before retrying.
- A credential, certificate, or signer already exists: import without generating another identity.

## Reuse the existing boundary

The [Suburban root](../../../opentofu/suburban/tack-qa/main.tf) declares six guests and base modules. The [Vault root](../../../opentofu/vault/tack-prod/main.tf) declares guests without the base module; shared S2 owns enrollment. Neither root declares application runtime resources or `tack_schema`.

The [Tack provider](https://github.com/agoodkind/tack/blob/main/provider/internal/tackprovider/provider.go) registers `tack_schema`. Existing import/acceptance code observes migration versions. Verify the pinned release and declare/import it instead of rebuilding the resource.

Track T1 through T4 under [TACK-568](https://tack.home.goodkind.io/browse/TACK-568) and [TACK-569](https://tack.home.goodkind.io/browse/TACK-569), QA adoption under [TACK-570](https://tack.home.goodkind.io/browse/TACK-570), retirement under [TACK-572](https://tack.home.goodkind.io/browse/TACK-572), and production under [TACK-573](https://tack.home.goodkind.io/browse/TACK-573). Keep TACK-571 cancelled.

## Task T1: Declare engine installation and runtime objects

Consume the shared S1 through S3 capabilities this task needs. Create the reusable Tack module with focused package/runtime/protection declarations. Its typed inputs are guest identity, immutable release/image identity, public configuration, protected secret references, and environment placement; its outputs are managed service identities and readiness targets. Declare these interfaces in the capability record before implementation.

Create `packages.tf`, `runtime.tf`, `variables.tf`, and `outputs.tf` under the existing [modules directory](../../../opentofu/modules) in a new `tack` module. Modify the two roots above, their provider declarations/pins, and the [current deployment](../../../ansible/playbooks/deploy-tack.yml) only when retiring an accepted writer. Read the [search-node tasks](../../../ansible/playbooks/tasks/tack-search-node.yml) for distributed search behavior.

- [ ] Declare Docker package versions/upgrade policy, daemon settings, forwarding/NDP settings, prune timers, release downloads, and unit dependencies with general resources.
- [ ] Declare the environment's application, consumer, FoundationDB, YugabyteDB, OpenSearch, and Kafka runtime state: exact image digests, placement, networks, volumes, mounts, ports, configuration, and readiness. Preserve independent application replicas.
- [ ] Verify readable nonsecret plan differences and real remote readback. Extend existing Compose/provider libraries through S1 only for demonstrated gaps; preserve their parsing and convergence implementation.
- [ ] Import QA runtime objects/volumes. Verify image replacement without volume deletion, configuration drift repair, and a repeated apply without work.
- [ ] Commit the module/QA declarations as a focused Configs PR. Add corresponding Vault declarations without applying them before T5.

## Task T2: Declare identities, access, and database administration

Consume T1 identities and S1 credential/certificate/database contracts. Add focused typed declarations to the Tack module; general providers own general operations. Read [service secrets](../../../ansible/playbooks/tasks/tack-service-secrets.yml), [ledger TLS](../../../ansible/playbooks/tasks/tack-ledger-tls.yml), and [database-secret delivery](../../../ansible/playbooks/tasks/tack-database-secret-runtime.yml). The current deployment's database/ledger sections supply cluster membership, placement, redundancy, roles, and grants.

- [ ] Import credential references, certificates, signer identities/trust, database identities, and existing FoundationDB backup-session identities. Keep private values absent from state/plans and expose rotation through public references/version fields.
- [ ] Declare FoundationDB membership/coordinators/redundancy and YugabyteDB membership/placement/settings/roles/grants with S1's selected general resources. Preserve production's prohibition on fresh FoundationDB initialization. Authenticate reads before creating an absent object on a fresh QA installation.
- [ ] Preserve required Kafka topics, retention, consumers, and the [queue firewall](../../../ansible/playbooks/tasks/tack-queue-firewall.yml). Keep external listeners limited to their current environments; verify loaded rules as well as files.
- [ ] Use existing audited Tack interfaces for application policy. Reads distinguish absent state from denied/unavailable state and do not initialize stores or rotate credentials.
- [ ] Verify import, rotation, membership change, partial-write failure, and interrupted recovery against real QA engines. Assert unchanged cluster IDs and persistent data. Commit general provider work separately from Tack policy and Configs declarations.

## Task T3: Adopt schema migrations and coordinated search changes

Consume the required T1/T2 resources. Modify the [schema resource](https://github.com/agoodkind/tack/blob/main/provider/internal/tackprovider/schema_resource.go) only for a demonstrated gap. Add its declarations with the actual import identity and desired migration version; use existing operator/audit interfaces.

Read [search security tasks](../../../ansible/playbooks/tasks/tack-search-security.yml) and the search requirements linked by the Tack specification. General resources own supported search members/models/mappings/security. Tack owns coordinated copying, alias changes, and retirement where application rules require them.

- [ ] From the Tack provider module, run `env TF_ACC=1 go test ./internal/tackprovider -run '^TestSchemaImportPlanAndRefresh$' -count=1 -v`. Supply the existing `TACK_ACC_*` QA settings and token-file reference. Require an executed passing test, the observed installed version, zero pending migrations at the imported target, and an unchanged plan. A skipped acceptance test does not satisfy this task.
- [ ] Transfer migration ownership from `ops provision` and startup to `tack_schema` after compatibility checks; retain read-only verification. Verify a real pending migration, interruption after remote completion, failed migration, and prohibited downgrade through the public lifecycle.
- [ ] Implement only the justified application-specific search contract from S1. Plans show model/mapping changes, copying, alias switching, and retirement. Read the actual phase after interruption and resume without deleting the serving index or recreating finished work.
- [ ] Verify read-only planning, real drift, failed copying, interruption before/after alias switching, and preserved serving search with real-engine acceptance. Run the owning repository's test targets and record exact tested revisions.
- [ ] Commit schema adoption separately from search lifecycle work. Search activation waits for its own checks and does not block independent runtime/backup adoption.

## Task T4: Declare protection, mounts, and coordinated updates

Consume T1 identities and the relevant general capabilities. Read current [archive](../../../ansible/playbooks/tasks/tack-ledger-archive.yml), [export](../../../ansible/playbooks/tasks/tack-ledger-export.yml), [retention](../../../ansible/playbooks/tasks/tack-backup-retention.yml), [staleness](../../../ansible/playbooks/tasks/tack-backup-staleness.yml), and [restore rehearsal](../../../ansible/playbooks/tasks/tack-backup-restore-drill.yml) operations.

- [ ] Declare backup destinations, continuous backup, archive/export schedules, retention, alarms, and restore schedules. General storage/systemd resources install these; Tack keeps audited backup/recovery decisions. Preserve existing configured retention and timeout policy.
- [ ] Replace imperative backup mounts and ignored mount changes with S1's qualified Proxmox capability. Read the [QA](../../../opentofu/modules/suburban/tack-qa/main.tf) and [production](../../../opentofu/modules/vault/tack-prod/main.tf) declarations. Import live mount identity and prove an update without guest replacement or backup-data deletion.
- [ ] Preserve readiness-aware member updates with existing Compose/engine operations and Tack health checks. Replace one member, verify service/quorum recovery, then permit another. Engine concurrency alone does not prove readiness.
- [ ] Run a complete QA backup/restore rehearsal on actual data and verify restored records, retention, and stale-backup alarms. A failed member recovery must stop the coordinator before another replacement.
- [ ] Commit protection, mount support, and coordinated updates in independent PRs where their source dependencies permit.

## Task T5: Adopt QA and production, then retire writers

Consume ready groups from T1 through T4 and the controller path from shared S4. The selectors are `suburban/tack-qa` and `vault/tack-prod`. Independent resource groups do not wait for unrelated groups.

- [ ] Build the coordination plan's environment manifest. Import/review each group's exact identities before the first provider write; reject guest/cluster/signer/data-volume replacement and retire only its competing Ansible/startup fields.
- [ ] Run the coordination root commands and the Tack specification's fresh-install/import/drift/interruption cases on QA. Require actual images, schema, database membership, search, backup recovery, and audit evidence for the declared revision.
- [ ] Adopt the corresponding production group after its QA checks and within current authorization. Verify installed state and a clean follow-up plan; do not run an unapproved production failure drill.
- [ ] Verify install/configure/update/recover entrypoints with Ansible unavailable while other services retain their deployment. Enable automatic deployment only after the separate workflow/runtime-audit prerequisites pass.
- [ ] Commit writer retirement with accepted replacements and evidence. Close TACK-567 only after the full specification and AC17/AC19 pass for both environments.
