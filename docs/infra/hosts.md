# Hosts outside the vault hypervisor

A few machines matter to the homelab without being guests of the vault hypervisor, so they never appear in its inventory. This page is their manual record, and the addresses here are noted by hand rather than pulled from a live source, so read them as a snapshot.

The suburban hypervisor in New Jersey is the most important of them. It is a second Proxmox host that carries the testbed and reaches production over a WireGuard tunnel, and you reach it by direct SSH over its Comcast address. Two workstations sit on the physical-device VLAN, a Linux mini PC that runs a nightly scripts-updater timer and a network-attached storage box that OPNsense knows by a DNS alias. A Home Assistant appliance runs on the home-automation VLAN behind a fixed DHCP reservation and listens for SSH on a nonstandard port. An Intel iMac in New Jersey reaches the network through suburban and has no place in any inventory yet.

Two more machines survive only as records. The berylax travel router is offline, and a pair of JetKVM console devices on the Monkeybrains segment are noted but unmanaged.

Every one of these hosts sends its mail through the same local send-email wrapper, and none is fully managed by Ansible, though a couple are partway there.
