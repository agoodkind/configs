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

variable "public_keys" {
  description = "SSH public keys that can log in as any local user of the guest. Ignored when authorized_key_lines is set."
  type        = list(string)
}

variable "restricted_key_lines" {
  description = "Further lines of the global authorized_keys file, such as a key with a from= restriction. Ignored when authorized_key_lines is set."
  type        = list(string)
}

variable "authorized_key_lines" {
  description = "Lines that replace the whole authorized_keys file of the guest. A line can start with key options such as command=\"...\",restrict. Null selects public_keys and restricted_key_lines."
  type        = list(string)
  default     = null

  # pveguest_file.sshd_dropin["global_authorized_keys"] sets one file as the
  # only key file of sshd. A file without a valid key locks every user out at
  # the next restart of ssh.service. The check covers the lines that the guest
  # receives: authorized_key_lines when set, otherwise public_keys.
  validation {
    condition = (
      length(var.authorized_key_lines == null ? var.public_keys : var.authorized_key_lines) > 0 &&
      alltrue([
        for key in(var.authorized_key_lines == null ? var.public_keys : var.authorized_key_lines) : can(regex(
          "^(([^ \"]|\"[^\"]*\")+ )?(ssh-[A-Za-z0-9@._+-]+|ecdsa-[A-Za-z0-9@._+-]+|sk-[A-Za-z0-9@._+-]+) [A-Za-z0-9+/]+={0,3}( .*)?$",
          key,
        ))
      ])
    )
    error_message = "The key file needs at least one line, and each line must be an SSH public key with optional key options."
  }
}

variable "packages" {
  description = "Names of the apt packages that every guest has installed."
  type        = set(string)
  default = [
    "apache2-utils",
    "curl",
    "gh",
    "git",
    "git-lfs",
    "gpg",
    "htop",
    "jq",
    "locales",
    "msmtp",
    "msmtp-mta",
    "neovim",
    "net-tools",
    "openssh-server",
    "ripgrep",
    "rsyslog",
    "tcpdump",
    "tree",
    "tzdata",
    "unzip",
    "wget",
    "yq",
  ]
}

variable "login_dir" {
  description = "Directory that an interactive shell in the guest changes to."
  type        = string
}

variable "scripts_installer" {
  description = "The install-updater download uses this agoodkind/scripts commit and SHA-256 checksum. The module downloads the installer without running it."
  type = object({
    commit = string
    sha256 = string
  })
  default = {
    commit = "73a3121464cba781dae2e435ce03da69613ae235"
    sha256 = "9a807dbfd3c2dfeecf91f85fe6e221f0245ec09872e97b29e380a0b20ea53d31"
  }
}

variable "debug_command_file" {
  description = "The module installs the generic debug helper at this path. The default is /usr/local/bin/debug."
  type        = string
  default     = "/usr/local/bin/debug"
}

variable "console_autologin" {
  description = "LXC guests use root autologin on container-getty@1.service when true. The default is true."
  type        = bool
  default     = true
}

variable "timezone" {
  description = "The guest timezone uses this zoneinfo name for the /etc/localtime link. Null omits the link resource."
  type        = string
  default     = null
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

  common_dir = "${path.module}/../../../common"

  locale_files = {
    locale_gen = {
      path    = "/etc/locale.gen"
      content = file("${path.module}/files/locale.gen")
    }
    default_locale = {
      path    = "/etc/default/locale"
      content = file("${path.module}/files/default-locale")
    }
  }

  timezones = var.timezone == null ? toset([]) : toset([var.timezone])

  kind_packages = var.kind == "qemu" ? toset(["qemu-guest-agent"]) : toset([])

  console_autologin_units = (
    var.kind == "lxc" && var.console_autologin
    ? toset(["container-getty@1.service"])
    : toset([])
  )

  package_updater_files = {
    script = {
      path    = "/usr/local/sbin/package-updater.sh"
      content = file("${local.common_dir}/scripts/package-updater.sh")
      mode    = "0755"
    }
    service = {
      path    = "/etc/systemd/system/package-updater.service"
      content = file("${local.common_dir}/services/package-updater.service")
      mode    = "0644"
    }
    timer = {
      path    = "/etc/systemd/system/package-updater.timer"
      content = file("${local.common_dir}/timers/package-updater.timer")
      mode    = "0644"
    }
  }

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

  path = local.authorized_keys_path
  content = "${join("\n", (
    var.authorized_key_lines == null
    ? sort(distinct(concat(var.public_keys, var.restricted_key_lines)))
    : var.authorized_key_lines
  ))}\n"
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

  depends_on = [
    pveguest_file.authorized_keys,
    pveguest_apt_packages.base,
  ]
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

  depends_on = [pveguest_apt_packages.base]
}

