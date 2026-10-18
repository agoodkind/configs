variable "poweredge_idrac_endpoint" {
  description = "HTTPS endpoint of the PowerEdge iDRAC Redfish service."
  type        = string
  default     = "https://[3d06:bad:b01:300::efb]"
}

variable "poweredge_idrac_user" {
  description = "Existing iDRAC user that can configure the PowerEdge NIC."
  type        = string
  default     = "root"
}

variable "vault_poweredge_idrac_password" {
  description = "Existing iDRAC password supplied by configsctl or TF_VAR_vault_poweredge_idrac_password."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "poweredge_idrac_insecure" {
  description = "Allow the existing iDRAC certificate without certificate verification."
  type        = bool
  default     = false
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
