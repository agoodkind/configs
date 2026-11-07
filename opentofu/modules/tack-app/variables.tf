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

variable "commit" {
  description = "Tack commit with 40 characters used as the image tag."
  type        = string

  validation {
    condition     = can(regex("^[0-9a-f]{40}$", var.commit))
    error_message = "Commit must contain exactly 40 lowercase hexadecimal characters."
  }
}

variable "server_digest" {
  description = "Expected tack-server index digest."
  type        = string

  validation {
    condition     = can(regex("^sha256:[0-9a-f]{64}$", var.server_digest))
    error_message = "Digest must start with `sha256:` followed by exactly 64 lowercase hexadecimal characters."
  }
}

variable "environment" {
  description = "Environment variables for the app container. Values must not contain secrets, and keys must not overlap with variables that the module sets."
  type        = map(string)
}

variable "extra_hosts" {
  description = "Map of host names to IPv6 addresses for the container."
  type        = map(string)
}

variable "network" {
  description = "Existing Docker network name and IPv6 gateway. The container attaches to this network without an IPv4 address."
  type = object({
    name         = string
    ipv6_gateway = string
  })
}

variable "log_volume" {
  description = "Name and labels of the existing Docker volume for app logs. Labels must match the existing volume because a label change requires replacement."
  type = object({
    name   = string
    labels = map(string)
  })
}

variable "compose_writer_active" {
  description = "Map of Compose writer unit names to their active states. Every value must be false before the module creates the app container."
  type        = map(bool)
}
