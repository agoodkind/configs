terraform {
  required_version = ">= 1.11"
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.3" }
  }
}

variable "guest" {
  type = object({ node = string, vmid = number, kind = string })
}

variable "identity" {
  type = object({ name = string, id = number })
  validation {
    condition     = can(regex("^[A-Za-z_][A-Za-z0-9_-]{0,30}$", var.identity.name))
    error_message = "Use a sysusers account name with 1 to 31 letters, digits, underscores, or hyphens, starting with a letter or underscore."
  }
  validation {
    condition     = var.identity.id > 0 && var.identity.id < 4294967295 && var.identity.id != 65535 && floor(var.identity.id) == var.identity.id
    error_message = "Use a positive integer account ID below 4294967295, excluding 65535."
  }
}
