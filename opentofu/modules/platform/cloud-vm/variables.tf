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
    ipv6_cidr         = optional(string)
    ipv6_gateway      = optional(string)
    dns_servers       = list(string)
    datastore_id      = string
    image_datastore   = string
    snippet_datastore = string
  })

  validation {
    condition = (
      (var.guest.ipv4_cidr == null || can(cidrnetmask(var.guest.ipv4_cidr))) &&
      (var.guest.ipv6_cidr == null ? var.network_data_file_id != null : can(cidrhost(var.guest.ipv6_cidr, 0)) && strcontains(var.guest.ipv6_cidr, ":")) &&
      length(var.guest.dns_servers) > 0
    )
    error_message = "Supply a valid IPv6 CIDR or a cloud-init network file, at least one DNS server, and a valid optional IPv4 CIDR."
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
    condition = var.cloud_image == null ? var.shared_cloud_image_id != null : (
      var.shared_cloud_image_id == null &&
      startswith(var.cloud_image.url, "https://") &&
      can(regex("^[a-f0-9]{128}$", var.cloud_image.sha512))
    )
    error_message = "Select an existing image ID or an HTTPS image URL with its 128-character lowercase SHA-512 checksum."
  }
}

variable "shared_cloud_image_id" {
  description = "Existing Proxmox image to import when cloud_image is null."
  type        = string
  default     = null
}

variable "network_data_file_id" {
  description = "Proxmox snippet containing the cloud-init network configuration."
  type        = string
  default     = null
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

variable "secondary_pci_mapping" {
  type    = string
  default = null

  validation {
    condition     = var.secondary_pci_mapping == null || (var.pci_mapping != null && var.secondary_pci_mapping != var.pci_mapping)
    error_message = "Set a primary PCI mapping and use a different mapping for the secondary PCI device."
  }
}

variable "started" {
  type    = bool
  default = true
}

variable "smbios_family" {
  description = "Set the SMBIOS family to expose the Proxmox-generated VM UUID."
  type        = string
  default     = null
}
