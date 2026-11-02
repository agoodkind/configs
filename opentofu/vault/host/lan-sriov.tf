resource "remote_file" "vault_lan_sriov" {
  conn {
    host  = local.overlay_hosts.vault.ssh_host
    user  = "root"
    agent = true
  }

  path        = "/etc/systemd/system/sriov-vf-lan.service"
  content     = file("${local.repository_root}/vault/sriov-vf-lan.service")
  permissions = "0644"
}

resource "terraform_data" "vault_lan_sriov_reload" {
  triggers_replace = [sha256(remote_file.vault_lan_sriov.content)]

  connection {
    type  = "ssh"
    host  = local.overlay_hosts.vault.ssh_host
    user  = "root"
    agent = true
  }

  # Restarting this unit would reset the MAC of the active router VF.
  provisioner "remote-exec" {
    inline = ["systemctl daemon-reload"]
  }
}
