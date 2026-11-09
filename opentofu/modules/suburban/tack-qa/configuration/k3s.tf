locals {
  tack_k3s_guest = {
    hostname    = "tack-k3s.suburban.goodkind.io"
    service_key = "tack_k3s_suburban"
    node        = "suburban"
    vmid        = 228
    kind        = "qemu"
    ipv4        = "10.240.4.10"
    ipv6        = "3d06:bad:b01:210::228"
    mac_address = "BC:24:11:04:02:28"
  }
  tack_k3s = {
    guest = merge(local.tack_k3s_guest, {
      node_name         = "hypervisor"
      ipv4_cidr         = "${local.tack_k3s_guest.ipv4}/24"
      ipv4_gateway      = module.opnsense_suburban.value.ipv4_vmnet
      ipv6_cidr         = "${local.tack_k3s_guest.ipv6}/64"
      ipv6_gateway      = module.opnsense_suburban.value.ipv6_vmnet
      dns_servers       = [module.dns64_suburban.value.ipv6]
      datastore_id      = "local-zfs"
      image_datastore   = "local"
      snippet_datastore = "local"
    })
    cloud_image = {
      url       = "https://cloud.debian.org/images/cloud/trixie/20261103-2651/debian-13-genericcloud-amd64-20261103-2651.qcow2"
      file_name = "debian-13-genericcloud-amd64-20261103-2651.qcow2"
      sha512    = "d25999de0770d4cde1be72c299ba3ad94eac7ea8fd4d18f8cc86c69df69337e2e8a91d64465a9bfd8f2a78c566fcbcb0842eacc91c09efdef72d719d9f13b535"
    }
    artifact = {
      version = "v1.37.2+k3s1"
      url     = "https://github.com/k3s-io/k3s/releases/download/v1.37.2%2Bk3s1/k3s"
      sha256  = "265b3ee6a3198ef14033c63ef46cbb32370d83795199fb6d1b01f21575eddbda"
    }
    cluster_network = {
      pod_cidr     = "fd42:7461:636b:100::/56"
      service_cidr = "fd42:7461:636b:200::/112"
      dns_address  = "fd42:7461:636b:200::a"
    }
    snapshot = {
      endpoint = "[3d06:bad:b01:210::218]:8333"
      bucket   = "tack-backups"
      folder   = "k3s/qa"
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
