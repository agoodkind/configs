resource "terraform_data" "retire_mwan" {
  triggers_replace = [filesha256("${path.module}/retire-mwan.sh")]

  connection {
    type  = "ssh"
    host  = local.overlay_hosts.vault.ssh_host
    user  = "root"
    agent = true
  }

  provisioner "remote-exec" {
    script = "${path.module}/retire-mwan.sh"
  }
}
