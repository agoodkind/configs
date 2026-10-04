locals {
  group_vars = "${path.module}/../../ansible/inventory/group_vars"

  shared_vars = yamldecode(file("${local.group_vars}/all/vars.yml"))
  proxy_vars  = yamldecode(file("${local.group_vars}/proxy_servers.yml"))
  service_mapping = yamldecode(
    file("${local.group_vars}/all/service_mapping.yml")
  ).service_mapping

  enrolled = toset(["clyde_suburban"])

  # The node key is the second label of the guest hostname, for example
  # "suburban" in clyde.suburban.goodkind.io.
  guests = {
    for name in local.enrolled : name => {
      vmid = local.service_mapping[name].vmid
      kind = "lxc"
      node = split(".", local.service_mapping[name].hostname)[1]
    }
  }

  # The service mapping stores the SSH address of a hypervisor under the key
  # <node>_hypervisor.
  nodes = {
    for node in toset([for guest in local.guests : guest.node]) :
    node => { host = local.service_mapping["${node}_hypervisor"].ipv6 }
  }

  github_keys = [
    for line in split("\n", replace(data.http.github_ssh_keys.response_body, "\r", "")) :
    trimspace(line)
    if trimspace(line) != ""
  ]

  # sshd accepts the sshpiper upstream key only from the address of the proxy
  # guest.
  sshpiper_lines = [
    "# Managed by Ansible - Restricted by SSHPiper",
    "from=\"${local.service_mapping.proxy.ipv6}/128\" ${trimspace(local.proxy_vars.sshpiper_upstream_pubkey)}",
  ]
}

data "http" "github_ssh_keys" {
  url = "https://github.com/${local.shared_vars.github_ssh_keys_user}.keys"

  lifecycle {
    postcondition {
      condition     = self.status_code == 200
      error_message = "GitHub returned ${self.status_code} for the SSH keys of ${local.shared_vars.github_ssh_keys_user}."
    }
  }
}

module "base" {
  source   = "./base"
  for_each = local.guests

  node = each.value.node
  vmid = each.value.vmid
  kind = each.value.kind
  name = each.key

  public_keys          = local.github_keys
  restricted_key_lines = local.sshpiper_lines
  login_dir            = local.shared_vars.login_dir
  revision             = local.shared_vars.guest_prep_revision
  revision_file        = local.shared_vars.guest_prep_revision_file
}
