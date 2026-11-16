terraform {
  required_version = ">= 1.11"
  required_providers {
    pveguest = { source = "tofu.home.arpa/agoodkind/pveguest", version = "0.1.3" }
  }
}

variable "data_guest" {
  type = object({ node = string, vmid = number, kind = string, ipv6 = string, subnet = string, gateway = string })
}
variable "application_address" {
  description = "IPv6 address of the single Tack application VM permitted to use Kafka."
  type        = string
  validation {
    condition     = can(cidrhost("${var.application_address}/128", 0)) && strcontains(var.application_address, ":")
    error_message = "Set application_address to the application VM IPv6 address."
  }
}
variable "release_commit" {
  type = string
  validation {
    condition     = can(regex("^[a-f0-9]{40}$", var.release_commit))
    error_message = "Supply the complete release commit from the reviewed artifact configuration."
  }
}
variable "server_image" { type = string }
variable "consumer_image" { type = string }
variable "seed_email" { type = string }
variable "seed_name" { type = string }
variable "signing_id" { type = string }
variable "search_username" {
  type = string
}
variable "secrets" {
  type      = map(string)
  sensitive = true
  ephemeral = true
}
variable "secret_versions" { type = map(number) }
variable "search_endpoint" { type = string }
variable "search_ready" { type = string }
variable "store_images" { type = object({ foundationdb = string, yugabyte = string, kafka = string }) }
variable "overlays" { type = map(string) }
variable "search_settings" { type = object({ replicas = string, shards = string, routing_shards = string }) }
