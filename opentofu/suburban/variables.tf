variable "automation_user" {
  description = "Proxmox user that receives the scoped overlay roles."
  type        = string
}

variable "ssh_keys" {
  description = "Newline-separated SSH public keys injected into new suburban guests."
  type        = string
}
