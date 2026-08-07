variable "ssh_keys" {
  description = "Newline-separated SSH public keys injected into new suburban guests."
  type        = string
}

variable "tack_qa" {
  description = "Settings for the tack-qa LXC on suburban. Lives on the opnsense-test VMNET segment (opt6, vmbrtrunk untagged, 3d06:bad:b01:210::/64), matching prod where the tack LXC sits on opt6. Default gateway is opnsense-test at 3d06:bad:b01:210::1. Outbound IPv4 reach is via NAT64 (3d06:bad:b01:2664::/96) on opnsense-test."
  type = object({
    hostname         = string
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
    hostname         = "tack-qa"
    ipv6_gateway     = "3d06:bad:b01:210::1"
    bridge           = "vmbrtrunk"
    mac_address      = "BC:24:11:04:00:00"
    disk_size_gb     = 40
    memory_mb        = 8192
    cpu_cores        = 2
    tags             = ["lxc", "tack", "qa", "docker"]
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    datastore_id     = "local-zfs"
    dns_servers      = ["3d06:bad:b01:210::64"]
  }
}

variable "github_runner" {
  description = "Settings for the repository-scoped GitHub Actions runner LXC on suburban."
  type = object({
    hostname         = string
    ipv6_gateway     = string
    bridge           = string
    disk_size_gb     = number
    memory_mb        = number
    cpu_cores        = number
    tags             = list(string)
    template_file_id = string
    datastore_id     = string
    dns_servers      = list(string)
  })
  default = {
    hostname         = "github-runner"
    ipv6_gateway     = "3d06:bad:b01:210::1"
    bridge           = "vmbrtrunk"
    disk_size_gb     = 80
    memory_mb        = 12288
    cpu_cores        = 6
    tags             = ["ci", "docker", "github-runner", "lxc"]
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    datastore_id     = "local-zfs"
    dns_servers      = ["3d06:bad:b01:210::64"]
  }
}

variable "seaweedfs" {
  description = "Settings for the internal-only SeaweedFS object-store LXC on suburban. Lives on the opnsense-test VMNET segment (opt6, vmbrtrunk untagged, 3d06:bad:b01:210::/64) beside tack-qa. Default gateway is opnsense-test at 3d06:bad:b01:210::1. Outbound IPv4 reach (for example GitHub release downloads) is via NAT64 (3d06:bad:b01:2664::/96) on opnsense-test. Runs the weed binary under systemd and exposes an S3 endpoint for tack backups and audit archives. Internal only; never exposed off the segment."
  type = object({
    hostname         = string
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
    hostname         = "seaweedfs"
    ipv6_gateway     = "3d06:bad:b01:210::1"
    bridge           = "vmbrtrunk"
    mac_address      = "BC:24:11:04:10:00"
    disk_size_gb     = 100
    memory_mb        = 4096
    cpu_cores        = 2
    tags             = ["lxc", "seaweedfs", "s3"]
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    datastore_id     = "local-zfs"
    dns_servers      = ["3d06:bad:b01:210::64"]
  }
}

variable "dns64" {
  description = "Settings for the suburban-segment DNS64 LXC. Mirrors the vault-side dns64 service. Lives on the opnsense-test VMNET segment alongside tack-qa. Synthesises AAAA records into 3d06:bad:b01:2664::/96 so guests on the segment can reach IPv4 services via opnsense-test's Tayga NAT64. Resolves through itself, as every prod guest does including prod's own dns64 and adguard containers, so one resolver serves the whole segment."
  type = object({
    hostname         = string
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
    hostname         = "dns64-suburban"
    ipv6_gateway     = "3d06:bad:b01:210::1"
    bridge           = "vmbrtrunk"
    mac_address      = "BC:24:11:04:64:00"
    disk_size_gb     = 4
    memory_mb        = 512
    cpu_cores        = 1
    tags             = ["lxc", "dns", "dns64"]
    template_file_id = "local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst"
    datastore_id     = "local-zfs"
    dns_servers      = ["3d06:bad:b01:210::64"]
  }
}
