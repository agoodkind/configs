variable "poweredge_idrac_endpoint" {
  description = "HTTPS endpoint of the PowerEdge iDRAC Redfish service."
  type        = string
  default     = "https://[3d06:bad:b01:300::efb]"
}

variable "poweredge_idrac_insecure" {
  description = "Allow the existing iDRAC certificate without certificate verification."
  type        = bool
  default     = false
}

variable "poweredge_idrac_password" {
  description = "Ephemeral iDRAC password supplied by the configsctl credential adapter."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "poweredge_idrac_user" {
  description = "Ephemeral iDRAC user supplied by the configsctl credential adapter."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "poweredge_maintenance_window" {
  description = "Explicit iDRAC maintenance window for a separately controlled host reset."
  type = object({
    start_time = string
    duration   = number
  })

  validation {
    condition = (
      can(timeadd(var.poweredge_maintenance_window.start_time, "0s")) &&
      var.poweredge_maintenance_window.duration > 0 &&
      floor(var.poweredge_maintenance_window.duration) == var.poweredge_maintenance_window.duration
    )
    error_message = "Use an RFC 3339 start time with a UTC offset and a positive whole number of seconds."
  }
}

variable "vault_tofu_state_passphrase" {
  description = "Passphrase that encrypts the OpenTofu state and plan files"
  type        = string
  sensitive   = true
}
