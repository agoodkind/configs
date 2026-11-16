locals {
  uplink_ipv6_files = {
    interface = {
      path   = "/etc/network/interfaces.d/suburban-uplink-ipv6"
      source = "suburban-uplink-ipv6"
    }
    sysctl = {
      path   = "/etc/sysctl.d/99-suburban-uplink-ipv6.conf"
      source = "uplink-ipv6.conf"
    }
  }
}

resource "remote_file" "uplink_ipv6" {
  for_each = local.uplink_ipv6_files

  conn {
    host  = "[${local.overlay_hosts.suburban.ssh_host}]"
    user  = "root"
    agent = true
  }

  path        = each.value.path
  content     = file("${path.module}/network/${each.value.source}")
  permissions = "0644"
}

resource "terraform_data" "uplink_ipv6_activate" {
  triggers_replace = [
    filesha256("${path.module}/network/activate-uplink-ipv6.sh"),
    { for name, file in remote_file.uplink_ipv6 : name => sha256(file.content) },
  ]

  lifecycle {
    create_before_destroy = true
  }

  connection {
    type  = "ssh"
    host  = local.overlay_hosts.suburban.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    script = "${path.module}/network/activate-uplink-ipv6.sh"
  }
}
