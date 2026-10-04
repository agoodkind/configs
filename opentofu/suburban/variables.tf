variable "acme_account" {
  description = "Name of the ACME account that the automation user manages."
  type        = string
}

variable "acme_plugin" {
  description = "Id of the ACME DNS plugin that the automation user manages."
  type        = string
}

variable "automation_user" {
  description = "Proxmox user that receives the scoped overlay roles."
  type        = string
}

variable "ssh_keys" {
  description = "Newline-separated SSH public keys injected into new suburban guests."
  type        = string
}
