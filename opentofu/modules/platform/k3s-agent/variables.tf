variable "guest" { type = object({ node = string, vmid = number, hostname = string }) }
variable "server_ipv6" { type = string }
variable "artifact" { type = object({ url = string, sha256 = string }) }
variable "taints" { type = list(string) }
variable "join_token" {
  type      = string
  sensitive = true
  ephemeral = true
}
variable "join_token_version" { type = number }
variable "registry_credentials" {
  type      = object({ username = string, token = string })
  sensitive = true
  ephemeral = true
}
variable "registry_credentials_version" { type = number }
