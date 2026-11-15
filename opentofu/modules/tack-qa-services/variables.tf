terraform {
  required_version = ">= 1.11"
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.3" }
  }
}

variable "data_guest" {
  type = object({ node = string, vmid = number, kind = string, ipv6 = string, subnet = string, gateway = string })
}
variable "search_guest" {
  type = object({ node = string, vmid = number, kind = string, ipv6 = string, subnet = string, gateway = string })
}
variable "release_commit" {
  type = string
  validation {
    condition     = var.release_commit == "74576c94ab9ec6662e9634978d72cb6f06de4470"
    error_message = "Use the reviewed Tack commit with verified overlay hashes."
  }
}
variable "server_image" { type = string }
variable "consumer_image" { type = string }
variable "seed_email" { type = string }
variable "seed_name" { type = string }
variable "signing_id" { type = string }
variable "search_username" {
  type    = string
  default = "tack_runtime"
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
