# Scoped Proxmox privileges from agoodkind/proxmox-overlays. The overlay adds
# VM.Config.Nesting, VM.Config.Keyctl, VM.Config.Vsock, and
# Sys.ACME.Account.* to the hypervisor. A custom role can then grant one of
# them to a non-root API token.
#
# The resource fetches the repository at one commit onto the hypervisor and
# runs `pve-overlay apply`. The script writes patched module copies to
# /etc/perl, installs a dpkg hook that runs it again after each package
# upgrade, and restarts the Proxmox API services.
locals {
  proxmox_overlays_archive = "https://github.com/agoodkind/proxmox-overlays/archive"
  proxmox_overlays_dir     = "/usr/local/share/proxmox-overlays"

  # A new commit here reruns the provisioner.
  proxmox_overlays_commit = "691e9280c44d51d6ea2498a31de4e2d716928d6f"
}

resource "terraform_data" "proxmox_overlays" {
  triggers_replace = [local.proxmox_overlays_commit]

  connection {
    type  = "ssh"
    host  = local.service_mapping.suburban_hypervisor.ipv6
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    inline = [
      "set -eu",
      "rm -rf ${local.proxmox_overlays_dir}.new",
      "mkdir -p ${local.proxmox_overlays_dir}.new",
      "curl --fail --silent --show-error --location ${local.proxmox_overlays_archive}/${local.proxmox_overlays_commit}.tar.gz | tar --extract --gzip --strip-components=1 --directory ${local.proxmox_overlays_dir}.new",
      "rm -rf ${local.proxmox_overlays_dir}",
      "mv ${local.proxmox_overlays_dir}.new ${local.proxmox_overlays_dir}",
      "${local.proxmox_overlays_dir}/pve-overlay apply",
    ]
  }
}
