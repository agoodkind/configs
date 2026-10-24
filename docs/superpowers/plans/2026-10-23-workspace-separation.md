# Workspace separation implementation plan

> Use `executing-plans` to implement these tasks. Track steps with checkbox syntax.

**Goal:** Each application environment and physical host can plan and deploy independently while preserving existing resource identities.

**Architecture:** Configs declares independent nested roots and backend keys. configsctl selects a workspace by relative path and checks each guest in its owning workspace. One manifest and one executor transfer the complete state set during a coordinated write pause.

**Tech stack:** Go, OpenTofu, existing encrypted R2 backends, existing providers, Ansible deployment entrypoints, and existing CLI integration tests.

**Spec:** [Independent infrastructure workspaces](../specs/2026-10-23-workspace-separation-design.md).

## Global constraints

- Preserve live identities and unmanaged fields. Do not apply pending feature changes during migration.
- Give every root a distinct backend key and every managed field exactly one writer.
- Preserve native encryption and protected credential delivery. Exclude attributes and credentials from visible output and Git.
- Use the existing Go tooling and real OpenTofu backends. Add no shell scripts or replacement test framework.
- Complete the current PowerEdge functional work before freezing the migration baseline. Preserve its deferred organization as review input.
- Reject the address-only workaround. Do not disable the deployment check or bypass a failed plan.

## Review focus

- An unrelated root can have invalid HCL or moved resources. Selecting Tack must not parse or plan that root.
- Repeated leaf names require full selectors. Traversal and directory symlinks must not select outside roots.
- Duplicate or missing guest ownership must fail explicitly. Preserve the absent-enrolled-guest refusal.
- An acknowledged state write can precede a crash. Recovery must compare actual serials and digests.
- A state can change after preparation. Refuse stale transfer evidence and retain the pause during recovery.

## Task 1: Discover and select nested roots

**Files:** Modify `cmd/configsctl/tofuenv.go`; create `cmd/configsctl/tofuworkspaces.go`, `cmd/configsctl/tofusettings.go`, and `cmd/configsctl/tofu_nested_test.go` in configsctl. Put native HCL fixtures under `cmd/configsctl/testdata/workspaces`.

**Interfaces:** Preserve `selectTofuWorkspace(workspacesDir string, args []string) (string, []string, error)` and `tofuChildWorkspaces(workspacesDir string) ([]string, error)`. Direct selection checks only its requested root. Existing immediate-child selectors and root-command behavior remain available.

- [ ] Extract settings and selection from the oversized mixed file.
- [ ] Recursively discover backend roots; skip hidden paths, generated provider directories, `modules`, and directory symlinks. Return sorted relative selectors.
- [ ] Reject absolute, nonclean, traversal, and symlink selectors before invocation. A missing nested root must fail directly.
- [ ] Add CLI tests with real local backends for nested roots, repeated leaf names, an unrelated invalid root, traversal, and backendless modules. Inspect real state and output.
- [ ] Run the focused tests and required gates, then commit.

## Task 2: Check guests in their owning workspace

**Files:** Modify `cmd/configsctl/plangate.go`, `cmd/configsctl/absentguests.go`, and `cmd/configsctl/tofusettings.go`. Create `cmd/configsctl/guestplans.go` and `cmd/configsctl/deploy_workspace_test.go`.

**Interfaces:** Add `GuestModules map[string]string`, YAML key `tofu.guest_modules`, to `tofuSettings`. Keys are workspace selectors and values are module addresses indexed by inventory `service_name`. Reuse workspace-keyed `LockTargets.GuestHosts`. Keep explicit legacy `GuestModule` during rollout and reject simultaneous use. Add workspace and module identity to `gatedGuest`.

