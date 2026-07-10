# Vault hypervisor

Vault is the production Proxmox hypervisor. It runs the household's core service containers and the two virtual machines that route its traffic, all behind the OPNsense router. This page names roles and points at where the current values live, because the addresses and ids drift and the config owns them.

## What runs here

The containers give the household its DNS, with an ad-blocking resolver that forwards upstream to NextDNS and a separate DNS64 resolver that lets IPv6-only clients reach IPv4 hosts. Alongside them run the UniFi network controller, a reverse proxy that also carries an SSH multiplexer and a Cloudflare tunnel, a groupware mail stack, a Minecraft server, and the Proxmox Datacenter Manager. A single-node Consul server ties the containers together for service discovery, and one container that Ansible does not manage is a developer sandbox.

Two virtual machines do the routing: the OPNsense LAN router, and the MWAN VM that owns the wide-area links. Vault also runs one service of its own outside the guests, the MWAN watchdog, which watches the MWAN VM from the host and rolls it back to a known-good snapshot when a change breaks its connectivity.

## Where the current values live

The canonical hostnames and IPv6 addresses for the service containers live in [service_mapping.yml](../../ansible/inventory/group_vars/all/service_mapping.yml). The live guest roster, each container and VM with its id, comes from the vault Proxmox dynamic inventory in [ansible/inventory/vault.proxmox.yml](../../ansible/inventory/vault.proxmox.yml), and the provisioned guests live in [opentofu/](../../opentofu/).

## Reading the current state

To see what is actually running rather than what a doc claims, probe vault directly. These are read-only:

```bash
ssh root@<vault> 'qm list'    # virtual machines with their ids and status
ssh root@<vault> 'pct list'   # containers with their ids and status
ssh root@<vault> "systemctl --plain --no-legend list-units 'mwan*'"  # the host MWAN services
```

Reach a specific guest to read its own state, preferring IPv6, using the entry points in [access.md](access.md).
