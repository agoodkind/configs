# Independent infrastructure workspaces

Each independently deployed system has a root configuration, encrypted backend state, and state lock of its own. Reusable modules supply code without sharing state. This replaces the combined guest workspace that lets unrelated MWAN changes block Tack deployment checks.

## Ownership

An application environment owns its guest creation, guest disks, base guest setup, and application services. Tack QA and Tack production are separate environments. Each host's MWAN deployment owns its routing guests and services. PowerEdge's primary gateway, LAN container, and failover container stay together in one routing workspace.

A physical host workspace owns shared host networking, bridges, storage pools, overlays, and permission policies. Firmware has a separate workspace when it has an independent maintenance lifecycle. The application owns a guest's attachment to an allocated host interface; the host owns the interface and its allowed use. Exactly one resource owns each managed field.

A shared service owns resources used by several applications. Shared Cloudflare profiles and their registrations have one shared owner; a router workspace owns its local beacon service. Workspaces pass explicit nonsecret identifiers and capability requirements. An application may read its actual host prerequisites, but it never plans or applies another workspace to perform its deployment.

All currently managed resources participate in one coordinated migration. Existing unmanaged resources and unapplied feature additions do not become managed as a side effect. The migration changes ownership records, not running services or configuration. It must not recreate guests, restart services, rotate credentials, or apply unrelated changes.

## Workspace layout

Host folders organize the roots. The proposed root identifiers are `poweredge/host`, `poweredge/firmware`, `poweredge/routing`, `suburban/host`, `suburban/mwan`, `suburban/tack-qa`, `vault/host`, and `vault/tack-prod`. Other existing applications and shared services receive equivalent roots under their current host or a shared-service group. Independently deployed routing simulator scenarios receive their own roots instead of joining the gateway's state.

Each root declares its own backend key under the existing bucket. Keys use the root identifier plus `.tfstate`. Every current resource must appear once in the migration manifest, including resources that retain their existing workspace. The complete manifest derives from the actual source-state inventory and the frozen configuration revision, not names alone.

Existing backend encryption remains enforced. The migration reuses existing protected credential delivery and does not print state values, credential values, or decrypted snapshots. The release must reject duplicate backend keys before state writes.

## configsctl discovery and deployment checks

Recursive discovery identifies directories containing backend blocks. A workspace selector is its clean slash-separated path relative to `tofu.workspaces_dir`. Existing immediate-child selectors and the root workspace keep their current meaning. Discovery skips generated directories, hidden directories, and reusable `modules` directories. It does not follow directory symlinks. Invalid workspace HCL and unreadable discovered paths fail with their exact path.

Selection never accepts absolute paths, parent traversal, or an ambiguous basename. A nested selector that is not a discovered workspace fails before OpenTofu runs. A configured workspace path must resolve inside the configured workspace tree.

`tofu.guest_modules` maps workspace selectors to the module address indexed by the inventory's `service_name`. `tofu.lock_targets` already supplies the enrolled inventory hosts for each workspace through `guest_hosts`. Each selected enrolled host must have exactly one configured deployment workspace. Duplicate host ownership, missing required mappings, an empty module address, and unknown workspace selectors fail before a plan or deployment.

Legacy `tofu.guest_module` retains its current single-guest-workspace behavior during the tooling rollout. A configuration cannot set both keys. The coordinated source cutover replaces the legacy key with the complete new mapping; there is no silent fallback to the shared guest root.

The gate reads credentials and variables from each selected workspace, verifies its backend, and plans the selected guest module there. It does not initialize, parse, refresh, or lock unrelated application roots. It still refuses selected-guest drift and selected-plan errors. A missing or stopped enrolled guest must not become an unmanaged host that silently skips the gate.

## Locks and prerequisites

Backend locks protect their own states. Existing guest locks protect guest operations. Host-wide locks protect actual shared host mutations and firmware operations. A guest-local change with an exact enrolled guest identity must not acquire every hypervisor lock merely because another application uses the same host. Unknown ownership prevents mutation instead of granting access to another application's state.

