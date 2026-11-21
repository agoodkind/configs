module "vault_network" { source = "../../modules/configuration/hosts/vault" }

variable "staged_guest_ids" {
  description = "Stage persistent network settings for these running guests before attachment replacement."
  type        = set(number)
  default     = []
}

variable "stopped_guest_ids" {
  description = "Keep these guests stopped while replacing their network attachments."
  type        = set(number)
  default     = []
}

module "guest_network" {
  source      = "../../modules/vault/guest-network"
  for_each    = { for key in ["seaweedfs"] : key => module.vault_network.guest_networks[key] }
  guest       = each.value
  phase       = contains(var.staged_guest_ids, each.value.vmid) ? "staged" : "active"
  dns_servers = module.vault_network.guest.dns_servers
}
