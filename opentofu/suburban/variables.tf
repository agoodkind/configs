variable "ssh_keys" {
  description = "Newline-separated SSH public keys injected into new suburban guests."
  type        = string
}

variable "tack_qa" {
  description = "Settings for the tack-qa LXC on suburban. Lives on the opnsense-test MANAGEMENT segment (opt9, vmbrtrunk untagged, 3d06:bad:b01:204::/64), the closest mirror of prod's opt6 VMNET where the prod tack LXC lives. Default gateway is opnsense-test at 3d06:bad:b01:204::1. Outbound IPv4 reach is via NAT64 (3d06:bad:b01:2664::/96) on opnsense-test."
  type = object({
    vm_id            = number
    hostname         = string
    ipv6_address     = string
    ipv6_gateway     = string
    bridge           = string
    mac_address      = string
    disk_size_gb     = number
    memory_mb        = number
    cpu_cores        = number
    tags             = list(string)
    template_file_id = string
    datastore_id     = string
    dns_servers      = list(string)
  })
  default = {
    vm_id            = 400
    hostname         = "tack-qa"
    ipv6_address     = "3d06:bad:b01:204::400/64"
    ipv6_gateway     = "3d06:bad:b01:204::1"
    bridge           = "vmbrtrunk"
    mac_address      = "BC:24:11:04:00:00"
    disk_size_gb     = 40
    memory_mb        = 8192
    cpu_cores        = 2
    tags             = ["lxc", "tack", "qa", "docker"]
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    datastore_id     = "local-zfs"
    dns_servers      = ["3d06:bad:b01:204::464"]
  }
}

variable "dns64" {
  description = "Settings for the suburban-segment DNS64 LXC. Mirrors the vault-side dns64 service. Lives on the opnsense-test VMNET segment alongside tack-qa. Synthesises AAAA records into 3d06:bad:b01:2664::/96 so guests on the segment can reach IPv4 services via opnsense-test's Tayga NAT64. Bootstrap resolver is NextDNS direct, which is also the bind9 upstream the playbook configures, so the LXC can resolve its own apt mirrors before bind9 starts and continues to use the same upstream after."
  type = object({
    vm_id            = number
    hostname         = string
    ipv6_address     = string
    ipv6_gateway     = string
    bridge           = string
    mac_address      = string
    disk_size_gb     = number
    memory_mb        = number
    cpu_cores        = number
    tags             = list(string)
    template_file_id = string
    datastore_id     = string
    dns_servers      = list(string)
  })
  default = {
    vm_id            = 464
    hostname         = "dns64-suburban"
    ipv6_address     = "3d06:bad:b01:204::464/64"
    ipv6_gateway     = "3d06:bad:b01:204::1"
    bridge           = "vmbrtrunk"
    mac_address      = "BC:24:11:04:64:00"
    disk_size_gb     = 4
    memory_mb        = 512
    cpu_cores        = 1
    tags             = ["lxc", "dns", "dns64"]
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    datastore_id     = "local-zfs"
    dns_servers      = ["3d06:bad:b01:2664::101:101"]
  }
}
