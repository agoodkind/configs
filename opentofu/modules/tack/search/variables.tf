terraform {
  required_version = ">= 1.11"
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.3" }
  }
}

variable "search_guest" {
  type = object({ node = string, vmid = number, kind = string, ipv6 = string })
}
variable "runtime_identity" {
  type = object({ name = string, id = number })
}
variable "search_username" {
  type = string
}
variable "search_admin_dn" {
  description = "Subject DN of the supplied OpenSearch admin certificate."
  type        = string
}
variable "search_node_dn" {
  description = "Subject DN of the supplied OpenSearch node certificate."
  type        = string
}
variable "secrets" {
  type      = map(string)
  sensitive = true
  ephemeral = true
}
variable "secret_versions" { type = map(number) }
variable "image" { type = string }
variable "endpoint" { type = string }
