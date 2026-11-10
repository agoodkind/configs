# Deploy Tack on k3s

Run these commands from clean, merged configs source. Preserve the existing
application containers, audit consumer, data services, proxy routes, and release
wiring until a separate cleanup change.

Use the migration targets below. A broad application-root apply can also adopt
existing deployment resources outside this migration. Inspect each plan before
applying it. Require unchanged repeat plans for each completed target.

## Bootstrap QA

1. Enable cloud-image imports on the existing local datastore. Require the plan
   to preserve its path and existing content types and add only `import`.

   ```bash
   ./configsctl tofu suburban/host plan -target=proxmox_storage_directory.local
   ./configsctl tofu suburban/host apply -target=proxmox_storage_directory.local
   ```

2. Create the VM before applying its guest resources.

   ```bash
   ./configsctl tofu suburban/tack-qa plan -target=module.tack_k3s.proxmox_virtual_environment_vm.server
   ./configsctl tofu suburban/tack-qa apply -target=module.tack_k3s.proxmox_virtual_environment_vm.server
   ```

   This target also includes the new Kafka listener prerequisites on the existing
   owner guest. Require the plan and lock selection to use existing SSH endpoints
   only. Do not apply a bootstrap plan that includes guest-management resources
   for the new VM. The launcher acquires SSH locks before OpenTofu creates
   resources; a full module apply cannot create the VM that its own guest lock
   requires.

3. Verify the QEMU guest agent and root SSH access to the new VM. Read the
   [QA guest configuration](../../../opentofu/modules/suburban/tack-qa/configuration/k3s.tf)
   for the hypervisor, VM identity, and addresses. Confirm cloud-init completed
   and started the agent. Do not advance after a failed or incomplete bootstrap.

4. Apply k3s and the application after guest access succeeds.

   ```bash
   ./configsctl tofu suburban/tack-qa plan -target=module.tack_k3s
   ./configsctl tofu suburban/tack-qa apply -target=module.tack_k3s
   ./configsctl tofu suburban/tack-qa plan -target=module.tack_k3s -detailed-exitcode
   ```

## Validate QA

1. Verify the node, two ready application replicas, actual image identities,
   IPv6-only Pod and Service addresses, and IPv6 connections to every data
   service. Verify Kafka denies other sources on the new listener and retains
   its existing internal listener and data.
2. Run the existing remote QA workload against two distinct replica URLs.
   Verify authentication, writes, search, continuation and replay across
   replicas, authorization refusals, and permanent audit records for the actual
   workload organization.
3. Verify a rolling application update and native etcd snapshot creation and
   restoration on QA. Preserve the original server token during restoration.
   Require the node and application checks to pass after recovery.
4. Create only the QA tunnel and connectors after application acceptance.

   ```bash
   ./configsctl tofu shared/cloudflare-zero-trust plan '-var=tack_k3s_environments=["qa"]' '-target=module.tack_cloudflared["qa"]'
   ./configsctl tofu shared/cloudflare-zero-trust apply '-var=tack_k3s_environments=["qa"]' '-target=module.tack_cloudflared["qa"]'
   ```

   Keep the accepted environment set explicit on unrelated zero-trust applies
   until production connector acceptance.

5. Verify connector cold startup and reconnection with IPv4-only Internet access
   and with IPv6-only Internet access. Preserve internal IPv6 during both checks.
   Use production-like DNS resolvers and expired caches. Verify the origin
   resolves to the IPv6 Service and serves authenticated requests.

## Bootstrap production after QA acceptance

1. Enable the existing Vault local datastore's import content type.

   ```bash
   ./configsctl tofu vault/host plan -target=proxmox_storage_directory.local
   ./configsctl tofu vault/host apply -target=proxmox_storage_directory.local
   ```

2. Preview and create the VM with the bootstrap target. Apply the same existing
   endpoint and guest-resource checks as QA.

   ```bash
   ./configsctl tofu vault/tack-prod plan -target=module.tack_k3s.proxmox_virtual_environment_vm.server
   ./configsctl tofu vault/tack-prod apply -target=module.tack_k3s.proxmox_virtual_environment_vm.server
   ```

3. Verify the guest agent, completed cloud-init, and root SSH access using the
   [production guest configuration](../../../opentofu/modules/vault/tack-prod/configuration/k3s.tf).
   Apply the full application module only after those checks succeed.

   ```bash
   ./configsctl tofu vault/tack-prod plan -target=module.tack_k3s
   ./configsctl tofu vault/tack-prod apply -target=module.tack_k3s
   ./configsctl tofu vault/tack-prod plan -target=module.tack_k3s -detailed-exitcode
   ```

4. Verify both replicas, ready Service endpoints, actual image identities,
   authenticated reads, search, and permanent audit evidence. Preserve database
   roles, cluster identities, volumes, signing identities, and consumer offsets.
   Do not generate production test data or inject production failures.
5. Add production connectors while preserving QA.

   ```bash
   ./configsctl tofu shared/cloudflare-zero-trust plan '-var=tack_k3s_environments=["qa","prod"]' '-target=module.tack_cloudflared["prod"]'
   ./configsctl tofu shared/cloudflare-zero-trust apply '-var=tack_k3s_environments=["qa","prod"]' '-target=module.tack_cloudflared["prod"]'
   ```

## Switch hostnames after production acceptance

1. Preview and apply `tack_hostname_cutover=public` in `shared/cloudflare-dns`.
   Verify public requests, TLS, authenticated reads, search, and permanent audit
   evidence before advancing.
2. Preview and apply `tack_hostname_cutover=aliases` in that root. Verify every
   alias and confirm unrelated proxy applications still work.
3. Verify valid Cloudflare edge TLS for `tack.home.goodkind.io` and inspect
   applicable private DNS overrides before applying
   `tack_hostname_cutover=private`. Verify private and public resolution and
   authenticated requests after that stage.
4. Require unchanged repeat plans with both tunnel environments and the accepted
   hostname stage. Keep the currently accepted `tack_hostname_cutover` stage
   explicit on every unrelated DNS apply until private hostname acceptance.
   Use `legacy` before the first public cutover.

If a hostname check fails, plan and apply its preceding stage. Use `aliases` to
remove the explicit private CNAME, or `legacy` to restore all previous targets.
Preserve both dedicated tunnels, the legacy tunnel, old containers, and proxy
routes during rollback.
