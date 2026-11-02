resource "terraform_data" "retire_opnsense" {
  triggers_replace = [filesha256("${path.module}/retire-opnsense.sh")]

  connection {
    type  = "ssh"
    host  = local.overlay_hosts.vault.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    script = "${path.module}/retire-opnsense.sh"
  }

  depends_on = [terraform_data.guest_network_activate]
}
