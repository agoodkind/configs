terraform {
  required_providers {
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = "0.1.0"
    }
  }
}

variable "node" {
  description = "Key of the hypervisor in the nodes map of the pveguest provider."
  type        = string
}

variable "vmid" {
  description = "Proxmox ID of the guest."
  type        = number
}

variable "kind" {
  description = "Guest kind, lxc or qemu."
  type        = string
}

variable "name" {
  description = "Key of the guest in the Ansible service mapping."
  type        = string
}

variable "hostname" {
  description = "Fully qualified host name of the guest."
  type        = string
}

variable "authorized_keys" {
  description = "Lines of the global authorized_keys file, in any order."
  type        = list(string)
}

variable "login_dir" {
  description = "Directory that an interactive shell in the guest changes to."
  type        = string
}

variable "revision" {
  description = "Guest preparation revision that the Ansible service deploys require."
  type        = string
}

variable "revision_file" {
  description = "Path of the marker file that stores the guest preparation revision."
  type        = string
}

locals {
  authorized_keys_path = "/etc/ssh/authorized_keys.d/authorized_keys"
  sshd_dropin_dir      = "/etc/ssh/sshd_config.d"

  sshd_dropins = {
    password_auth = {
      path    = "${local.sshd_dropin_dir}/99-ansible-disable-password-auth.conf"
      content = file("${path.module}/files/sshd-disable-password-auth.conf")
    }
    global_authorized_keys = {
      path = "${local.sshd_dropin_dir}/99-sshpiper-global-authorized-keys.conf"
      content = templatefile(
        "${path.module}/files/sshd-global-authorized-keys.conf.tftpl",
        { authorized_keys_path = local.authorized_keys_path },
      )
    }
  }
}

resource "pveguest_file" "authorized_keys" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = local.authorized_keys_path
  content = "${join("\n", sort(distinct(var.authorized_keys)))}\n"

  lifecycle {
    # pveguest_file.sshd_dropin["global_authorized_keys"] sets this file as the
    # only key file of sshd. An empty file locks every user out at the next
    # restart of ssh.service.
    precondition {
      condition     = length(var.authorized_keys) > 0
      error_message = "The authorized_keys list for ${var.name} is empty."
    }
  }
}

resource "pveguest_file" "sshd_dropin" {
  for_each = local.sshd_dropins

  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = each.value.path
  content = each.value.content

  # pveguest_file runs this text with `sh -c` in the guest and replaces %s with
  # the quoted path of the temporary file. The temporary file name does not end
  # in .conf, and the Include line of /etc/ssh/sshd_config skips it. The script
  # runs `sshd -t -f` on a candidate config: the temporary file at the position
  # of the destination, every other drop-in, and /etc/ssh/sshd_config without
  # its Include lines.
  validate = templatefile("${path.module}/files/validate-sshd-dropin.sh.tftpl", {
    destination = each.value.path
    dropin_dir  = local.sshd_dropin_dir
  })

  depends_on = [pveguest_file.authorized_keys]
}

resource "pveguest_file" "sshd_tmpfiles" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = "/etc/tmpfiles.d/sshd.conf"
  content = file("${path.module}/files/sshd-tmpfiles.conf")

  depends_on = [pveguest_file.authorized_keys]
}

resource "pveguest_systemd_unit" "ssh" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "ssh.service"
  enabled = true
  active  = true

  # The sha256 of a file is equal before a hand edit and after the rewrite that
  # repairs it. The write_id changes on every write.
  restart_on = merge(
    { authorized_keys = pveguest_file.authorized_keys.write_id },
    { for key, dropin in pveguest_file.sshd_dropin : key => dropin.write_id },
  )
}

resource "pveguest_file" "rsyslog_local_time" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = "/etc/rsyslog.d/50-default-local.conf"
  content = file("${path.module}/files/rsyslog-local-time.conf")
}

resource "pveguest_systemd_unit" "rsyslog" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "rsyslog.service"
  enabled = true
  active  = true

  restart_on = {
    local_time = pveguest_file.rsyslog_local_time.write_id
  }
}

resource "pveguest_file" "systemd_timeout" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = "/etc/systemd/system.conf.d/timeout.conf"
  content = file("${path.module}/files/systemd-timeout.conf")
}

resource "pveguest_file" "login_dir" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  path = "/etc/profile.d/99-prep-guests-login-dir.sh"
  content = templatefile("${path.module}/files/login-dir.sh.tftpl", {
    service   = var.name
    login_dir = var.login_dir
  })
}

# The Ansible service deploys read this file until the last guest is enrolled.
resource "pveguest_file" "revision" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = var.revision_file
  content = "${var.revision}\n"

  depends_on = [
    pveguest_file.authorized_keys,
    pveguest_file.sshd_dropin,
    pveguest_file.sshd_tmpfiles,
    pveguest_systemd_unit.ssh,
    pveguest_file.rsyslog_local_time,
    pveguest_systemd_unit.rsyslog,
    pveguest_file.systemd_timeout,
    pveguest_file.login_dir,
  ]
}
