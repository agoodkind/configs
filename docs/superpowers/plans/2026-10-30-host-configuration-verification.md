# Host Configuration Verification Implementation Plan

> Use `subagent-driven-development` or `executing-plans` to implement the tasks and track steps with checkbox syntax.

**Goal:** Hosts install verified configuration from trusted builds and accept secret updates only through specific secret fields.

**Architecture:** Configs publishes complete nonsecret host files with GitHub artifact attestations. The host verifies each artifact before installation. The provider supplies secrets through a separate restricted interface.

**Tech Stack:** Reuse GitHub Actions attestations, go-makefile verification, OpenTofu, pveguest, the Proxmox overlay, and existing service configuration readers.

**Spec:** [Host deployment verification](../gueststate/spec.md#13-host-deployment-verification) defines the policy. This plan supplies the host verification portion of shared task S3 and MWAN task M3.

## Global constraints

Preserve the existing deployment locks, recovery protocol, environment identities, and production authorization. Keep generic verification and installation separate from service-specific secret consumers. Reuse existing implementations before adding code. Keep the broader provider reuse review independent from this plan.

Reuse the [independent workspace implementation](../specs/2026-10-23-workspace-separation-design.md) for feature and host ownership. Use its exact selected workspace, independent backend, credential selection, and lock decision for artifact publishing, installation, secret updates, and recovery. Shared code must not create shared deployment state.

## Review focus

- A verified archive contains altered extracted files: compare and install the exact verified member bytes.
- A valid build targets another environment: reject it before writing host state.
- An archive member attempts a path escape or symbolic-link write: reject it before replacing a destination.
- A secret contains configuration syntax: treat it only as a value and reject unsupported fields.
- Verification or installation fails: preserve the prior file and report the failing operation.

## Task 1: Qualify existing interfaces

Use the existing [shared capability selection task](2026-10-29-shared-deployment-migration.md#task-s1-complete-resource-selection-and-exact-gap-contracts). Record one checked contract for the producer, verifier, host installer, provider caller, and secret consumer before assigning their code changes.

The source interfaces to qualify are:

| Existing source | Qualification |
| --- | --- |
| [Shared archive attestation workflow](https://github.com/agoodkind/go-makefile/blob/main/.github/workflows/_package.yml) | Determine the smallest reusable workflow change needed to attest Configs output without compiling a Go binary. |
| [Release verification implementation](https://github.com/agoodkind/go-makefile/blob/main/selfupdate/release.go) | Reuse checksum and attestation checks. Determine the exact interface needed to verify the selected local archive at the host installation boundary. |
| [Host API overlay](https://github.com/agoodkind/proxmox-overlays/blob/main/patches/pve-manager.patch) | Establish the existing API integration and worker interface before adding verification or installation operations. |
| [Provider download implementation](https://github.com/agoodkind/terraform-provider-pveguest/blob/main/internal/provider/controller_fetch.go) | Reuse fetching and transfer behavior where the selected host interface permits it. |
| [MWAN host configuration](../../../mwan/config/config-host.toml.j2) | Separate actual secret consumers from nonsecret rendering. Check current runtime readers before preserving every template field. |

- [ ] Pin the inspected source revisions and complete the shared capability record for each interface. Include exact files, exported signatures, request and response fields, target identity, failure behavior, owning repository, and acceptance command.
- [ ] Bind each artifact and secret contract to the existing workspace selector and actual host identity. Reuse configsctl's selected-workspace and mutation-lock interfaces; do not create a second workspace discovery or lock implementation.
- [ ] Qualify the current Configs rendering path without Ansible executables or decrypted vault inputs. Reuse existing configuration projections and serialization; keep one source for each service setting.
- [ ] Select an artifact layout that binds complete file bytes, permitted destinations, file metadata, source revision, and environment. Use the existing archive and attestation formats. Keep this layout distinct from a new signature or approval protocol.
- [ ] Define the secret fields from real consumer reads. Include the existing MWAN `SMTP2GO_API_KEY` input where applicable. Record the exact authenticated caller, permitted consumer, value serialization, storage, rotation, and readback contract for each field.
- [ ] Resolve routine interface choices from source evidence. Ask for a user decision only if the result would change the approved policy or service behavior. Commit the checked contracts with the corresponding implementation PR.

Completion requires executable producer, verifier, installer, and consumer contracts. Later tasks consume these checked interfaces rather than inventing independent implementations.

## Task 2: Publish nonsecret host configuration

Modify the [Configs CI workflow](../../../.github/workflows/ci.yml) and the existing renderer selected in Task 1. Extend the shared publishing component only for a proven missing capability. Preserve the current lint job.

- [ ] Build complete host configuration and service files for each declared environment from the selected reviewed revision. Fail if required nonsecret inputs are missing.
- [ ] Publish the artifact and its GitHub attestation. Give publication permissions only to the trusted build job; untrusted pull requests cannot publish trusted output or read deployment secrets.
- [ ] Record the immutable source revision and artifact digest as deployment inputs. Preserve the existing saved-plan identity checks.
- [ ] Publish and pin each feature on each host independently. A change to one target must not advance another target's desired revision, artifact digest, or installed-version record merely because both use the same repository.
- [ ] Run the actual build with Ansible executables unavailable and without vault access. Inspect the produced archive using synthetic secret values only in the negative test; require the build to reject any attempted secret inclusion.
- [ ] Commit and review publishing separately from host activation.

Completion requires a real published artifact with verifiable provenance and complete nonsecret files for its declared environment.

## Task 3: Enforce verification at host installation

Modify the shared verifier and host overlay integration only where Task 1 proves a gap. The provider uses the selected API and existing transfer implementation. Keep destination restrictions on the host rather than trusting a provider request.

- [ ] Verify the selected artifact's bytes, repository, permitted workflow, source revision, and environment before modifying an installed file. Reuse the existing attestation verification library and trust checks.
- [ ] Require the artifact's workspace and host identity to match the selected deployment. Reject another feature on the same host and the same feature on another host before writing files. Apply and recover only the selected target's installed files and version record.
- [ ] Install only permitted archive members with the declared metadata. Reject path escapes, unexpected links, unsupported destinations, and bytes that differ from the verified member.
- [ ] Preserve atomic replacement and readable live state. Verification failure leaves the previous file intact. Import reads existing state without changing files or restarting services.
- [ ] Add public API acceptance cases using real attestations and a disposable host. Cover valid installation, altered archive bytes, altered extracted contents, a valid artifact for another environment, an untrusted workflow, and an invalid destination. Compare the installed file before and after every rejected request.
- [ ] Verify interrupted installation and retry through the actual host worker and provider. Preserve the existing deployment recovery and locking rules.
- [ ] Commit and review generic verification and installation separately from service-specific declarations.

Completion requires the real host API to enforce the policy independently of provider cooperation.

## Task 4: Connect restricted secret inputs and adopt

Use the secret contracts from Task 1, the existing credential source, and the selected provider's write-only inputs. Preserve authenticated Proxmox transport. Change service readers only when their existing inputs cannot satisfy the contract.

- [ ] Implement the checked secret-field interface. Accept only declared names and values for their permitted consumers. Serialize values with the consumer's parser or serializer rather than interpolating configuration syntax.
- [ ] Scope each secret update and resulting service action to the same selected feature and host. Reject requests for another target's secret field or consumer.
- [ ] Connect each service to its verified nonsecret configuration and separate secret input. Keep command arguments, unit contents, executable paths, and general environment settings outside the secret interface.
- [ ] Through real OpenTofu and a disposable service, rotate a synthetic credential and verify that the service uses it while the configuration artifact digest stays constant. Check state, plan output, host responses, and logs for the synthetic value.
- [ ] Attempt an unsupported secret name, configuration-fragment value, and extra destination. Require rejection without another setting or installed file changing.
- [ ] Adopt Suburban through the existing MWAN M5 procedure after acceptance. Adopt Vault within current production authorization after Suburban proof. Retire each matching Ansible writer only when its replacement passes acceptance.
- [ ] Verify clean repeat plans, a second apply without extra writes, secret rotation, service behavior, and released locks. Record source changes, releases, installed revisions, and runtime results separately.
- [ ] Prove AC21 through the public deployment interface with real isolated backends and disposable service dependencies. Select one feature on one host; make another feature on that host invalid and the selected feature on a second host depend on an unavailable service. Repeat artifact installation, selected secret rotation, and interruption followed by recovery with those same target identities.
- [ ] For every AC21 case, compare unrelated state bytes, artifact pins, installed-version records, installed files, and secret versions. Record service invocation identities and start times to detect restarts. Capture workspace-operation and state-lock events throughout execution, including acquisitions followed by release. Require the executor to avoid initializing, refreshing, planning, applying, or acquiring state locks for unrelated roots. Reuse existing independent-workspace acceptance; add only the host artifact and secret cases it does not cover.

Completion requires AC20, AC21, and the applicable existing service acceptance checks. Generic host capability remains under LAB-93; MWAN declarations remain under MWAN-586. Update their existing records when authorized rather than creating duplicate workstreams.

## Review separate pull requests

Review configuration publishing, generic host verification, and restricted secret integration separately. Publish any shared verifier extension before pinning its consumer. Service adoption depends on those accepted capabilities; unrelated guest and controller work retains its existing dependencies.
