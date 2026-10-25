# PowerEdge iDRAC certificate implementation plan

**Goal:** Serve a trusted iDRAC certificate at the approved hostname and renew it daily from PowerEdge through its internal USB connection.

**Architecture:** A separate OpenTofu configuration creates the public DNS record, requests a certificate using iDRAC's signing request, and imports it with the existing REST provider. A persistent systemd timer runs the protected `configsctl` command on PowerEdge. An encrypted local state file and a dedicated encrypted vault belong only to this job.

**Tech Stack:** OpenTofu, Cloudflare DNS, the unchanged ACME provider, `magodo/restful` 0.25.2, `configsctl`, and systemd 257.

**Spec:** [iDRAC design](../specs/2026-10-25-idrac-certificate-design.md).

## Global constraints

Use the exact hostname `idrac.poweredge.management.collingwood.goodkind.io`.

Keep the iDRAC HTTPS private key inside iDRAC. Keep the ACME account key in encrypted state.

Use authenticated, verified TLS. Preserve the management network and existing VLANs.

Do not add provider forks, repository shell scripts, Ruby files, or a Configs test framework.

Do not copy the full repository vault or shared backend credentials to the job.

## Review focus

An interrupted certificate import must allow the next apply to install the already-issued certificate.

An unchanged certificate must not trigger a new signing request or import.

Initial factory-certificate verification must change to normal verification after installation.

A service restart or package update must preserve the encrypted certificate state and signing request.

A local deployment lock must use the actual host identity and serialize with existing deployments.

## Task 1: Support local host deployment locks

Extend Configsctl inventory selection to accept the actual operating-system hostname with `ansible_connection: local`. Use the existing local lock execution and preserve remote host selection.

- [x] Read the host target code and existing real lock tests.
- [x] Implement the local inventory selection without changing remote host selection.
- [x] Verify selection and actual local acquire, contention, and release using a temporary directory and the production lock implementation.
- [x] Run the applicable Configsctl checks and commit the change.

## Task 2: Declare the certificate configuration and runtime service

Define the public AAAA record, retained Redfish signing request, ACME registration, certificate, and direct `ImportSSLCertificate` operation in the [certificate workspace](../../../opentofu/poweredge/idrac-certificate). Enforce encryption for local state and plans. Use an ephemeral input for the iDRAC password and provider environment credentials for Cloudflare.

Provision the service, timer, and minimal runtime package through the [PowerEdge module](../../../opentofu/modules/poweredge/idrac-certificate). Include only the certificate workspace and its required inventory and settings. Use protected file references for the separate encrypted vault and its unlock credential.

- [x] Pin the provider and executable versions and retain dependency checksums.
- [x] Declare the certificate resources and the initial factory-certificate connection inputs.
- [x] Configure the persistent daily service with the local host deployment lock and only the job's vault.
- [x] Configure the USB interface and hostname resolution within the service.
- [x] Update the firmware client endpoint and adapter to use the selected hostname and system trust.
- [x] Run the final Configs source checks, inspect the complete diff, and commit the configuration.

## Task 3: Install and verify

- [x] Inspect iDRAC account slots and permissions through the verified factory-certificate connection.
- [x] Create the dedicated login, enable the USB connection, and verify the host-side connection without changing the management LAN.
- [x] Build the fixed Configsctl executable for Linux and install the pinned OpenTofu executable.
- [x] Provision the minimal runtime package and protected credentials through the existing Configs deployment path.
- [x] Inspect the certificate-only plan and apply it on PowerEdge.
- [x] Verify the AAAA answer against Cloudflare's authoritative servers and public DNS.
- [x] Verify the certificate hostname, chain, validity, and DER fingerprint through the management address.
- [x] Verify that the USB route uses the `idrac` interface and normal HTTPS verification succeeds through the USB address.
- [x] Remove the factory-certificate connection override and activate the daily timer.
- [x] Verify that the installed renewal service exits successfully and reports `0 added, 0 changed, 0 destroyed` with an exact leaf certificate match.
- [x] Confirm that the timer is enabled, active, and waiting with persistent scheduling and a 15-minute randomized delay.
- [ ] Open the focused pull requests, attach them, and report runtime proof and any remaining work.
