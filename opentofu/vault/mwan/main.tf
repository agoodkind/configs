locals { repository_root = abspath("${path.module}/../../..") }

module "contracts" {
  source          = "../../modules/contracts"
  repository_root = local.repository_root
}

locals {
  service_mapping         = module.contracts.service_mapping
  proxmox_token_principal = module.contracts.proxmox_token_principal
  nodes = {
    vault = {
      endpoint  = regex("^https://[^/]+", var.proxmox_endpoint)
      api_token = "${local.proxmox_token_principal}=${var.vault_proxmox_token_secret}"
      insecure  = true
      node_name = "vault"
    }
  }
  retirement_files = {
    library = {
      path    = "/usr/local/lib/configs/retire-routing-common.sh"
      content = file("${local.repository_root}/common/scripts/retire-routing-common.sh")
      mode    = "0644"
    }
    script = {
      path    = "/usr/local/sbin/retire-mwan-routing.sh"
      content = file("${path.module}/retire-routing.sh")
      mode    = "0755"
    }
    unit = {
      path    = "/etc/systemd/system/retire-mwan-routing.service"
      content = file("${path.module}/retire-routing.service")
      mode    = "0644"
    }
    forwarding = {
      path    = "/etc/sysctl.d/99-client-network.conf"
      content = file("${local.repository_root}/common/config/client-network.conf")
      mode    = "0644"
    }
  }
}

resource "pveguest_file" "retirement" {
  for_each = local.retirement_files

  node = "vault"
  vmid = local.service_mapping.mwan_failover.vmid
  kind = "lxc"

  path    = each.value.path
  content = each.value.content
  mode    = each.value.mode
}

resource "pveguest_systemd_unit" "routing_retired" {
  node = "vault"
  vmid = local.service_mapping.mwan_failover.vmid
  kind = "lxc"

  name    = "retire-mwan-routing.service"
  enabled = false
  active  = true

  restart_on = {
    for name, artifact in pveguest_file.retirement : name => artifact.write_id
  }
}

module "vault" {
  source = "../../modules/vault/mwan"

  providers = { proxmox = proxmox }

  service_mapping = local.service_mapping

  depends_on = [pveguest_systemd_unit.routing_retired]
}

removed {
  from = module.vault.proxmox_virtual_environment_vm.mwan
  lifecycle { destroy = true }
}

# The retirement unit stops the old client and removes its configuration.
removed {
  from = pveguest_file.failover_dhcp6
  lifecycle { destroy = false }
}

removed {
  from = pveguest_systemd_unit.failover_dhcp6
  lifecycle { destroy = false }
}
