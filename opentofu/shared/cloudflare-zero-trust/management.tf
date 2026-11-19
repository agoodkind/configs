variable "management_ipv4_cidr" {
  description = "Static management IPv4 address and prefix on Vault enp4s0."
  type        = string
  validation {
    condition     = can(cidrnetmask(var.management_ipv4_cidr)) && !try(cidrcontains("10.250.0.0/24", split("/", var.management_ipv4_cidr)[0]) || cidrcontains(var.management_ipv4_cidr, "10.250.0.0"), false)
    error_message = "Supply a management IPv4 CIDR outside the private guest subnet."
  }
}

variable "management_ipv6_cidr" {
  description = "Static management IPv6 address and prefix on Vault enp4s0."
  type        = string
  validation {
    condition     = can(cidrhost(var.management_ipv6_cidr, 0)) && strcontains(var.management_ipv6_cidr, ":") && !try(cidrcontains("3d06:bad:b01::/64", split("/", var.management_ipv6_cidr)[0]) || cidrcontains(var.management_ipv6_cidr, "3d06:bad:b01::"), false)
    error_message = "Supply a management IPv6 CIDR outside the private guest subnet."
  }
}
