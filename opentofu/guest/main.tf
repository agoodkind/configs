locals {
  group_vars = "${path.module}/../../ansible/inventory/group_vars"

  shared_vars = yamldecode(file("${local.group_vars}/all/vars.yml"))
  proxy_vars  = yamldecode(file("${local.group_vars}/proxy_servers.yml"))
  service_mapping = yamldecode(
    file("${local.group_vars}/all/service_mapping.yml")
  ).service_mapping

  enrolled = toset(["clyde_suburban"])

  proxmox_token_principal = "${local.shared_vars.proxmox_api_user}!${local.shared_vars.proxmox_token_id}"

  # The site is the second label of the guest hostname, for example "suburban"
  # in clyde.suburban.goodkind.io.
  guest_sites = {
    for name in local.enrolled : name => split(".", local.service_mapping[name].hostname)[1]
  }

  # The provider nodes map is keyed by site. The provider puts the key in the
  # API path unless the entry sets node_name. The suburban node is named
  # hypervisor, and the other nodes are named like their site.
  proxmox_node_name_overrides = {
    suburban = "hypervisor"
  }

  # A guest listed here receives exactly these lines as its authorized_keys
  # file, without the GitHub keys and the sshpiper lines. The map key is the
  # service mapping name of the guest.
  authorized_key_lines_overrides = {}

  proxmox_token_secrets = {
    suburban  = var.vault_suburban_testbed_pve_token_secret
    poweredge = var.vault_poweredge_pve_token_secret
    vault     = var.vault_proxmox_token_secret
  }

  guests = {
    for name in local.enrolled : name => {
      vmid                 = local.service_mapping[name].vmid
      kind                 = "lxc"
      node                 = local.guest_sites[name]
      authorized_key_lines = try(local.authorized_key_lines_overrides[name], null)
    }
  }

  # The poweredge node receives host and container resources that this
  # workspace declares without enrolling a guest.
  host_only_sites = toset(["poweredge"])

  node_sites = setunion(toset(values(local.guest_sites)), local.host_only_sites)

  # The service mapping stores the address of a hypervisor under the key
  # <site>_hypervisor, as ipv6 or as ipv4. The certificate of the API does not
  # cover that address.
  node_hosts = {
    for site in local.node_sites :
    site => try(
      "[${local.service_mapping["${site}_hypervisor"].ipv6}]",
      local.service_mapping["${site}_hypervisor"].ipv4,
    )
  }

  nodes = {
    for site in local.node_sites :
    site => {
      endpoint  = "https://${local.node_hosts[site]}:8006"
      api_token = "${local.proxmox_token_principal}=${local.proxmox_token_secrets[site]}"
      insecure  = true
      node_name = try(local.proxmox_node_name_overrides[site], null)
    }
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

  authorized_key_lines = each.value.authorized_key_lines
  public_keys          = each.value.authorized_key_lines == null ? local.github_keys : []
  restricted_key_lines = each.value.authorized_key_lines == null ? local.sshpiper_lines : []
  login_dir            = local.shared_vars.login_dir
  revision             = local.shared_vars.guest_prep_revision
  revision_file        = local.shared_vars.guest_prep_revision_file
}
