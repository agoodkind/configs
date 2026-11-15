# Deploy Tack on k3s

Run these commands from clean, merged configs source after authorizing QA
provisioning.

QA uses separate guests for the application, data services, and search.

Guest resources:

| Guest | VMID | Services | CPU cores | Memory | Disk | Storage |
| --- | --- | --- | --- | --- | --- | --- |
| Data LXC | 217 | FoundationDB, YugabyteDB, Kafka, audit-consumer | 2 | 6 GiB | 40 GiB | local-zfs |
| Search LXC | 227 | OpenSearch | 2 | 8 GiB | 40 GiB | local-zfs |
| k3s VM | 228 | One Tack application Pod and one cloudflared connector | 2 | 4 GiB | 40 GiB | local-zfs |

These allocations do not establish tested runtime minima. QA does not provision
backup guests, backup jobs, S3 resources, or k3s snapshots.

## Bootstrap QA

1. Reconcile stale state for the deleted QA guests before creating their
   replacements. Inspect the encrypted state and remove only records for deleted
   QA resources. The old application root still contains service records for
   guests 217, 220, 221, 222, 223, and 227 and VM 228. Do not refresh guest-managed
   resources while those guests do not exist. Follow the separate
   [SeaweedFS retirement procedure](../../../opentofu/suburban/seaweedfs/README.md)
   for deleted guest 218. Reconcile the deleted QA tunnel in the shared tunnel
   root without changing production tunnel records. Preserve encrypted state
   backups before every state edit.

2. Enable cloud-image imports on the existing local datastore. Require the plan
   to preserve its path and existing content types and add only `import`.

   ```bash
   ./configsctl tofu suburban/host plan -target=proxmox_storage_directory.local
   ./configsctl tofu suburban/host apply -target=proxmox_storage_directory.local
   ```

3. Create the data and search containers and the VM before applying guest
   resources. These targets lock the surviving hypervisor.

   ```bash
   ./configsctl tofu suburban/tack-qa plan -target=module.suburban
   ./configsctl tofu suburban/tack-qa apply -target=module.suburban
   ./configsctl tofu suburban/tack-qa plan -target=module.tack_k3s.proxmox_virtual_environment_vm.server
   ./configsctl tofu suburban/tack-qa apply -target=module.tack_k3s.proxmox_virtual_environment_vm.server
   ```

   Require bootstrap plans to exclude guest-management resources. The launcher
   acquires SSH locks before OpenTofu creates resources. A full module apply
   cannot create a guest that its own guest lock requires.

4. Verify root SSH access to both containers and the VM. Verify the QEMU guest
   agent on the VM. Read the
   [QA guest configuration](../../../opentofu/modules/suburban/tack-qa/configuration/k3s.tf)
   for the hypervisor, VM identity, and addresses. Confirm cloud-init completed
   and started the agent. Do not advance after a failed or incomplete bootstrap.

5. Apply the QA root after guest access succeeds. Supply the protected QA
   credentials from the encrypted vault. The data bootstrap configures an empty
   FoundationDB database, initializes SQL roles and migrations, provisions the
   search model and serving alias, and seeds the initial token-authenticated
   account. Successful bootstrap markers prevent a repeat apply from rerunning
   empty-ledger initialization.

   ```bash
   ./configsctl tofu suburban/tack-qa plan
   ./configsctl tofu suburban/tack-qa apply
   ./configsctl tofu suburban/tack-qa plan -detailed-exitcode
   ```

## Validate QA

1. Verify the node, one ready application replica, actual image identities,
   IPv6-only Pod and Service addresses, and IPv6 connections to every data
   service. Verify SQL certificate validation and OpenSearch certificate
   validation against the QA authorities.
2. Run the existing remote QA workload against the application URL.
   Verify API token authentication, writes, search, continuation and replay,
   authorization refusals, and permanent audit records for the actual
   workload organization.
3. Verify that an application update terminates the existing Pod before creating
   its replacement. Require at most one application Pod during the update.
4. Create only the QA tunnel and its single connector after application acceptance.

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