- [ ] Validate selector syntax, module values, required host maps, and unique inventory-host ownership.
- [ ] Prepare each selected guest request from its root and declared variables. Check only its backend. Refuse a missing required workspace mapping.
- [ ] Aggregate enrolled hosts for absent-guest validation. Preserve legacy behavior only under its explicit legacy field.
- [ ] Add a real CLI regression with independent Tack and MWAN roots. Leave MWAN in an unapplied address-move state; require a successful Tack play and unchanged MWAN state. A changed Tack declaration must still refuse deployment.
- [ ] Cover multiple selected roots, duplicate and missing mappings, synthetic variable isolation, and stopped enrolled guests.
- [ ] Run focused tests and required gates, update the command reference, and commit.

## Task 3: Scope locks to actual effects

**Files:** Review `cmd/configsctl/hostlocks.go`, `internal/tofulock/targets.go`, and `internal/tofulock/decide.go`. Add CLI and real host-lock regressions where behavior changes.

**Interfaces:** Preserve existing backend and guest lock interfaces. Exact guest-local changes require that guest's lock; shared host and firmware changes require host locks. Unknown ownership refuses mutation in independent-workspace mode.

- [ ] Compare current decisions with the new ownership map before changing them.
- [ ] Prove guest-local operations do not lock unrelated applications or every hypervisor.
- [ ] Prove conflicting guest operations serialize and real host changes still obtain host locks.
- [ ] Run the relevant gates and commit.

## Task 4: Implement native state transfer and recovery

**Files:** Add focused units under `internal/statemigration` for manifests, private files, native commands, journals, transfer, verification, and recovery. Add the `configsctl migrate-state` entrypoint and its real CLI tests in the tooling repository.

**Interfaces:** A version-1 manifest records the reviewed configuration commit, selectors, backend identities, expected source lineage/serial, and old-to-new addresses without attributes. CLI actions prepare, verify, execute, resume, and restore consume that manifest and a private working directory. Durable checkpoints contain identities, counts, and digests only.

- [ ] Prove native encrypted local state transfer and restoration with real OpenTofu before remote mutation. Preserve provider-private fields and use native encryption.
- [ ] Implement complete mapping checks, snapshots, serial checks, one-executor ownership, and the durable deployment pause.
- [ ] Prepare every destination before remote writes. Reject duplicate keys, absent source records, stale commits, and unexpected destination state.
- [ ] Write the complete set with native backend locks. Compare actual state after each write; retain the pause after failures or uncertain results.
- [ ] Resume or restore the complete set only after verifying expected migration-owned state. Do not overwrite unrelated writes or force unlock active owners.
- [ ] Interrupt real local transfers at write boundaries and prove recovery, identity preservation, and synthetic-secret exclusion.
- [ ] Run required gates, obtain independent review, and commit.

## Task 5: Declare the complete source split

**Files:** Update Configs roots, backend entrypoints, encryption declarations, and `configsctl.yml`. Reuse reviewed shared modules. Consolidate the old guest-workspace contract into the canonical design.

**Interfaces:** Consume the tested configsctl release. Produce the complete root/backend map and reviewed resource migration manifest. Preserve approved routing groups and firmware ownership.

- [ ] Freeze the actual source baseline after the PowerEdge functional work completes. Inventory all states through supported tooling without displaying attributes.
- [ ] Assign every existing managed resource once under the approved application, host, and shared-service rules. Separate independently deployed simulator scenarios.
- [ ] Declare explicit nonsecret prerequisite inputs; application deployment must not read another root's full state or invoke its plan.
- [ ] Validate backend-key uniqueness and field ownership. Compare prepared plans with the unchanged source snapshot.
- [ ] Review and commit the complete cutover source and manifest together; do not migrate a subset.

## Task 6: Execute one coordinated migration

- [ ] Confirm active owners and pause infrastructure writers while services and independent coding continue.
- [ ] Save final encrypted snapshots and verify commit, manifest, identities, counts, serials, and destination expectations.
- [ ] Transfer the complete set with one executor. Apply no runtime plan during transfer.
- [ ] Verify identity preservation, unique ownership, destination locks, and independent plans. Distinguish prior pending changes from migration-caused actions.
- [ ] Exercise supported Tack and MWAN deployment checks independently, including host, firmware, and shared-service roots.
- [ ] Release the pause only after every root verifies. Retain recovery evidence and report exact commits and live results.
