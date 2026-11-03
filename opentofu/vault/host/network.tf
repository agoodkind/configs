locals {
  guest_network_files = {
    private_tunnel_unit = {
      path   = "/etc/systemd/system/cloudflared.service"
      source = "cloudflared.service"
    }
    bridge = {
      path   = "/etc/network/interfaces.d/mbrains"
      source = "mbrains"
    }
    uplink = {
      path   = "/etc/systemd/network/20-vault-uplink.network"
      source = "uplink.network"
    }
    translator = {
      path   = "/etc/tayga.conf"
      source = "tayga.conf"
    }
    translator_unit = {
      path   = "/etc/systemd/system/tayga.service"
      source = "tayga.service"
    }
    firewall = {
      path   = "/etc/vault-guest-network.nft"
      source = "guest-network.nft"
    }
    firewall_unit = {
      path   = "/etc/systemd/system/vault-guest-network.service"
      source = "guest-network.service"
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

resource "remote_file" "guest_network" {
  for_each = local.guest_network_files

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

locals {
  guest_network_activation_revision = sha256(jsonencode(concat([
    terraform_data.guest_network_packages.id,
    module.overlay["vault"].network_interfaces_sha256,
    filesha256("${path.module}/network/activate.sh"),
    ], [
    for name in sort(keys(remote_file.guest_network)) :
    sha256(remote_file.guest_network[name].content)
  ])))
}

# Stop the previous DHCP owner before systemd-networkd requests a lease.
# activate.sh defines the complete runtime activation sequence.
resource "terraform_data" "guest_network_activate" {
  triggers_replace = [local.guest_network_activation_revision]

  connection {
    type        = "ssh"
    host        = local.overlay_hosts.vault.ssh_host
    user        = "root"
    agent       = true
    script_path = "/tmp/vault-network-${local.guest_network_activation_revision}.sh"
  }

  provisioner "remote-exec" {
    script = "${path.module}/network/activate.sh"
  }
}
