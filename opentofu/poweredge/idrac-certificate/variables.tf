variable "idrac_endpoint" {
  description = "Verified iDRAC HTTPS origin. Initial installation can use the pinned factory certificate name."
  type        = string
  default     = "https://idrac.poweredge.management.collingwood.goodkind.io"
}

variable "idrac_initial_ca_file" {
  description = "Factory certificate file for initial installation. An empty value uses system trust."
  type        = string
  default     = ""
}

variable "vault_poweredge_idrac_certificate_password" {
  description = "Protected runtime password of the dedicated certificate account."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_idrac_certificate_state_passphrase" {
  description = "Password that encrypts the certificate job's state and plans."
  type        = string
  sensitive   = true
}
