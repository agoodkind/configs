variable "poweredge_host_tunnel_token_file" {
  description = "Protected local file containing the home-poweredge connector token."
  type        = string
  default     = "~/.config/configs/cloudflared/home-poweredge.token"
}

variable "poweredge_host_tunnel_token_version" {
  description = "Increase this value after rotating the host connector token."
  type        = number
  default     = 1
}

variable "poweredge_cloudflared_ssh_host" {
  description = "PowerEdge IPv6 management address for installing cloudflared."
  type        = string
  default     = "3d06:bad:b01:300::254"
}

locals {
  cloudflared_files   = "${path.module}/../../modules/poweredge/cloudflared"
  cloudflared_release = jsondecode(file("${local.cloudflared_files}/release.json"))
  cloudflared_host    = var.poweredge_cloudflared_ssh_host
  cloudflared_host_files = {
    service = {
      source = "cloudflared.service"
      path   = "/etc/systemd/system/cloudflared.service"
      mode   = "0644"
    }
    installer = {
      source = "install-host.sh"
      path   = "/usr/local/sbin/install-poweredge-cloudflared"
      mode   = "0755"
    }
  }
}

resource "remote_file" "poweredge_cloudflared" {
  for_each = local.cloudflared_host_files

  conn {
    host  = "[${local.cloudflared_host}]"
    user  = "root"
    agent = true
  }

  path        = each.value.path
  content     = file("${local.cloudflared_files}/${each.value.source}")
  permissions = each.value.mode
}

resource "terraform_data" "poweredge_cloudflared" {
  triggers_replace = {
    token_version = var.poweredge_host_tunnel_token_version
    package       = local.cloudflared_release.sha256
    service       = filesha256("${local.cloudflared_files}/cloudflared.service")
    installer     = filesha256("${local.cloudflared_files}/install-host.sh")
  }

  connection {
    type  = "ssh"
    host  = local.cloudflared_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    inline = ["systemd-run --unit=poweredge-cloudflared-${self.id} --collect --property=Type=exec --property=RuntimeDirectory=poweredge-cloudflared-${self.id} --property=RuntimeDirectoryMode=0700 --property=RuntimeMaxSec=600 /usr/bin/sleep infinity"]
  }

  provisioner "file" {
    source      = pathexpand(var.poweredge_host_tunnel_token_file)
    destination = "/run/poweredge-cloudflared-${self.id}/token"
  }

  provisioner "remote-exec" {
    inline = ["/usr/local/sbin/install-poweredge-cloudflared '${local.cloudflared_release.url}' '${local.cloudflared_release.sha256}' 'poweredge-cloudflared-${self.id}.service'"]
  }

  depends_on = [remote_file.poweredge_cloudflared]
}
