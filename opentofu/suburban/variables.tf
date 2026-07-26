variable "ssh_keys" {
  description = "Newline-separated SSH public keys injected into new suburban guests."
  type        = string
}

variable "tack_qa" {
  description = "Settings for the tack-qa LXC on suburban. Lives on the opnsense-test MANAGEMENT segment (opt9, vmbrtrunk untagged), the closest mirror of prod's opt6 VMNET where the prod tack LXC lives. That segment sits inside the LAN aggregate MWAN translates, so the guest reaches native IPv6; IPv4-only destinations go via NAT64 on opnsense-test."
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
    ipv6_address     = "3d06:bad:b01:214::400/64"
    ipv6_gateway     = "3d06:bad:b01:214::1"
    bridge           = "vmbrtrunk"
    mac_address      = "BC:24:11:04:00:00"
    disk_size_gb     = 40
    memory_mb        = 8192
    cpu_cores        = 2
    tags             = ["lxc", "tack", "qa", "docker"]
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    datastore_id     = "local-zfs"
    dns_servers      = ["3d06:bad:b01:214::464"]
  }
}

variable "seaweedfs" {
  description = "Settings for the internal-only SeaweedFS object-store LXC on suburban. Lives on the opnsense-test MANAGEMENT segment (opt9, vmbrtrunk untagged) beside tack-qa, with opnsense-test as its default gateway. IPv4-only destinations (for example GitHub release downloads) go via NAT64 on opnsense-test. Runs the weed binary under systemd and exposes an S3 endpoint for tack backups and audit archives. Internal only; never exposed off the segment."
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
    vm_id            = 410
    hostname         = "seaweedfs"
    ipv6_address     = "3d06:bad:b01:214::410/64"
    ipv6_gateway     = "3d06:bad:b01:214::1"
    bridge           = "vmbrtrunk"
    mac_address      = "BC:24:11:04:10:00"
    disk_size_gb     = 100
    memory_mb        = 4096
    cpu_cores        = 2
    tags             = ["lxc", "seaweedfs", "s3"]
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    datastore_id     = "local-zfs"
    dns_servers      = ["3d06:bad:b01:214::464"]
  }
}

variable "dns64" {
  description = "Settings for the suburban-segment DNS64 LXC. Mirrors the vault-side dns64 service. Lives on the opnsense-test MANAGEMENT segment alongside tack-qa. Synthesises AAAA records for IPv4-only names into the NAT64 prefix so guests on the segment reach IPv4 services via opnsense-test's Tayga; dual-stack names keep their native AAAA. Its bootstrap resolver in /etc/resolv.conf matches the bind9 upstream the playbook configures, so the LXC can resolve its own apt mirrors before bind9 starts and keeps the same upstream after."
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
    ipv6_address     = "3d06:bad:b01:214::464/64"
    ipv6_gateway     = "3d06:bad:b01:214::1"
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
