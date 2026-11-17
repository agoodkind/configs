variable "dns_api_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

locals {
  proxy_node = proxmox_virtual_environment_container.proxy.node_name
  proxy_vmid = proxmox_virtual_environment_container.proxy.vm_id

  certificate_domains = concat(
    [local.primary_certificate],
    sort(tolist(setsubtract(keys(local.http_routes), [local.primary_certificate])))
  )
  lego_command = join(" ", concat(
    ["/usr/bin/lego", "--path", "/var/lib/lego", "--email", "alex@goodkind.io", "--accept-tos", "--dns", "cloudflare"],
    [for domain in local.certificate_domains : "--domains ${domain}"]
  ))
  certificate_units = {
    "proxy-certificate.service" = templatefile("${path.module}/files/proxy-certificate.service.tftpl", {
      lego_command = local.lego_command
      primary      = local.primary_certificate
    })
    "proxy-renew.service" = templatefile("${path.module}/files/proxy-renew.service.tftpl", {
      lego_command = local.lego_command
    })
    "proxy-renew.timer" = file("${path.module}/files/proxy-renew.timer")
  }
}

resource "pveguest_apt_packages" "proxy" {
  node           = local.proxy_node
  vmid           = local.proxy_vmid
  kind           = "lxc"
  packages       = ["lego", "nginx", "libnginx-mod-stream", "libnginx-mod-http-headers-more-filter"]
  start_services = false
}

resource "pveguest_file" "dns_token" {
  node               = local.proxy_node
  vmid               = local.proxy_vmid
  kind               = "lxc"
  path               = "/etc/lego/cloudflare-dns-token"
  mode               = "0600"
  content_wo         = trimspace(var.dns_api_token)
  content_wo_version = 1
}

resource "pveguest_file" "certificate_unit" {
  for_each = local.certificate_units
  node     = local.proxy_node
  vmid     = local.proxy_vmid
  kind     = "lxc"
  path     = "/etc/systemd/system/${each.key}"
  content  = each.value
}

resource "pveguest_systemd_unit" "certificates" {
  node       = local.proxy_node
  vmid       = local.proxy_vmid
  kind       = "lxc"
  name       = "proxy-renew.timer"
  enabled    = true
  active     = true
  restart_on = { for name, unit in pveguest_file.certificate_unit : name => unit.write_id }
  depends_on = [pveguest_apt_packages.proxy, pveguest_file.dns_token]
}

resource "pveguest_file" "nginx" {
  node = local.proxy_node
  vmid = local.proxy_vmid
  kind = "lxc"
  path = "/etc/nginx/nginx.conf"
  content = templatefile("${path.module}/files/nginx.conf.tftpl", {
    routes              = local.http_routes
    streams             = local.stream_routes
    primary_certificate = local.primary_certificate
  })
  validate   = "/usr/sbin/nginx -t -c %s"
  depends_on = [pveguest_systemd_unit.certificates]
}

resource "pveguest_systemd_unit" "stop_traefik" {
  node          = local.proxy_node
  vmid          = local.proxy_vmid
  kind          = "lxc"
  name          = "traefik.service"
  enabled       = false
  active        = false
  allow_missing = true
  depends_on    = [pveguest_file.nginx]
}

resource "pveguest_systemd_unit" "nginx" {
  node       = local.proxy_node
  vmid       = local.proxy_vmid
  kind       = "lxc"
  name       = "nginx.service"
  enabled    = true
  active     = true
  restart_on = { configuration = pveguest_file.nginx.write_id }
  depends_on = [pveguest_systemd_unit.stop_traefik]
}

resource "pveguest_file_absent" "traefik" {
  for_each = toset([
    "/usr/local/bin/traefik",
    "/etc/systemd/system/traefik.service",
    "/etc/traefik/traefik.yml",
    "/etc/traefik/traefik.yml.pre-22001",
    "/etc/traefik/dynamic/routes.yml",
    "/etc/traefik/dynamic/routes.yml.pre-22001",
    "/etc/traefik/dynamic/middlewares.yml",
  ])
  node       = local.proxy_node
  vmid       = local.proxy_vmid
  kind       = "lxc"
  path       = each.value
  depends_on = [pveguest_systemd_unit.nginx]
}
