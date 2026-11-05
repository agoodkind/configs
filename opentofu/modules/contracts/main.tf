locals {
  tack_tunnel_id = "4b602332-6413-4f95-8874-561ed6d9b266"

  tack_domain_aliases = {
    "get-tack.com"    = "cfff9eaba9a95c6b6375be80bb88a31c"
    "tack.so"         = "ab327a658a8a5c849315a2833c6ae007"
    "tack.technology" = "6b20dd82026942a481429af4447d75f2"
  }

  shared_vars = {
    "proxmox_api_user"         = "ansible@pam"
    "proxmox_token_id"         = "ansible-token"
    "github_ssh_keys_user"     = "agoodkind"
    "login_dir"                = "/root"
    "guest_prep_revision"      = "1"
    "guest_prep_revision_file" = "/var/lib/configs/guest-prep-revision"
    "smtp_host"                = "mail.smtp2go.com"
    "smtp_port"                = "587"
    "smtp_from_domain"         = "goodkind.io"
  }

  cloudflare_zone_ids = {
    "goodkind.io"      = "13ccc5ba064b1661495c0ec86511e9a7"
    "silver-flare.com" = "de02872854ceebbe1d731e3aef200de2"
  }
}

output "shared_vars" { value = local.shared_vars }
output "proxmox_token_principal" { value = "${local.shared_vars.proxmox_api_user}!${local.shared_vars.proxmox_token_id}" }
output "cloudflare_zone_ids" { value = local.cloudflare_zone_ids }
output "tack_domain_aliases" { value = local.tack_domain_aliases }
output "tack_tunnel_id" { value = local.tack_tunnel_id }
