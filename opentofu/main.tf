data "http" "github_ssh_keys" {
  url = "https://github.com/${var.github_ssh_keys_user}.keys"
}

module "suburban" {
  source = "./suburban"

  providers = {
    proxmox = proxmox.suburban
  }

  ssh_keys = trimspace(data.http.github_ssh_keys.response_body)

  tack_qa_vm_id            = var.tack_qa_vm_id
  tack_qa_hostname         = var.tack_qa_hostname
  tack_qa_ipv6_address     = var.tack_qa_ipv6_address
  tack_qa_ipv6_gateway     = var.tack_qa_ipv6_gateway
  tack_qa_bridge           = var.tack_qa_bridge
  tack_qa_mac_address      = var.tack_qa_mac_address
  tack_qa_disk_size_gb     = var.tack_qa_disk_size_gb
  tack_qa_memory_mb        = var.tack_qa_memory_mb
  tack_qa_cpu_cores        = var.tack_qa_cpu_cores
  tack_qa_tags             = var.tack_qa_tags
  tack_qa_template_file_id = var.tack_qa_template_file_id
  tack_qa_datastore_id     = var.tack_qa_datastore_id
}

module "vault" {
  source = "./vault"

  providers = {
    proxmox = proxmox
  }

  ssh_keys = trimspace(data.http.github_ssh_keys.response_body)
}
