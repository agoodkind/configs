terraform {
  required_providers {
    pveguest = {
      source  = "tofu.home.arpa/agoodkind/pveguest"
      version = ">= 0.1.3"
    }
  }
}

variable "node" {
  description = "The node value is the hypervisor key in the pveguest provider's nodes map."
  type        = string
}

variable "vmid" {
  description = "The vmid value is the guest's Proxmox ID."
  type        = number
}

variable "kind" {
  description = "The kind value selects the guest kind, either lxc or qemu."
  type        = string
}

variable "engine_versions" {
  description = "The engine_versions map assigns an exact Debian version to each Docker engine package. The module installs those versions and runs apt-mark hold to prevent the weekly package updater from upgrading them."
  type        = map(string)

  validation {
    condition = (
      length(var.engine_versions) == 6 &&
      length(setsubtract(keys(var.engine_versions), [
        "containerd.io",
        "docker-buildx-plugin",
        "docker-ce",
        "docker-ce-cli",
        "docker-ce-rootless-extras",
        "docker-compose-plugin",
      ])) == 0
    )
    error_message = "The engine_versions map must contain exactly these keys: containerd.io, docker-buildx-plugin, docker-ce, docker-ce-cli, docker-ce-rootless-extras, and docker-compose-plugin."
  }
}

variable "repository" {
  description = "The repository value supplies the suite and architecture for the Suites and Architectures fields in /etc/apt/sources.list.d/docker.sources. The file configures apt to use https://download.docker.com/linux/debian."
  type = object({
    suite        = string
    architecture = string
  })
}

variable "uplink_interface" {
  description = "The uplink_interface value selects the guest interface where the sysctl file enables proxy_ndp and ndppd answers neighbor solicitations for the Docker bridge prefix."
  type        = string
}

variable "ndp_proxy_prefix" {
  description = "The ndp_proxy_prefix value is the IPv6 prefix of the guest's Docker bridge network. The ndppd service proxies neighbor discovery for addresses in this prefix on the uplink interface."
  type        = string

  validation {
    condition     = can(cidrhost(var.ndp_proxy_prefix, 0)) && strcontains(var.ndp_proxy_prefix, ":")
    error_message = "The ndp_proxy_prefix value must be an IPv6 prefix in CIDR notation."
  }
}

variable "sysctl_path" {
  description = "The sysctl_path value selects the file that enables IPv6 forwarding on all interfaces and proxy_ndp on the uplink interface. The systemd-sysctl.service unit applies the file again after it changes."
  type        = string

  validation {
    condition     = can(regex("^/etc/sysctl\\.d/[^/]+\\.conf$", var.sysctl_path))
    error_message = "The sysctl_path value must be an absolute path to a file ending in .conf directly inside /etc/sysctl.d."
  }
}

module "paths" {
  source = "./paths"

  sysctl_path = var.sysctl_path
}

locals {
  file_paths = module.paths.file_paths

  host_packages = toset(["ca-certificates", "ndppd", "openssl"])

  file_contents = {
    keyring = file("${path.module}/files/docker.asc")
    repository = templatefile("${path.module}/files/docker.sources.tftpl", {
      architecture = var.repository.architecture
      keyring_path = local.file_paths.keyring
      suite        = var.repository.suite
    })
    daemon = file("${path.module}/files/daemon.json")
    sysctl = templatefile("${path.module}/files/sysctl.conf.tftpl", {
      interface = var.uplink_interface
    })
    ndppd = templatefile("${path.module}/files/ndppd.conf.tftpl", {
      interface = var.uplink_interface
      prefix    = var.ndp_proxy_prefix
    })
    prune_service = file("${path.module}/files/docker-prune.service")
    prune_timer   = file("${path.module}/files/docker-prune.timer")
  }
}

resource "pveguest_file" "docker" {
  for_each = local.file_contents

  node = var.node
  vmid = var.vmid
  kind = var.kind

  path    = local.file_paths[each.key]
  content = each.value
}

resource "pveguest_apt_packages" "docker" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  packages = setunion(local.host_packages, keys(var.engine_versions))
  versions = var.engine_versions
  locked   = keys(var.engine_versions)

  depends_on = [
    pveguest_file.docker["keyring"],
    pveguest_file.docker["repository"],
    pveguest_file.docker["daemon"],
  ]
}

resource "pveguest_systemd_unit" "sysctl" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "systemd-sysctl.service"
  enabled = true
  active  = true

  restart_on = {
    sysctl = pveguest_file.docker["sysctl"].write_id
  }
}

resource "pveguest_systemd_unit" "ndppd" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "ndppd.service"
  enabled = true
  active  = true

  restart_on = {
    ndppd = pveguest_file.docker["ndppd"].write_id
  }

  depends_on = [pveguest_apt_packages.docker, pveguest_systemd_unit.sysctl]
}

resource "pveguest_systemd_unit" "docker" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "docker.service"
  enabled = true
  active  = true

  restart_on = {
    daemon = pveguest_file.docker["daemon"].write_id
  }

  depends_on = [pveguest_apt_packages.docker, pveguest_systemd_unit.sysctl]
}

resource "pveguest_systemd_unit" "prune_timer" {
  node = var.node
  vmid = var.vmid
  kind = var.kind

  name    = "docker-prune.timer"
  enabled = true
  active  = true

  restart_on = {
    service = pveguest_file.docker["prune_service"].write_id
    timer   = pveguest_file.docker["prune_timer"].write_id
  }

  depends_on = [pveguest_systemd_unit.docker]
}
