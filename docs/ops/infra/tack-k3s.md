# Deploy Tack on k3s

Run these commands from clean, merged configs source. Preserve the existing
application containers, audit consumer, data services, proxy routes, and release
wiring until a separate cleanup change.

Use the migration targets below. A broad application-root apply can also adopt
existing deployment resources outside this migration. Inspect each plan before
applying it. Require unchanged repeat plans for each completed target.

## Bootstrap production

1. Enable the existing Vault local datastore's import content type.

   ```bash
   ./configsctl tofu vault/host plan -target=proxmox_storage_directory.local
   ./configsctl tofu vault/host apply -target=proxmox_storage_directory.local
   ```

2. Preview and create the VM with the bootstrap target. Require the plan and lock selection to use existing SSH endpoints only.
   Do not include guest-management resources for the new VM in this target.

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
5. Configure production connectors.

   ```bash
   ./configsctl tofu shared/cloudflare-zero-trust plan '-var=tack_k3s_environments=["prod"]' '-target=module.tack_cloudflared["prod"]'
   ./configsctl tofu shared/cloudflare-zero-trust apply '-var=tack_k3s_environments=["prod"]' '-target=module.tack_cloudflared["prod"]'
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
4. Require unchanged repeat plans with the production tunnel environment and the accepted
   hostname stage. Keep the currently accepted `tack_hostname_cutover` stage
   explicit on every unrelated DNS apply until private hostname acceptance.
   Use `legacy` before the first public cutover.

If a hostname check fails, plan and apply its preceding stage. Use `aliases` to
remove the explicit private CNAME, or `legacy` to restore all previous targets.
Preserve the production dedicated tunnel, the legacy tunnel, old containers, and proxy
routes during rollback.
