variable "sysctl_path" {
  description = "The sysctl_path value selects the file that enables IPv6 forwarding on all interfaces and proxy_ndp on the uplink interface. The systemd-sysctl.service unit applies the file again after it changes."
  type        = string
}

output "file_paths" {
  description = "The file_paths map associates each managed file key with its absolute path in the guest. Root import blocks use these paths to build pveguest_file import IDs."
  value = {
    keyring       = "/etc/apt/keyrings/docker.asc"
    repository    = "/etc/apt/sources.list.d/docker.sources"
    daemon        = "/etc/docker/daemon.json"
    sysctl        = var.sysctl_path
    ndppd         = "/etc/ndppd.conf"
    prune_service = "/etc/systemd/system/docker-prune.service"
    prune_timer   = "/etc/systemd/system/docker-prune.timer"
  }
}
