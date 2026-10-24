variable "node" {
  description = "Proxmox node that runs the LAN container."
  type        = string
}

variable "vmid" {
  description = "Existing LAN container identifier."
  type        = number
}

variable "nat64_prefix" {
  description = "IPv6 prefix shared by DNS64, VPP NAT64, and the translation route."
  type        = string
}
