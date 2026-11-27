module "contracts" { source = "../../../contracts" }

locals {
  hostname = "vault"
  fqdn     = "hypervisor.${local.hostname}.${module.contracts.management_domain}"
}

output "value" { value = {
  "hostname"     = local.hostname
  "fqdn"         = local.fqdn
  "api_endpoint" = "https://${local.fqdn}:8006"
  "ipv6_oob"     = "3d06:bad:b01:ff::1"
  "ssh_host_key" = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIODevOIi5BNnPpxX0Z+hAFi5zVm7Tnl6HMgVCE5fOiAJ"
} }
