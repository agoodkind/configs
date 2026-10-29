variable "guest_keys" { type = set(string) }

variable "shared_vars" { type = any }

variable "service_mapping" { type = any }

variable "proxy_vars" { type = any }

variable "timezone" {
  type     = string
  nullable = false
}

locals {

  shared_vars     = var.shared_vars
  proxy_vars      = var.proxy_vars
  service_mapping = var.service_mapping

  enrolled = var.guest_keys



  # The site is the second label of the guest hostname, for example "suburban"
  # in clyde.suburban.goodkind.io.
  guest_sites = {
    for name in local.enrolled : name => split(".", local.service_mapping[name].hostname)[1]
  }

  # Hostname site labels select keys in the pveguest provider's nodes map.
  # The home site uses the vault node. An unlisted site causes evaluation to
  # fail.
  site_nodes = {
    home     = "vault"
    suburban = "suburban"
  }


  # A guest listed here receives exactly these lines as its authorized_keys
  # file, without the GitHub keys and the sshpiper lines. The map key is the
  # service mapping name of the guest.
  authorized_key_lines_overrides = {}

  # This map supplies key-file paths that the module appends after the global
  # key-file path for individual guests.
  # Each map key is the service mapping name of the guest.
  # ISP simulators use /etc/ssh/authorized_keys.d/mwan-testbed.
  # The MWAN testbed deploy writes that file.
  authorized_keys_files_overrides = {
    isp_astound_suburban = local.isp_simulator_key_files
    isp_att_suburban     = local.isp_simulator_key_files
    isp_mbrains_suburban = local.isp_simulator_key_files
    isp_routed_suburban  = local.isp_simulator_key_files
    isp_webpass_suburban = local.isp_simulator_key_files
  }

  isp_simulator_key_files = ["/etc/ssh/authorized_keys.d/mwan-testbed"]

  scripts_updater_timer_default = {
    activation_run           = true
    randomized_delay_seconds = 300
  }

  scripts_updater_timer_overrides = {
    mwan_suburban = {
      activation_run           = false
      randomized_delay_seconds = 3600
    }
  }


  guest_kind_values = ["lxc", "qemu"]

  guest_kinds = {
    for name in local.enrolled : name => try(local.service_mapping[name].kind, "lxc")
  }

  guests = {
    for name in local.enrolled : name => {
      vmid                        = local.service_mapping[name].vmid
      kind                        = local.guest_kinds[name]
      node                        = local.site_nodes[local.guest_sites[name]]
      timezone                    = var.timezone
      authorized_key_lines        = try(local.authorized_key_lines_overrides[name], null)
      extra_authorized_keys_files = try(local.authorized_keys_files_overrides[name], [])
      scripts_updater_timer = try(
        local.scripts_updater_timer_overrides[name],
        local.scripts_updater_timer_default,
      )
    }
  }

  mail_relay_guests = setintersection(local.enrolled, toset([
    "clyde_suburban",
    "dns64_suburban",
    "mwan",
    "mwan_failover",
    "mwan_failover_suburban",
    "mwan_suburban",
    "seaweedfs_suburban",
    "tack",
    "tack_app2",
    "tack_app2_suburban",
    "tack_data1",
    "tack_data1_suburban",
    "tack_data2",
    "tack_data2_suburban",
    "tack_data3",
    "tack_data3_suburban",
    "tack_qa_suburban",
    "tack_search1",
    "tack_search1_suburban",
  ]))

  default_mail_password_version = 1

  mail_relays = {
    for name in local.mail_relay_guests : name => {
      username = "host-${trim(replace(lower(local.service_mapping[name].hostname), "/[^a-z0-9]+/", "-"), "-")}"
      host     = local.shared_vars.smtp_host
      port     = tonumber(local.shared_vars.smtp_port)
      from     = "${split(".", local.service_mapping[name].hostname)[0]}@${local.shared_vars.smtp_from_domain}"
      password_version = try(
        local.service_mapping[name].mail_password_version,
        local.default_mail_password_version,
      )
    }
  }






  # sshd accepts the sshpiper upstream key only from the address of the proxy
  # guest.
  sshpiper_lines = [
    "# Managed by Ansible - Restricted by SSHPiper",
    "from=\"${local.service_mapping.proxy.ipv6}/128\" ${trimspace(local.proxy_vars.sshpiper_upstream_pubkey)}",
  ]
}

output "guests" { value = local.guests }

output "guest_kinds" { value = local.guest_kinds }

output "guest_kind_values" { value = local.guest_kind_values }

output "mail_relays" { value = local.mail_relays }

output "sshpiper_lines" { value = local.sshpiper_lines }
