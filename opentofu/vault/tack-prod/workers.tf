locals {
  worker_networks = {
    for key in keys(module.configuration.k3s_workers) : key => module.vault_network.guest_networks[key]
  }
}

resource "proxmox_virtual_environment_file" "worker_network" {
  for_each     = local.worker_networks
  node_name    = module.vault_network.node.name
  datastore_id = module.vault_network.guest.snippet_datastore
  content_type = "snippets"

  source_raw {
    file_name = "tack-k3s-${each.value.vmid}-network.yaml"
    data = yamlencode({
      version  = 2
      renderer = "networkd"
      ethernets = {
        for name, mac in {
          vf0 = each.value.mac
          vf1 = each.value.secondary_mac
          } : name => {
          match    = { macaddress = mac }
          set-name = name
          optional = true
        }
      }
      bonds = {
        eth0 = {
          interfaces  = ["vf0", "vf1"]
          dhcp4       = true
          accept-ra   = true
          nameservers = { addresses = module.vault_network.guest.dns_servers }
          # VM 124 also listens on a private address on this Ethernet segment.
          routes = [{
            to    = "${module.configuration.tack_k3s.guest.ipv6}/128"
            scope = "link"
          }]
          parameters = {
            mode                    = "active-backup"
            primary                 = each.value.primary_member
            mii-monitor-interval    = "100ms"
            fail-over-mac-policy    = "active"
            primary-reselect-policy = "failure"
          }
        }
      }
    })
  }
}

module "k3s_worker_vm" {
  source                = "../../modules/platform/cloud-vm"
  for_each              = module.configuration.k3s_workers
  guest                 = each.value
  smbios_family         = "k3s-worker"
  cloud_image           = null
  shared_cloud_image_id = module.tack_k3s_vm.cloud_image_id
  vm_capacity           = module.configuration.vm_capacity
  tags                  = module.configuration.vm_recipe.tags
  pci_mapping           = proxmox_hardware_mapping_pci.guest[each.key].name
  secondary_pci_mapping = proxmox_hardware_mapping_pci.guest["${each.key}-secondary"].name
  network_data_file_id  = proxmox_virtual_environment_file.worker_network[each.key].id
  started               = !contains(var.stopped_guest_ids, each.value.vmid)
  initialization = {
    public_keys     = local.k3s_preparation.public_keys
    packages        = module.configuration.vm_recipe.packages
    filename_prefix = module.configuration.vm_recipe.filename_prefix
  }
}

module "k3s_worker_base" {
  for_each = module.k3s_worker_vm
  source   = "../../modules/base"

  node = each.value.guest.node
  vmid = each.value.vmid
  kind = "qemu"
  name = each.value.guest.service_key

  networkd_wait_online_enabled = true

  public_keys                              = local.k3s_preparation.public_keys
  restricted_key_lines                     = []
  timezone                                 = local.k3s_preparation.timezone
  login_dir                                = local.k3s_preparation.login_dir
  revision                                 = local.k3s_preparation.revision
  revision_file                            = local.k3s_preparation.revision_file
  scripts_updater_activation_run           = local.k3s_preparation.scripts_updater_activation_run
  scripts_updater_randomized_delay_seconds = local.k3s_preparation.scripts_updater_randomized_delay_seconds
}

module "k3s_agent" {
  source              = "../../modules/platform/k3s-agent"
  for_each            = module.k3s_worker_vm
  guest               = each.value.guest
  server_ipv6         = module.configuration.tack_k3s.guest.ipv6
  artifact            = module.configuration.tack_k3s.artifact
  build_uuid          = each.value.build_uuid
  agent_token         = "K10${module.configuration.tack_k3s.server_ca_hash}::node:${var.vault_tack_k3s_agent_token}"
  agent_token_version = module.configuration.tack_k3s.agent_token_version
  taints              = ["workload=opt-in:NoSchedule"]
  registry_credentials = {
    username = module.configuration.vm_recipe.registry_username
    token    = var.vault_tack_ghcr_pull_token
  }
  registry_credentials_version = module.configuration.tack_k3s_application.registry_credentials_version
  depends_on                   = [module.k3s_worker_base, module.tack_k3s]
}
