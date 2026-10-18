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

variable "poweredge_nic_functions" {
  description = "Discovered BCM57504 network device function IDs mapped to their Redfish network adapter IDs."
  type        = map(string)

  validation {
    condition = length(var.poweredge_nic_functions) == 4 && alltrue([
      for function_id, adapter_id in var.poweredge_nic_functions :
      trimspace(function_id) != "" && trimspace(adapter_id) != ""
    ])
    error_message = "Supply the four discovered BCM57504 device function IDs and their adapter IDs."
  }
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
