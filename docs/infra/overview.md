# Infrastructure overview

The goodkind.io homelab runs on two Proxmox hypervisors. The one in San Francisco, named vault, carries production: the containers and virtual machines that sit behind the OPNsense router and serve the household every day. The one in New Jersey, named suburban, carries a testbed that mirrors production closely enough to rehearse a risky change before it reaches the real thing. A handful of machines belong to neither hypervisor, among them the workstations, a network-attached storage box, and a travel router named berylax that is offline now and survives only as a record.

Every page in this area is written from what someone observed at a point in time, not from a live feed, so a page can fall behind the running system. When a page and a live host disagree, the host is right. Read the host before you change anything in production.

The homelab meets the internet through Cloudflare, which fronts its public services with tunnels and answers DNS for the domain. Reaching a host yourself, whether from a laptop or from the Ansible controller, runs through a small set of SSH entry points that try IPv6 first and fall back to a jump host when a machine cannot be reached directly.
