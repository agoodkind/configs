variable "guest" {
  description = "Proxmox VM identity, network addresses, and disk storage."
  type = object({
    node              = string
    node_name         = string
    vmid              = number
    hostname          = string
    service_key       = string
    bridge            = optional(string)
    mac_address       = string
    ipv4_cidr         = optional(string)
    ipv4_gateway      = optional(string)
    ipv6_cidr         = string
    ipv6_gateway      = optional(string)
    dns_servers       = list(string)
    datastore_id      = string
    image_datastore   = string
    snippet_datastore = string
  })

  validation {
    condition = (
      (var.guest.ipv4_cidr == null || can(cidrnetmask(var.guest.ipv4_cidr))) &&
      can(cidrhost(var.guest.ipv6_cidr, 0)) && strcontains(var.guest.ipv6_cidr, ":") &&
      length(var.guest.dns_servers) > 0
    )
    error_message = "Set a valid IPv6 CIDR and at least one DNS server for the VM. The optional IPv4 CIDR must be valid."
  }
}

variable "cloud_image" {
  description = "Immutable Debian cloud image URL, file name, and published SHA-512 checksum."
  type = object({
    url       = string
    file_name = string
    sha512    = string
  })

  validation {
    condition = (
      startswith(var.cloud_image.url, "https://") &&
      can(regex("^[a-f0-9]{128}$", var.cloud_image.sha512))
    )
    error_message = "Set an HTTPS cloud image URL and its 128-character lowercase SHA-512 checksum."
  }
}

variable "vm_capacity" {
  description = "Set the k3s VM CPU cores, memory in MiB, and root disk size in GiB."
  type = object({
    cores      = number
    memory_mib = number
    disk_gib   = number
  })
}

variable "initialization" {
  type = object({ public_keys = list(string), packages = list(string), filename_prefix = string })
}
variable "tags" { type = list(string) }

variable "pci_mapping" {
  type    = string
  default = null
}
variable "started" {
  type    = bool
  default = true
}
