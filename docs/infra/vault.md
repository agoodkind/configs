# Vault hypervisor

Vault is the production Proxmox hypervisor, a twelve-core mini PC in San Francisco that carries the household's core services and the two virtual machines that route its traffic. Everything on this page is what a probe found running, so treat it as a snapshot and trust the running system over the page.

Most of what vault runs lives in lightweight containers. Together they give the household its DNS, both an ad-blocking resolver that forwards upstream to NextDNS and a separate DNS64 resolver that lets IPv6-only clients reach IPv4 hosts. Alongside them run the UniFi network controller, a Traefik reverse proxy that also carries an SSH multiplexer and a Cloudflare tunnel, a groupware mail stack, a Minecraft server, and the Proxmox Datacenter Manager. A single-node Consul server ties the containers together for service discovery. One more container, which Ansible does not manage, is a developer sandbox holding a GitHub Actions runner and assorted desktop tooling.

Two virtual machines do the routing. One runs OPNsense, the LAN router, and carries a passed-through network card so the MWAN VM can authenticate the AT&T line. The other is the MWAN VM itself, which runs the multi-WAN agent, a Cloudflare tunnel, and the health daemon, and holds the AT&T certificates for its supplicant. A small FreeBSD test VM and two stopped VMs, a scratch machine and a cloud-image template, round out the set.

Vault also runs one service of its own, outside the guests. The MWAN watchdog watches the MWAN VM from the host and rolls it back to a known-good snapshot when a change breaks its connectivity, reaching the VM over a virtual socket and falling back to TCP when that is unavailable.
