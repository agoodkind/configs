# Tack and MWAN implementation plan

Remove Ansible from [Tack deployment](https://tack.home.goodkind.io/browse/TACK-567)
and [MWAN deployment](https://tack.home.goodkind.io/browse/MWAN-585). Use reusable
OpenTofu resources under the [deployment contract](spec.md#12-tack-and-mwan-migration).
Develop both services together. Each task waits only for the capabilities it needs.

## Review shared capabilities

The generic provider maintainer owns one capability list under [LAB-89](https://tack.home.goodkind.io/browse/LAB-89).
The deployment maintainer inventories existing behavior and combines the
selected resources in Configs. Service provider maintainers implement the
application-specific rules that pass the reuse review.

For each capability, record the source operation, consumers, proposed provider
and version, supporting contract, exact gap, implementation owner, dependencies,
next action, and completion check. Record how the resource reads live state,
plans changes, applies them, performs declared removal, and recovers from
interruption.
Distinguish an existing implementation from one that still needs development.

The source tasks establish required behavior. Their current grouping does not
determine provider ownership.

Docker connection selection must account for initial setup and recovery.
Verify a fresh guest before Docker and its management endpoint exist. Verify
repair after guest management networking becomes unavailable. Use the existing
Proxmox guest API for prerequisite setup and repair. A Docker network endpoint
cannot be the only management path. Preserve the generic Docker resource
model and prove the exact connection gap before implementing an adapter.
These checks use existing real test guests; they do not require new PowerEdge
simulators.

| Work | Tickets | Owner | Concrete action | Completion check |
| --- | --- | --- | --- | --- |
| Guest preparation | [LAB-90](https://tack.home.goodkind.io/browse/LAB-90) | Generic provider maintainer | Reuse the [base module](../../../opentofu/guest/base/main.tf). Check the provider contract for package versions, upgrade restrictions, repositories, users, directories, file differences, and removal behavior before proposing extensions. | Each required operation has an existing resource or a verified gap. Both services use the same implementation where they require that operation. |
| Containers and images | [LAB-91](https://tack.home.goodkind.io/browse/LAB-91) | Generic provider maintainer | Evaluate the Docker resources cited below for container state, image identity, downloads, authentication, health checks, and import. Verify the required connection method and access permissions. Treat Compose configuration separately from application rules. | Record supported fields and exact gaps. Prove live reads, adoption, and image updates on a disposable guest before selecting the implementation. |
| Databases, credentials, certificates, and protection | [LAB-92](https://tack.home.goodkind.io/browse/LAB-92) | Generic provider maintainer with service maintainers | Review existing providers for database administration, credential lifecycle, certificates, backup storage, schedules, and snapshots. Separate each reusable operation from application compatibility or recovery rules. | Each operation has a provider contract and acceptance evidence, or a specific reusable extension task. Preserve the service's audit and data protection requirements. |
| Host and network configuration | [LAB-93](https://tack.home.goodkind.io/browse/LAB-93) | Generic provider maintainer | Inventory live host settings and routes in [testbed deployment](../../../ansible/playbooks/deploy-testbed.yml) and [host deployment](../../../ansible/playbooks/deploy-proxmox.yml). Select generic host, network, and Proxmox resources, including narrowly scoped API extensions where required. | Planning detects changes to running settings and routes. Apply restores declared state through the approved transport and permissions. A configuration file alone does not prove live state. |
| Simulator behavior | [LAB-94](https://tack.home.goodkind.io/browse/LAB-94) | Deployment maintainer | Reuse the existing guest work. Convert [ISP configuration](../../../ansible/playbooks/tasks/deploy-testbed-isp-lxc.yml) and [routing simulator configuration](../../../ansible/playbooks/tasks/deploy-routing-simulators.yml) into package, file, service, and network declarations. | Simulators assign the expected addresses, install return routes, and pass their traffic checks. A second apply does not perform extra work. |
| Tack rules | [TACK-568](https://tack.home.goodkind.io/browse/TACK-568), [TACK-569](https://tack.home.goodkind.io/browse/TACK-569) | Tack provider maintainer | Compare [Tack deployment](../../../ansible/playbooks/deploy-tack.yml) with the approved Tack design. Implement only the justified application rules after identifying generic resources for the other operations. | Each custom resource has the specification's justification and passes Tack acceptance through its public OpenTofu interface. |
| MWAN rules | [MWAN-586](https://tack.home.goodkind.io/browse/MWAN-586) | MWAN provider maintainer | Compare [gateway deployment](../../../ansible/playbooks/deploy-mwan.yml), [failover deployment](../../../ansible/playbooks/deploy-mwan-failover.yml), and [OPNsense deployment](../../../ansible/playbooks/deploy-opnsense.yml) with the MWAN contract. Separate generic installation and host operations from MWAN network and deployment health rules. | Each custom resource has a justified state contract. MWAN acceptance includes failover, observers, watchdog recovery, and required OPNsense behavior. |

## Implement and adopt resources

1. **Assign owners and complete the reuse review.** The deployment maintainer
   confirms the shared owner and both service owners. Recover the existing
   MWAN provider assignment before starting a competing implementation. If
   that owner cannot resume, present two options: assign a dedicated MWAN
   provider maintainer, or give that work to the shared maintainer. Recommend
   a dedicated maintainer to preserve parallel work. Completion requires a
   named owner for each implementation task and a capability list that
   accounts for both services. Independent work continues during assignment.
2. **Implement each verified capability gap.** The appropriate provider
   maintainer adds only the missing behavior and updates that provider's
   public contract. New runtime behavior requires acceptance through
   OpenTofu against real disposable dependencies. Verify read-only planning,
   visible changes, drift repair, import, declared removal, interruption
   recovery, and secret handling. Completion requires a released provider
   that passes the checks for its consumers. Release independent changes
   separately; neither service waits for the entire generic backlog.
3. **Compose each ready part of the deployment.** The deployment maintainer
   updates the [guest declarations](../../../opentofu/guest/main.tf), the
   [MWAN module](../../../opentofu/guest/mwan/main.tf), and the
   [PowerEdge dependencies](../../../opentofu/guest/poweredge.tf) where the
   selected resources apply. Keep shared configuration reusable and declare
   actual dependencies. Track remaining guest enrollment under [LAB-95](https://tack.home.goodkind.io/browse/LAB-95)
   without blocking independent service work. Run
   `./configsctl tofu guest validate`, then review
   `./configsctl tofu guest plan -detailed-exitcode` in the authorized
   environment. Exit 0 means zero changes; exit 2 requires review of the
   proposed changes; exit 1 reports an error. Completion requires a valid
   configuration and a reviewed plan for the exact state under adoption.
4. **Adopt existing objects without competing writers.** The deployment
   maintainer imports live objects and compares their identities and data
   volumes before changing writer ownership. Stop the matching Ansible
   writer before the provider's first write. Complete
   [Tack QA adoption](https://tack.home.goodkind.io/browse/TACK-570) or
   [MWAN test environment adoption](https://tack.home.goodkind.io/browse/MWAN-587)
   for the applicable resource group. Adopt production under
   [TACK-573](https://tack.home.goodkind.io/browse/TACK-573) and
   [MWAN-588](https://tack.home.goodkind.io/browse/MWAN-588) within the existing
   authorization and coordination requirements. Completion requires
   the applicable service acceptance, preserved data, a clean subsequent
   plan, and an idempotent second apply. One service's adoption does not
   require completion of the other service.
5. **Retire the remaining Ansible dependencies.** The deployment maintainer
   checks service commands, shared inventory, locks, and controller paths.
   Complete Tack's writer and command-path retirement under
   [TACK-572](https://tack.home.goodkind.io/browse/TACK-572).
   Remove replaced tasks only after their replacement passes acceptance.
   Preserve deployments of other services. Completion requires execution
   evidence for installation, configuration, update, and recovery without
   Ansible, plus every capability's recorded completion check.

Record later service migrations under
[LAB-96](https://tack.home.goodkind.io/browse/LAB-96). Their application migration
does not block this plan.

For every unresolved implementation decision, state the verified constraint,
available options, practical effects, and recommendation. Only dependent work
waits for the decision. The migration does not authorize production changes
or physical LAN activation beyond the existing limits.

## Supporting references

- The [Tack deployment design](https://github.com/agoodkind/tack/blob/main/docs/superpowers/specs/2026-10-10-tack-provider.md) requires generic provider reuse and complete deployment acceptance.
- The [MWAN network specification](https://github.com/agoodkind/mwan/blob/main/docs/superpowers/networkplan/spec.md) defines network state and validation requirements.
- The [pveguest contract](https://github.com/agoodkind/terraform-provider-pveguest/blob/main/README.md) establishes existing guest resource behavior.
- The Docker provider documents [container lifecycle](https://github.com/kreuzwerker/terraform-provider-docker/blob/master/docs/resources/container.md) and [image management](https://github.com/kreuzwerker/terraform-provider-docker/blob/master/docs/resources/image.md) as reuse candidates.
- OpenTofu documents [planning](https://opentofu.org/docs/cli/commands/plan/), [import](https://opentofu.org/docs/cli/import/), and [resource behavior](https://opentofu.org/docs/language/resources/behavior/).
