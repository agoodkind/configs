variable "vault_seaweedfs_s3_access_key" {
  description = "Existing SeaweedFS S3 access key from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_seaweedfs_s3_secret_key" {
  description = "Existing SeaweedFS S3 secret key from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}

variable "vault_tack_k3s_server_token" {
  description = "Production k3s server token for snapshot recovery from the encrypted vault."
  type        = string
  sensitive   = true
  ephemeral   = true
}
