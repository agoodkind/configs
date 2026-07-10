# Infrastructure overview

The goodkind.io homelab runs on two Proxmox hypervisors. Vault, in San Francisco, carries production: the containers and virtual machines that sit behind the OPNsense router and serve the household every day. Suburban, in New Jersey, carries a testbed that mirrors production closely enough to rehearse a risky change before it reaches the real thing. A few machines belong to neither hypervisor, and one of them, a travel router named berylax, is offline now and kept only as a record.

These pages are point-in-time snapshots, not a live feed, so trust the running host over any page here and read the host before you change production.

The homelab meets the internet through Cloudflare, which fronts its public services with tunnels and answers DNS for the domain. Reaching a host yourself, whether from a laptop or from the Ansible controller, runs through a small set of SSH entry points that try IPv6 first and fall back to a jump host when a machine cannot be reached directly.
