# Retire Vault router VMs

The retirement plans delete VM101 and VM113, including their disks. Complete
their backups and verify current application backups before starting. Confirm
working serial console and out-of-band access, and keep both available through
the cutover and first reboot.

Run commands from the repository root. Initialize each selected workspace with
the [backend instructions](../../../opentofu/backend.md). Review fresh plans
before each apply. Plans must preserve every LXC and application IPv6 address,
and must not include unrelated ACME or permission changes, or changes to Suburban
or PowerEdge. Keep existing permissions and tokens until the network layout and
configuration pass verification.

1. Deploy the merged network layer before advancing to either retirement layer.
   Scope the host deployment to native guest network activation. Its dependencies
   include package installation, native configuration files, and the host
   interfaces file; this target excludes the unrelated ACME update.

   ```bash
   ./configsctl tofu vault/host plan -target=terraform_data.guest_network_activate
   ./configsctl tofu vault/host apply -target=terraform_data.guest_network_activate
   ```

2. Deploy the production resolver through its native OpenTofu root. This root
   owns CT103's BIND and upstream DNS configuration. The Ansible DNS64 play
   targets Suburban only.

   ```bash
   ./configsctl tofu vault/dns64 plan
   ./configsctl tofu vault/dns64 apply
   ```

3. Plan and apply each retained application root with `-target=module.vault`.
   This scope excludes unapplied guest preparation and SSH changes. For the
   initial Tack cutover, expect zero additions, six gateway-only updates, and
   zero deletions. Retain its application IPv6 addresses and Compose
   configuration. Apply the same scope to the other application roots:

   - `vault/debianct`
   - `vault/unifi`
   - `vault/mc`
   - `vault/proxy`
   - `vault/seaweedfs`

   ```bash
   ./configsctl tofu vault/tack-prod plan -target=module.vault
   ./configsctl tofu vault/tack-prod apply -target=module.vault
   ```

4. Verify the client network before retiring either router:

   - Confirm that CT103 resolves names and synthesizes DNS64 answers, and that
     IPv6-only guests can connect to IPv4-only services through NAT64.
   - Confirm ordinary guest IPv4 outbound access, application operation, and
     successful backup jobs.
   - Confirm private guest ingress and the separate out-of-band connection.

5. Advance to the merged OPNsense retirement layer. Require the router workspace
   plan to destroy only VM101. Then retire its host services and LAN virtual
   function. The host script refuses to proceed while VM101 still exists.

   ```bash
   ./configsctl tofu vault/opnsense plan
   ./configsctl tofu vault/opnsense apply
   ./configsctl tofu vault/host plan -target=terraform_data.retire_opnsense
   ./configsctl tofu vault/host apply -target=terraform_data.retire_opnsense
   ```

6. Advance to the merged MWAN retirement layer. Update CT124 in place, then
   apply the MWAN workspace. Its plan destroys only VM113, updates CT116 in
   place, and forgets two former DHCPv6 resource records. It must not destroy
   either retained container.

   ```bash
   ./configsctl tofu vault/sit6 plan
   ./configsctl tofu vault/sit6 apply
   ./configsctl tofu vault/mwan plan
   ./configsctl tofu vault/mwan apply
   ./configsctl tofu vault/host plan -target=terraform_data.retire_mwan
   ./configsctl tofu vault/host apply -target=terraform_data.retire_mwan
   ```

   The final host script refuses to proceed while VM113 exists or the transit
   bridge still has guest ports. Its file cleanup does not revoke credentials;
   handle exposed credential cleanup and rotation as separate work.

7. Apply native guest network activation again from the final merged source.
   This saves the host configuration without the obsolete transit bridge and
   virtual function declaration.

   ```bash
   ./configsctl tofu vault/host plan -target=terraform_data.guest_network_activate
   ./configsctl tofu vault/host apply -target=terraform_data.guest_network_activate
   ```

8. Retire the router-only Cloudflare tunnel and its three WireGuard routes.
   Require zero additions, zero updates, and exactly four deletions. Preserve
   public application ingress, the `home-vault` guest tunnel and its guest
   routes, and the separate out-of-band tunnel.

   ```bash
   ./configsctl tofu shared/cloudflare-zero-trust plan \
     -target=module.cloudflare.cloudflare_zero_trust_tunnel_cloudflared.router \
     -target=module.cloudflare.cloudflare_zero_trust_tunnel_cloudflared_route.router_wireguard
   ./configsctl tofu shared/cloudflare-zero-trust apply \
     -target=module.cloudflare.cloudflare_zero_trust_tunnel_cloudflared.router \
     -target=module.cloudflare.cloudflare_zero_trust_tunnel_cloudflared_route.router_wireguard
   ```

9. Reboot Vault with serial access available, then repeat the checks in step 4
   to verify the persistent guest network and ingress. The existing unattended
   firmware boot pause remains a separate unresolved issue; router retirement
   does not fix it.
