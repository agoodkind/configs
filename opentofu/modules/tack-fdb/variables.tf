variable "node" {
  description = "Provider node key of the guest."
  type        = string
}

variable "vmid" {
  description = "Proxmox ID of the guest."
  type        = number
}

variable "kind" {
  description = "Guest kind."
  type        = string
}

variable "image" {
  description = "FoundationDB image version and expected image ID."
  type = object({
    version = string
    id      = string
  })

  validation {
    condition     = can(regex("^[0-9]+\\.[0-9]+\\.[0-9]+$", var.image.version))
    error_message = "Image version must contain three numbers separated by dots."
  }

  validation {
    condition     = can(regex("^sha256:[0-9a-f]{64}$", var.image.id))
    error_message = "Digest must start with `sha256:` followed by exactly 64 lowercase hexadecimal characters."
  }
}

variable "tack_commit" {
  description = "Tack commit that the module uses to download the overlay."
  type        = string

  validation {
    condition     = can(regex("^[0-9a-f]{40}$", var.tack_commit))
    error_message = "Commit must contain exactly 40 lowercase hexadecimal characters."
  }
}

variable "overlay" {
  description = "Absolute guest path and expected SHA-256 of the overlay file at the Tack commit."
  type = object({
    path   = string
    sha256 = string
  })

  validation {
    condition     = can(regex("^[0-9a-f]{64}$", var.overlay.sha256))
    error_message = "Overlay SHA-256 must contain exactly 64 lowercase hexadecimal characters."
  }
}

variable "repository_read_token" {
  description = "GitHub token with read access to agoodkind/tack for downloading the overlay."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "repository_read_token_version" {
  description = "Version of the repository read token. Changing it downloads the overlay again on the next apply."
  type        = number
}

variable "public_address" {
  description = "Address that the FoundationDB server announces on port 4500 to peers and clients."
  type        = string
}

variable "locality_id" {
  description = "Guest identifier used for both the FoundationDB zone and machine IDs. FoundationDB places copies of a key in different zones."
  type        = string
}

variable "network" {
  description = "Existing Docker network name and IPv6 gateway for the backup agent. The agent attaches without an IPv4 address."
  type = object({
    name         = string
    ipv6_gateway = string
  })
}

variable "blobstore_address" {
  description = "IPv6 address of the backup object store. The backup agent maps fdb-blobstore-host to this address because fdbbackup cannot resolve an IPv6 literal."
  type        = string
}

variable "volumes" {
  description = "Names and labels of the existing state, server data, and backup agent data volumes. Labels must match the existing volumes because a label change requires replacement."
  type = object({
    state = object({
      name   = string
      labels = map(string)
    })
    data = object({
      name   = string
      labels = map(string)
    })
    agent_data = object({
      name   = string
      labels = map(string)
    })
  })
}
