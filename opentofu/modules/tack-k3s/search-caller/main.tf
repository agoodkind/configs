terraform {
  required_providers {
    remote = { source = "tenstad/remote", version = ">= 0.2.1" }
  }
}

variable "host" {
  type = string
}

variable "caller" {
  type = string
  validation {
    condition     = can(cidrhost("${var.caller}/128", 0)) && strcontains(var.caller, ":") && !strcontains(var.caller, "/")
    error_message = "Supply the single k3s node IPv6 address without a prefix."
  }
}

locals {
  # remote_file builds the SSH address as <host>:<port>.
  remote_host = strcontains(var.host, ":") ? "[${var.host}]" : var.host

  files = {
    script = {
      path    = "/usr/local/sbin/tack-search-caller.py"
      content = file("${path.module}/tack-search-caller.py")
      mode    = "0755"
    }
    unit = {
      path = "/etc/systemd/system/tack-search-caller.service"
      content = join("\n", [
        "[Unit]",
        "Description=Authorize the Tack k3s search caller",
        "AssertPathExists=/etc/tack-search-proxy/traefik.yml",
        "",
        "[Service]",
        "Type=oneshot",
        "RemainAfterExit=yes",
        "TimeoutStartSec=60",
        "ExecStart=/usr/bin/python3 /usr/local/sbin/tack-search-caller.py ${var.caller}",
        "ExecStartPost=/usr/bin/timeout 45s /usr/bin/systemctl restart tack-search-proxy.service",
        "",
      ])
      mode = "0644"
    }
  }
}

resource "remote_file" "caller" {
  for_each = local.files
  conn {
    host  = local.remote_host
    user  = "root"
    agent = true
  }
  path        = each.value.path
  content     = each.value.content
  permissions = each.value.mode
}

resource "terraform_data" "activate" {
  triggers_replace = { for key, installed in remote_file.caller : key => sha256(installed.content) }
  connection {
    type  = "ssh"
    host  = var.host
    user  = "root"
    agent = true
  }
  provisioner "remote-exec" {
    inline = [
      "/usr/bin/systemctl daemon-reload",
      "/usr/bin/systemctl restart tack-search-caller.service",
    ]
  }
}
