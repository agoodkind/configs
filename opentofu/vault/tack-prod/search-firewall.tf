locals {
  search_guest = local.service_mapping[module.configuration.static_guest_locks["linux_file_line.search_firewall_include"]]
  search_callers = [
    local.service_mapping.tack.ipv6,
    local.service_mapping.tack.docker_v6_subnet,
    local.service_mapping.tack_app2.ipv6,
    local.service_mapping.tack_app2.docker_v6_subnet,
    module.configuration.tack_k3s.guest.ipv6,
  ]
}

resource "pveguest_file" "search_firewall" {
  node = local.search_guest.node
  vmid = local.search_guest.vmid
  kind = local.search_guest.kind
  path = "/etc/nftables.d/tack-search.nft"
  content = templatefile("${path.module}/files/search.nft.tftpl", {
    address = local.search_guest.ipv6
    callers = join(", ", local.search_callers)
  })
  validate = "/usr/sbin/nft --check --file %s"
}

resource "pveguest_file" "search_firewall_unit" {
  node    = local.search_guest.node
  vmid    = local.search_guest.vmid
  kind    = local.search_guest.kind
  path    = "/etc/systemd/system/tack-search-firewall.service"
  content = file("${path.module}/files/tack-search-firewall.service")
}

provider "linux" {
  alias = "search"
  ssh   = { host = local.search_guest.ipv6 }
}

resource "linux_file_line" "search_firewall_include" {
  provider = linux.search
  path     = "/etc/nftables.conf"
  line     = format("include %s", jsonencode(pveguest_file.search_firewall.path))
}

resource "pveguest_systemd_unit" "search_firewall" {
  node    = local.search_guest.node
  vmid    = local.search_guest.vmid
  kind    = local.search_guest.kind
  name    = "tack-search-firewall.service"
  enabled = true
  active  = true

  restart_on = {
    rules = pveguest_file.search_firewall.write_id
    unit  = pveguest_file.search_firewall_unit.write_id
  }

  depends_on = [linux_file_line.search_firewall_include]
}