resource "pveguest_apt_packages" "base" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  packages = setunion(var.packages, local.kind_packages)
}

resource "pveguest_file" "rsyslog_local_time" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = "/etc/rsyslog.d/50-default-local.conf"
  content = file("${path.module}/files/rsyslog-local-time.conf")

  depends_on = [pveguest_apt_packages.base]
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

  depends_on = [pveguest_apt_packages.base]
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

resource "pveguest_download" "scripts_installer" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  url    = "https://raw.githubusercontent.com/agoodkind/scripts/${var.scripts_installer.commit}/install-updater"
  sha256 = var.scripts_installer.sha256
  path   = "/usr/local/sbin/install-updater"
  mode   = "0755"

  depends_on = [pveguest_apt_packages.base]
}

resource "pveguest_systemd_unit" "networkd_wait_online" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "systemd-networkd-wait-online.service"
  enabled = false
  active  = false
}

resource "pveguest_file" "debug_command" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  path = var.debug_command_file
  content = templatefile("${path.module}/files/debug-generic.sh.tftpl", {
    service = var.name
  })
  mode = "0755"
}

resource "pveguest_file" "console_autologin" {
  for_each = local.console_autologin_units

  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = "/etc/systemd/system/${each.key}.d/override.conf"
  content = file("${path.module}/files/container-getty-autologin.conf")
}

resource "pveguest_systemd_unit" "console_getty" {
  for_each = local.console_autologin_units

  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = each.key
  enabled = true
  active  = true

  restart_on = {
    override = pveguest_file.console_autologin[each.key].write_id
  }
}

resource "pveguest_file" "environment" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = "/etc/profile.d/98-prep-guests-environment.sh"
  content = file("${path.module}/files/environment.sh")
}

resource "pveguest_file" "pip" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = "/etc/pip.conf"
  content = file("${path.module}/files/pip.conf")
}

resource "pveguest_file" "locale" {
  for_each = local.locale_files

  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = each.value.path
  content = each.value.content

  depends_on = [pveguest_apt_packages.base]
}

resource "pveguest_link" "localtime" {
  for_each = local.timezones

  node = var.node
  vmid = var.vmid
  kind = var.kind

  path   = "/etc/localtime"
  target = "/usr/share/zoneinfo/${each.key}"

  depends_on = [pveguest_apt_packages.base]
}

resource "pveguest_file" "package_updater" {
  for_each = local.package_updater_files

  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = each.value.path
  content = each.value.content
  mode    = each.value.mode
}

resource "pveguest_systemd_unit" "package_updater_timer" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "package-updater.timer"
  enabled = true
  active  = true

  restart_on = {
    service = pveguest_file.package_updater["service"].write_id
    timer   = pveguest_file.package_updater["timer"].write_id
  }

  depends_on = [pveguest_file.package_updater]
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
    pveguest_apt_packages.base,
    pveguest_file.rsyslog_local_time,
    pveguest_systemd_unit.rsyslog,
    pveguest_file.systemd_timeout,
    pveguest_file.login_dir,
    pveguest_download.scripts_installer,
    pveguest_systemd_unit.networkd_wait_online,
    pveguest_file.debug_command,
    pveguest_file.console_autologin,
    pveguest_systemd_unit.console_getty,
    pveguest_file.environment,
    pveguest_file.pip,
    pveguest_file.locale,
    pveguest_link.localtime,
    pveguest_file.package_updater,
    pveguest_systemd_unit.package_updater_timer,
  ]
}
