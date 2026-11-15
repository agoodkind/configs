variable "vault_tack_qa_k3s_server_token" {
  description = "QA k3s server token from the encrypted credential source."
  type        = string
  sensitive   = true
  ephemeral   = true
}
