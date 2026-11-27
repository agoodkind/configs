module "vaults" {
  source = "../../modules/shared/onepassword-vaults"

  services = {
    tack = ["qa", "prod"]
  }
}

module "configuration" {
  source = "../../modules/shared/onepassword-vaults/configuration"
}

output "vault_ids" {
  value = module.vaults.vault_ids
}
