variable "guest" { type = object({ node = string, vmid = number }) }
variable "k3s_artifact" { type = object({ url = string, sha256 = string }) }
variable "registry_credentials" {
  type      = object({ username = string, token = string })
  sensitive = true
  ephemeral = true
}
variable "registry_credentials_version" { type = number }
