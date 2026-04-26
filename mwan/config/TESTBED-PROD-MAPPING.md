# Testbed to Production Config Mapping

Values that differ between suburban testbed and vault production.
Use this when translating a tested cutover2 procedure to production.

| Config field | Testbed (suburban) | Production (vault) | Source |
|---|---|---|---|
| `hostname` | `suburban-testbed` | `vault` | |
| `mwan_vmid` | `950` | `113` | |
| `mwan_mgmt_addr` | `3d06:bad:b01:200::950` | `3d06:bad:b01::113` | |
| `opnsense.url` | `https://192.168.1.1` | TBD (query prod) | |
| `opnsense.api_key` | Ansible Vault (`vault_suburban_testbed_opnsense_api_key`) | Ansible Vault | |
| `opnsense.api_secret` | Ansible Vault (`vault_suburban_testbed_opnsense_api_secret`) | Ansible Vault | |
| `opnsense.gateway_names` | `["GW_WAN", "GW_WANv6"]` | TBD (query prod) | |
| `opnsense.bgp.router_id` | `10.250.250.2` | `10.250.250.2` (same) | OPNsense WAN IP |
| `opnsense.bgp.neighbors` (v4) | `.3`, `.4` | `.3`, `.4` (same) | VM + LXC IPv4 |
| `opnsense.bgp.neighbors` (v6) | `201::3`, `201::4` | `fe::3`, `fe::4` | Different prefix |
| `watchdog.vsock_cid` | `950` | `113` | |
| `watchdog.mwan_agent_tcp_addr` | `[200::950]:50052` | `[::113]:50052` | |
| `bgp.router_id` | `10.250.250.3` | `10.250.250.3` (same) | VM real address |
| `bgp.neighbors` (v4) | `10.250.250.2` | `10.250.250.2` (same) | OPNsense |
| `bgp.neighbors_v6` | `201::2` | `fe::2` | Different prefix |
| `cutover.failover_lxc_id` | `100` | `116` | |
| `watchdog.service_name` | `mwan-watchdog-testbed` | `mwan-watchdog` | systemd unit name |

## Suburban as the cutover orchestrator (testbed-prod parity)

On production, vault is a single host that hosts everything: PVE, the
mwan-watchdog systemd service, and a LAN bridge that reaches the OPNsense
API directly. cutover2 commands therefore all run from one place.

To mirror that on testbed, suburban joins the OPNsense LAN bridge:

- vmbr3 on suburban has `192.168.1.5/24` (declared in `/etc/network/interfaces`)
- vmbr3 also carries OPNsense's `vtnet0` (the LAN port) and LXC 203's `eth0`,
  so suburban becomes an additional LAN client with direct access to the
  OPNsense API at `https://192.168.1.1`
- Suburban's hypervisor SSH key is in OPNsense's authorized_keys, so the
  switch-to-bgp reboot SSH works
- All cutover2 phases (configure-opnsense, deploy-agents, switch-to-bgp,
  arm-watchdog, disarm-watchdog, unfuck) are intended to run from suburban,
  matching how they all run from vault on prod

Earlier testbed runs split phases between LXC 203 (OPNsense API) and suburban
(PVE + watchdog), which silently no-op'd switch-to-bgp's local watchdog
stop. The single-host arrangement above eliminates that asymmetry.

## Before production cutover

1. Query production OPNsense for gateway names (API or `config.xml`)
2. Create API key on production OPNsense, store in Ansible Vault
3. Query production OPNsense URL (may be `3d06:bad:b01::1` or similar)
4. Populate Ansible group_vars with all `opnsense_*` and `bgp_*` variables
5. Render and deploy `production.toml.j2` via Ansible
