variable "poweredge_host_tunnel_token_file" {
  description = "Protected local file containing the home-poweredge connector token."
  type        = string
  default     = "~/.config/configs/cloudflared/home-poweredge.token"
}

removed {
  from = remote_file.poweredge_cloudflared
  lifecycle { destroy = false }
}

removed {
  from = terraform_data.poweredge_cloudflared
  lifecycle { destroy = false }
}

locals {
  cloudflared_files = "${path.module}/../../modules/poweredge/cloudflared"
}

resource "system_file" "cloudflared_keyring" {
  path   = "/usr/share/keyrings/cloudflare-main.gpg"
  source = "https://pkg.cloudflare.com/cloudflare-main.gpg"
  mode   = "644"
}

resource "system_file" "cloudflared_repository" {
  path    = "/etc/apt/sources.list.d/cloudflared.list"
  content = file("${local.cloudflared_files}/cloudflared.list")
  mode    = "644"

  depends_on = [system_file.cloudflared_keyring]
}

resource "system_packages_apt" "cloudflared" {
  package {
    name = "cloudflared"
  }

  depends_on = [system_file.cloudflared_repository]
}

resource "system_file" "cloudflared_token" {
  path   = "/etc/cloudflared/token"
  source = pathexpand(var.poweredge_host_tunnel_token_file)
  mode   = "600"

  depends_on = [system_packages_apt.cloudflared]
}

resource "system_file" "cloudflared_service" {
  path    = "/etc/systemd/system/cloudflared.service"
  content = file("${local.cloudflared_files}/cloudflared.service")
  mode    = "644"
}

resource "system_systemd_unit" "cloudflared" {
  type    = "service"
  name    = "cloudflared"
  enabled = true
  status  = "started"
  restart_on = [
    system_file.cloudflared_service.md5sum,
    system_file.cloudflared_token.md5sum,
  ]

  depends_on = [system_packages_apt.cloudflared]
}