The migration uses one executor. Infrastructure writers pause for the coordinated window, while application traffic and independent coding continue. A durable migration journal prevents ordinary deployment commands from using a partially transferred set. Stale or abandoned migration ownership requires explicit recovery; it does not silently clear a pause.

## Migration manifest and state transfer

The versioned manifest records source and destination workspace selectors, backend identities, the reviewed source commit, and a complete mapping of old address to new address. It records provider source and version expectations and the count and digest of each resource identity set. It contains no state attributes or secret values. Every destination root must validate before execution.

Preparation captures encrypted snapshots of all source states and checks their lineage and serial. Native OpenTofu state operations prepare each destination from private local copies. The implementation must preserve resource IDs, instances, provider-private fields, dependencies, and encryption. It must not reconstruct a state record from a resource name or use destroy/import as an ownership transfer.

The implementation first proves native encrypted state transfer and restoration using real temporary local backends. It uses a Go command in configsctl for the reviewed transfer, protected files, journal, checkpoints, and resume behavior. It adds no migration shell scripts and does not implement replacement encryption.

Before remote writes, the executor rechecks source revision, state serials, active apply owners, manifest digest, and the write pause. It refuses any mismatch. Each state write uses normal backend locking and lineage/serial protection. The ordinary path never uses force push or force unlock.

Remote backends do not provide one transaction across all states. The executor therefore keeps the pause active while it writes the whole reviewed set and verifies it. The journal records each confirmed write and its digest. Recovery compares actual backend state with the recorded expected states before continuing. A failure retains the pause and all encrypted backups. Recovery never repeats an uncertain write without checking the backend first.

Rollback restores the entire affected state set from the verified pre-migration snapshots under the same pause and executor. Recovery must account for the serial changes made by this migration without permitting an unrelated writer's state to be overwritten. Restoration follows the same reviewed manifest and refuses unexpected identities or serials.

The source and destination configuration cutover uses the reviewed release together with the state transfer. The executor retains the old configuration and snapshots for recovery. No ordinary apply runs during transfer. The write pause ends only after every root passes ownership, identity, and planning verification.

## Acceptance

Use real OpenTofu local backends and the existing configsctl CLI tests. Do not use mocked processes or production fixtures.

1. Nested roots with repeated leaf names select the exact requested root. A backendless shared module is not selectable.
2. Generated provider directories and hidden configuration do not become deployment workspaces. Traversal, symlink escapes, and malformed selected roots fail before OpenTofu runs.
3. A valid Tack fixture deploys while an unrelated MWAN root has an unapplied address move or an unavailable dependency. Its state bytes and backend lock remain unchanged.
4. A changed or unreadable Tack guest still blocks its deployment. Multiple selected applications check only their own roots. Duplicate and missing guest ownership fail explicitly.
5. A selected root receives only its declared provider variables plus the existing backend environment. Tests use distinct synthetic values and verify exclusion without printing values.
6. A stopped enrolled guest remains a deployment failure after the workspace split. Legacy fixtures continue to pass while the new configuration rolls out.
7. A real encrypted state split preserves each resource identity and all provider state. Native plans against the destination roots introduce no infrastructure actions. Preexisting unapplied changes remain separate from the migration.
8. A deliberately interrupted transfer retains the pause and resumes from verified backend state. A changed source serial, altered manifest, duplicate destination ownership, or unexpected destination state refuses execution.
9. An injected interruption followed by restoration recovers the complete source set. State and logs do not expose the synthetic secret marker. No new resource is created and no existing resource is destroyed.
10. Live cutover accounts for every managed source resource exactly once. Tack and MWAN plan independently afterward. Deployment checks retain their actual host prerequisites without planning another system.

## Execution ownership

The PowerEdge chat finishes its approved functional changes before the coordinated migration. Deferred host organization commit `9ea98388` is a reference, not an automatically accepted source baseline. This task owns the eventual complete source split, configsctl changes, independent review, and sole state transfer. Audit and parent-integrity work continue independently.
