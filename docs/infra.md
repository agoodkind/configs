# Infrastructure overview

The goodkind.io homelab runs on two Proxmox hypervisors. Vault, in San Francisco, carries production: the containers and virtual machines that sit behind the OPNsense router and serve the household every day. Suburban, in New Jersey, carries a testbed that mirrors production closely enough to rehearse a risky change before it reaches the real thing.

These pages are point-in-time snapshots, not a live feed, so trust the running host over any page here and read the host before you change production.

The homelab meets the internet through Cloudflare, which fronts its public
services with tunnels and answers DNS for the domain.
