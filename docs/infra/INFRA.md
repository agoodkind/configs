# Infrastructure

This directory contains the point-in-time infrastructure snapshot for the `goodkind.io`
homelab. The source probe was last captured on 2026-05-02 from live read-only checks
against vault, router, suburban, mwan, proxy, mini, nas, OPNsense, and Cloudflare.
Treat any IP or service state here as potentially stale because these files are not a
live feed.

## Snapshot Files

- [docs/infra/vault.md](vault.md): Proxmox vault hypervisor, vault LXCs, QEMU VMs,
  stopped VMs, and vault host services.
- [docs/infra/mwan-layout.md](mwan-layout.md): MWAN command surfaces, host layout,
  repo drift, stale binaries, manual rollout order, and WAN links.
- [docs/infra/hosts.md](hosts.md): Hosts not on vault Proxmox, including berylax,
  JetKVM, mini, nas, suburban, and related host notes.
- [docs/infra/oob.md](oob.md): Emergency out-of-band access status.
- [docs/infra/suburban-testbed.md](suburban-testbed.md): Suburban MWAN testbed,
  bridges, guests, and production/testbed comparison.
- [docs/infra/opnsense.md](opnsense.md): OPNsense network topology, NPT mapping,
  WireGuard peers, KEA notes, DNS notes, and known failed units.
- [docs/infra/cloudflare.md](cloudflare.md): Cloudflare account, tunnels, WARP
  routes, load balancers, Pages, Workers, email routing, and DNS records.
