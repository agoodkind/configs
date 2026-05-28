variable "ssh_keys" {
  description = "Newline-separated SSH public keys injected into new suburban guests."
  type        = string
}

variable "tack_qa" {
  description = "Settings for the tack-qa LXC on suburban. The default pins every field so the resource is fully described from the module."
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
    ipv6_address     = "3d06:bad:b01:200:1::400/64"
    ipv6_gateway     = "3d06:bad:b01:200::1"
    bridge           = "vmbr1"
    mac_address      = "BC:24:11:04:00:00"
    disk_size_gb     = 40
    memory_mb        = 8192
    cpu_cores        = 2
    tags             = ["lxc", "tack", "qa", "docker"]
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    datastore_id     = "local-zfs"
    dns_servers      = ["3d06:bad:b01:200::464"]
  }
}

variable "dns64" {
  description = "Settings for the suburban-segment DNS64 LXC. Mirrors the vault-side dns64 service. Provides AAAA synthesis for IPv4-only services so IPv6-only guests on the suburban /64 can reach them through a NAT64 path."
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
  })
  default = {
    vm_id            = 464
    hostname         = "dns64-suburban"
    ipv6_address     = "3d06:bad:b01:200::464/64"
    ipv6_gateway     = "3d06:bad:b01:200::1"
    bridge           = "vmbr1"
    mac_address      = "BC:24:11:04:64:00"
    disk_size_gb     = 4
    memory_mb        = 512
    cpu_cores        = 1
    tags             = ["lxc", "dns", "dns64"]
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    datastore_id     = "local-zfs"
  }
}
