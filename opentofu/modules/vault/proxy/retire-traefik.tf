locals {
  proxy_node = proxmox_virtual_environment_container.proxy.node_name
  proxy_vmid = proxmox_virtual_environment_container.proxy.vm_id
}

resource "pveguest_systemd_unit" "stop_traefik" {
  node          = local.proxy_node
  vmid          = local.proxy_vmid
  kind          = "lxc"
  name          = "traefik.service"
  enabled       = false
  active        = false
  allow_missing = true
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
    "/etc/traefik/.cloudflare.env",
    "/etc/traefik/.rfc2136.env",
    "/etc/traefik/acme.json",
  ])
  node       = local.proxy_node
  vmid       = local.proxy_vmid
  kind       = "lxc"
  path       = each.value
  depends_on = [pveguest_systemd_unit.stop_traefik]
}
