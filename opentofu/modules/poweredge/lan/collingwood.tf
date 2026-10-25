locals {
  collingwood_networks = {
    for index, name in ["privileged", "general", "guest"] : name => {
      vlan_id       = (index + 1) * 100
      subnet_id     = 101 + index
      subnet4       = "10.230.${index + 1}.0/24"
      subnet6       = "3d06:bad:b01:${301 + index}::/64"
      gateway4      = "10.230.${index + 1}.1"
      gateway6      = "3d06:bad:b01:${301 + index}::1"
      pool4         = "10.230.${index + 1}.100 - 10.230.${index + 1}.200"
      pool6         = "3d06:bad:b01:${301 + index}::100 - 3d06:bad:b01:${301 + index}::ffff"
      domain        = "${name}.collingwood.goodkind.io"
      reverse4_zone = "${index + 1}.230.10.in-addr.arpa"
      reverse6_zone = "${index + 1}.0.3.0.1.0.b.0.d.a.b.0.6.0.d.3.ip6.arpa"
    }
  }
  collingwood_zones = merge([
    for name, network in local.collingwood_networks : {
      "${name}_forward" = {
        zone       = network.domain
        path       = "/var/lib/bind/kea/${name}-forward.zone"
        gateway4   = network.gateway4
        gateway6   = network.gateway6
        forward    = true
        nameserver = "ns.${network.domain}"
        reverse6   = false
      }
      "${name}_reverse4" = {
        zone       = network.reverse4_zone
        path       = "/var/lib/bind/kea/${name}-reverse4.zone"
        gateway4   = network.gateway4
        gateway6   = network.gateway6
        forward    = false
        nameserver = "ns.${network.domain}"
        reverse6   = false
      }
      "${name}_reverse6" = {
        zone       = network.reverse6_zone
        path       = "/var/lib/bind/kea/${name}-reverse6.zone"
        gateway4   = network.gateway4
        gateway6   = network.gateway6
        forward    = false
        nameserver = "ns.${network.domain}"
        reverse6   = true
      }
    }
  ]...)
  kea_base4     = jsondecode(file("${path.module}/files/kea-dhcp4.conf"))
  kea_base6     = jsondecode(file("${path.module}/files/kea-dhcp6.conf"))
  kea_base_ddns = jsondecode(file("${path.module}/files/kea-dhcp-ddns.conf"))
  collingwood_kea_configs = {
    dhcp4 = {
      Dhcp4 = merge(local.kea_base4.Dhcp4, {
        "interfaces-config" = merge(local.kea_base4.Dhcp4["interfaces-config"], {
          interfaces = concat(local.kea_base4.Dhcp4["interfaces-config"].interfaces, keys(local.collingwood_networks))
        })
        subnet4 = concat(local.kea_base4.Dhcp4.subnet4, [
          for name, network in local.collingwood_networks : {
            id                       = network.subnet_id
            subnet                   = network.subnet4
            interface                = name
            pools                    = [{ pool = network.pool4 }]
            "ddns-qualifying-suffix" = network.domain
            "option-data" = [
              { name = "dhcp-server-identifier", data = network.gateway4 },
              { name = "routers", data = network.gateway4 },
              { name = "domain-name-servers", data = network.gateway4 },
              { name = "domain-name", data = network.domain },
              { name = "domain-search", data = network.domain },
            ]
          }
        ])
      })
    }
    dhcp6 = {
      Dhcp6 = merge(local.kea_base6.Dhcp6, {
        "interfaces-config" = merge(local.kea_base6.Dhcp6["interfaces-config"], {
          interfaces = concat(local.kea_base6.Dhcp6["interfaces-config"].interfaces, keys(local.collingwood_networks))
        })
        subnet6 = concat(local.kea_base6.Dhcp6.subnet6, [
          for name, network in local.collingwood_networks : {
            id                       = network.subnet_id
            subnet                   = network.subnet6
            interface                = name
            pools                    = [{ pool = network.pool6 }]
            "ddns-qualifying-suffix" = network.domain
            "option-data" = [
              { name = "dns-servers", data = network.gateway6 },
              { name = "domain-search", data = network.domain },
            ]
          }
        ])
      })
    }
    ddns = {
      DhcpDdns = merge(local.kea_base_ddns.DhcpDdns, {
        "forward-ddns" = {
          "ddns-domains" = concat(local.kea_base_ddns.DhcpDdns["forward-ddns"]["ddns-domains"], [
            for zone in values(local.collingwood_zones) : {
              name          = "${zone.zone}."
              "key-name"    = "kea-ddns."
              "dns-servers" = [{ "ip-address" = "::1", port = 53 }]
            } if zone.forward
          ])
        }
        "reverse-ddns" = {
          "ddns-domains" = concat(local.kea_base_ddns.DhcpDdns["reverse-ddns"]["ddns-domains"], [
            for zone in values(local.collingwood_zones) : {
              name          = "${zone.zone}."
              "key-name"    = "kea-ddns."
              "dns-servers" = [{ "ip-address" = "::1", port = 53 }]
            } if !zone.forward
          ])
        }
      })
    }
  }
}

resource "pveguest_file" "collingwood_netdev" {
  for_each = local.collingwood_networks

  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  path    = "/etc/systemd/network/60-${each.key}.netdev"
  content = templatefile("${path.module}/files/vlan.netdev.tftpl", { name = each.key, network = each.value })
}

resource "pveguest_file" "collingwood_network" {
  for_each = local.collingwood_networks

  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  path    = "/etc/systemd/network/60-${each.key}.network"
  content = templatefile("${path.module}/files/vlan.network.tftpl", { name = each.key, network = each.value })
}

resource "pveguest_file" "collingwood_parent" {
  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  path    = "/etc/systemd/network/40-lan-out.network.d/collingwood.conf"
  content = templatefile("${path.module}/files/collingwood-network.conf.tftpl", local.template_vars)
}

resource "pveguest_file" "collingwood_network_unit" {
  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  path    = "/etc/systemd/system/collingwood-network.service"
  content = templatefile("${path.module}/files/collingwood-network.service", local.template_vars)
}

resource "pveguest_file" "collingwood_zone" {
  for_each = local.collingwood_zones

  node     = var.node
  vmid     = var.vmid
  kind     = "lxc"
  path     = each.value.path
  content  = templatefile("${path.module}/files/ddns-zone.tftpl", each.value)
  owner    = "bind"
  group    = "bind"
  mode     = "0640"
  validate = "/usr/bin/named-checkzone ${each.value.zone} %s"

  lifecycle {
    ignore_changes = [content]
  }

  depends_on = [pveguest_apt_packages.lan]
}

# Adopt the active firewall without restarting it. Moving the previous unit
# state would restart the service when its restart_on map changes.
resource "pveguest_systemd_unit" "firewall" {
  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  name    = "nftables.service"
  enabled = true
  active  = true

  depends_on = [pveguest_file.service["firewall"]]
}

resource "pveguest_systemd_unit" "collingwood_network" {
  node    = var.node
  vmid    = var.vmid
  kind    = "lxc"
  name    = "collingwood-network.service"
  enabled = true
  active  = true
  restart_on = merge(
    { for name, config in pveguest_file.collingwood_netdev : "netdev_${name}" => config.write_id },
    { for name, config in pveguest_file.collingwood_network : "network_${name}" => config.write_id },
    {
      parent   = pveguest_file.collingwood_parent.write_id
      unit     = pveguest_file.collingwood_network_unit.write_id
      firewall = pveguest_file.service["firewall"].write_id
    }
  )

  depends_on = [pveguest_systemd_unit.network, pveguest_systemd_unit.firewall]
}
