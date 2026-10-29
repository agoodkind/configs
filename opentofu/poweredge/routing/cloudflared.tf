variable "poweredge_mwan_tunnel_token_file" {
  description = "Protected local file containing the home-mwan-poweredge connector token."
  type        = string
  default     = "~/.config/configs/cloudflared/home-mwan-poweredge.token"
}

variable "poweredge_mwan_tunnel_token_version" {
  description = "Increase this value after rotating the MWAN connector token."
  type        = number
  default     = 1
}

locals {
  cloudflared_files   = "${path.module}/../../modules/poweredge/cloudflared"
  cloudflared_release = jsondecode(file("${local.cloudflared_files}/release.json"))
}

resource "pveguest_download" "poweredge_cloudflared" {
  node   = local.poweredge_node
  vmid   = module.poweredge_lxc["gateway"].vmid
  kind   = "lxc"
  fetch  = "controller"
  url    = local.cloudflared_release.url
  sha256 = local.cloudflared_release.sha256
  path   = "/var/cache/cloudflared/cloudflared.deb"
}

resource "pveguest_deb_packages" "poweredge_cloudflared" {
  node = local.poweredge_node
  vmid = module.poweredge_lxc["gateway"].vmid
  kind = "lxc"

  packages = { cloudflared = pveguest_download.poweredge_cloudflared.path }
}

resource "pveguest_file" "poweredge_cloudflared_token" {
  node = local.poweredge_node
  vmid = module.poweredge_lxc["gateway"].vmid
  kind = "lxc"

  path               = "/etc/cloudflared/token"
  content_wo         = sensitive(trimspace(file(pathexpand(var.poweredge_mwan_tunnel_token_file))))
  content_wo_version = var.poweredge_mwan_tunnel_token_version
  mode               = "0600"
}

resource "pveguest_file" "poweredge_cloudflared_service" {
  node = local.poweredge_node
  vmid = module.poweredge_lxc["gateway"].vmid
  kind = "lxc"

  path    = "/etc/systemd/system/cloudflared.service"
  content = file("${local.cloudflared_files}/cloudflared.service")

  depends_on = [pveguest_deb_packages.poweredge_cloudflared]
}

resource "pveguest_systemd_unit" "poweredge_cloudflared" {
  node = local.poweredge_node
  vmid = module.poweredge_lxc["gateway"].vmid
  kind = "lxc"

  name    = "cloudflared.service"
  enabled = true
  active  = true
  restart_on = {
    package = pveguest_download.poweredge_cloudflared.write_id
    token   = pveguest_file.poweredge_cloudflared_token.write_id
    service = pveguest_file.poweredge_cloudflared_service.write_id
  }
}
