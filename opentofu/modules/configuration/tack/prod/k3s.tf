locals {
  tack_k3s_guest = {
    hostname    = "tack-k3s.vault.goodkind.io"
    service_key = "tack_k3s"
    node        = module.vault_host.node.key
    vmid        = 124
    kind        = "qemu"
    ipv4        = "10.250.0.124"
    ipv6        = "3d06:bad:b01::124"
    mac_address = "BC:24:11:A3:52:7C"
  }
  tack_k3s = {
    guest = merge(local.tack_k3s_guest, module.vault_host.guest, {
      ipv4_cidr    = "${local.tack_k3s_guest.ipv4}/24"
      ipv4_gateway = module.vault_hypervisor.value.ipv4
      ipv6_cidr    = "${local.tack_k3s_guest.ipv6}/64"
      ipv6_gateway = module.vault_hypervisor.value.ipv6
    })
    cloud_image = module.artifacts.debian_cloud_image
    artifact    = module.artifacts.k3s
    cluster_network = {
      pod_cidr     = "fd42:7461:636b:300::/56"
      service_cidr = "fd42:7461:636b:400::/112"
      dns_address  = "fd42:7461:636b:400::a"
    }
    snapshot = {
      endpoint = "[${module.seaweedfs.value.ipv6}]:8333"
      bucket   = "tack-backups"
      folder   = "k3s/prod"
      region   = "us-east-1"
      insecure = true
    }
    snapshot_credentials_version             = 1
    server_token_version                     = 1
    scripts_updater_activation_run           = true
    scripts_updater_randomized_delay_seconds = 300
  }
}

output "tack_k3s" {
  value = local.tack_k3s
}
