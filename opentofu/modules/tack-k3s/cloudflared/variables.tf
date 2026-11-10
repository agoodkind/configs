terraform {
  required_version = ">= 1.11"
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.3" }
  }
}

variable "guest" {
  description = "Existing k3s VM identity and node DNS resolvers."
  type = object({
    node        = string
    vmid        = number
    dns_servers = list(string)
  })

  validation {
    condition     = length(var.guest.dns_servers) > 0 && length(var.guest.dns_servers) <= 2
    error_message = "Supply one or two node resolvers; cluster DNS uses the first nameserver slot."
  }
}

variable "tunnel_id" {
  description = "Public identity of this cluster's dedicated Cloudflare tunnel."
  type        = string

  validation {
    condition     = can(regex("^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$", var.tunnel_id))
    error_message = "Supply the dedicated Cloudflare tunnel UUID."
  }
}

variable "token" {
  description = "Native Cloudflare token for this cluster's tunnel."
  type        = string
  sensitive   = true
  ephemeral   = true

  validation {
    condition     = length(trimspace(var.token)) > 0
    error_message = "Supply the native Cloudflare token for the dedicated tunnel."
  }
}

variable "token_version" {
  description = "Increment this public version when rotating the token within one tunnel."
  type        = number

  validation {
    condition     = var.token_version >= 1 && floor(var.token_version) == var.token_version
    error_message = "Set token_version to a positive integer."
  }
}
