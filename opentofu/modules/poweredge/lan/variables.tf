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

variable "vpp_idle_wait_usec" {
  description = "Microseconds that VPP waits between polling iterations. Zero disables waiting."
  type        = number
  default     = 0

  validation {
    condition = (
      var.vpp_idle_wait_usec >= 0 &&
      var.vpp_idle_wait_usec <= 2147483647 &&
      floor(var.vpp_idle_wait_usec) == var.vpp_idle_wait_usec
    )
    error_message = "Set vpp_idle_wait_usec to a whole number from 0 through 2147483647."
  }
}
