locals {
  tack_k3s_guest = {
    hostname    = "tack-k3s.suburban.goodkind.io"
    service_key = "tack_k3s_suburban"
    node        = module.suburban_host.node.key
    vmid        = 228
    kind        = "qemu"
    ipv4        = cidrhost(module.suburban_host.vmnet.ipv4_cidr, 10)
    ipv6        = "3d06:bad:b01:210::228"
    mac_address = "BC:24:11:04:02:28"
  }
  tack_k3s = {
    guest = merge(local.tack_k3s_guest, {
      node_name         = module.suburban_host.guest.node_name
      ipv4_cidr         = "${local.tack_k3s_guest.ipv4}/${split("/", module.suburban_host.vmnet.ipv4_cidr)[1]}"
      ipv4_gateway      = module.suburban_host.vmnet.ipv4_gateway
      ipv6_cidr         = "${local.tack_k3s_guest.ipv6}/64"
      ipv6_gateway      = module.suburban_host.vmnet.ipv6_gateway
      dns_servers       = module.suburban_host.guest.dns_servers
      datastore_id      = module.suburban_host.guest.datastore_id
      image_datastore   = module.suburban_host.guest.image_datastore
      snippet_datastore = module.suburban_host.guest.snippet_datastore
    })
    cloud_image = module.artifacts.debian_cloud_image
    artifact    = module.artifacts.k3s
    cluster_network = {
      pod_cidr     = "fd42:7461:636b:100::/56"
      service_cidr = "fd42:7461:636b:200::/112"
      dns_address  = "fd42:7461:636b:200::a"
    }
    server_token_version                     = 1
    scripts_updater_activation_run           = true
    scripts_updater_randomized_delay_seconds = 300
  }
}

output "tack_k3s" {
  value = local.tack_k3s
}
