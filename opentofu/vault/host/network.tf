locals {
  host_network_files = {
    private_tunnel_unit = {
      path   = "/etc/systemd/system/cloudflared.service"
      source = "cloudflared.service"
    }
    bridge = {
      path   = "/etc/network/interfaces.d/vmbr1"
      source = "vmbr1"
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

moved {
  from = remote_file.guest_network["private_tunnel_unit"]
  to   = remote_file.host_network["private_tunnel_unit"]
}

moved {
  from = remote_file.guest_network["bridge"]
  to   = remote_file.host_network["bridge"]
}

moved {
  from = remote_file.guest_network["uplink"]
  to   = remote_file.host_network["uplink"]
}

moved {
  from = remote_file.guest_network["forwarding"]
  to   = remote_file.host_network["forwarding"]
}

locals {
  guest_network_activation_revision = sha256(jsonencode(concat([
    terraform_data.guest_network_packages.id,
    module.overlay["vault"].network_interfaces_sha256,
    filesha256("${path.module}/network/activate.sh"),
    ], [
    for name in sort(keys(remote_file.host_network)) :
    sha256(remote_file.host_network[name].content)
  ])))
}

# Run service activation independently of the SSH session.
resource "terraform_data" "guest_network_activate" {
  triggers_replace = [local.guest_network_activation_revision]

  lifecycle {
    create_before_destroy = true
  }

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
