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

resource "linux_download" "cloudflared_keyring" {
  url  = "https://pkg.cloudflare.com/cloudflare-main.gpg"
  path = "/usr/share/keyrings/cloudflare-main.gpg"
}

resource "linux_file" "cloudflared_repository" {
  path    = "/etc/apt/sources.list.d/cloudflared.list"
  content = file("${local.cloudflared_files}/cloudflared.list")

  depends_on = [linux_download.cloudflared_keyring]
}

resource "linux_package" "cloudflared" {
  name = "cloudflared"

  depends_on = [linux_file.cloudflared_repository]
}

resource "linux_file" "cloudflared_token" {
  path    = "/etc/cloudflared/token"
  mode    = "0600"
  content = sensitive(file(pathexpand(var.poweredge_host_tunnel_token_file)))

  depends_on = [linux_package.cloudflared]
}

resource "linux_file" "cloudflared_service" {
  path    = "/etc/systemd/system/cloudflared.service"
  content = file("${local.cloudflared_files}/cloudflared.service")
}

resource "linux_systemd_unit" "cloudflared" {
  name          = "cloudflared.service"
  enabled       = true
  active        = true
  daemon_reload = true

  triggers = {
    unit  = linux_file.cloudflared_service.sha256
    token = linux_file.cloudflared_token.sha256
  }

  depends_on = [linux_package.cloudflared]
}
