locals {
  host_network_files = {
    private_tunnel_unit = {
      path   = "/etc/systemd/system/cloudflared.service"
      source = "cloudflared.service"
    }
    bridge = {
      path   = "/etc/network/interfaces.d/uplink"
      source = "uplink"
    }
    uplink = {
      path   = "/etc/systemd/network/20-vault-uplink.network"
      source = "uplink.network"
    }
    forwarding = {
      path   = "/etc/sysctl.d/70-vault-guest-network.conf"
      source = "forwarding.conf"
    }
  }
}

resource "terraform_data" "guest_network_packages" {
  triggers_replace = [filesha256("${path.module}/network/install-packages.sh")]

  connection {
    type  = "ssh"
    host  = local.overlay_hosts.vault.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    script = "${path.module}/network/install-packages.sh"
  }
}

resource "remote_file" "host_network" {
  for_each = local.host_network_files

  conn {
    host  = local.overlay_hosts.vault.ssh_host
    user  = "root"
    agent = true
  }

  path        = each.value.path
  content     = file("${path.module}/network/${each.value.source}")
  permissions = "0644"

  depends_on = [terraform_data.guest_network_packages]
}
