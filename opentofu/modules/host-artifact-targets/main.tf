# This file catalogs host artifact targets by exact workspace selector.
# Each entry lists the Proxmox node, the Ansible group variable files in
# Ansible precedence order, the group file that sets mwan_environment, and
# the target's directories, files, and secret fields. The helper renders a
# template:<path> source as a Jinja template. It copies an
# asset:<repository>:<path> source byte for byte from the verified checkout
# of that repository. The repository agoodkind/configs means this repository.
locals {
  group_vars_dir = "ansible/inventory/group_vars"
  unit_dir       = "/etc/systemd/system"

  mwan_directories = [
    { path = "/etc/mwan", mode = "0755", owner = "root", group = "root" },
    { path = "/etc/opnsensectl", mode = "0755", owner = "root", group = "root" },
    { path = "${local.unit_dir}/mwan-ifmgr.service.d", mode = "0755", owner = "root", group = "root" },
    { path = "/var/lib/mwan", mode = "0750", owner = "root", group = "root" },
  ]

  mwan_files = [
    {
      destination = "/etc/mwan/config.toml"
      source      = "template:mwan/config/config-host.toml.j2"
      mode        = "0600"
      owner       = "root"
      group       = "root"
    },
    {
      destination = "/etc/opnsensectl/config.toml"
      source      = "template:opnsense/config.toml.j2"
      mode        = "0600"
      owner       = "root"
      group       = "root"
    },
    {
      destination = "${local.unit_dir}/mwan-ifmgr.service"
      source      = "asset:agoodkind/mwan:gateway/internal/installspec/mwan-ifmgr.service"
      mode        = "0644"
      owner       = "root"
      group       = "root"
    },
    {
      destination = "${local.unit_dir}/mwan-ifmgr.service.d/oob-dhcpv4.conf"
      source      = "asset:agoodkind/configs:proxmox/services/mwan-ifmgr-oob-dhcpv4.conf"
      mode        = "0644"
      owner       = "root"
      group       = "root"
    },
    {
      destination = "${local.unit_dir}/mwan-opnsense-host.service"
      source      = "asset:agoodkind/opnsensectl:cmd/opnsensectl/mwan-opnsense-host.service"
      mode        = "0644"
      owner       = "root"
      group       = "root"
    },
    {
      destination = "${local.unit_dir}/mwan-opnsense-drain.service"
      source      = "asset:agoodkind/opnsensectl:cmd/opnsensectl/mwan-opnsense-drain.service"
      mode        = "0644"
      owner       = "root"
      group       = "root"
    },
  ]

  catalog = {
    "suburban/mwan" = {
      node                   = "hypervisor"
      environment_group_vars = "${local.group_vars_dir}/suburban_servers.yml"
      group_vars = [
        "${local.group_vars_dir}/all/vars.yml",
        "${local.group_vars_dir}/mwan_testbed_all.yml",
        "${local.group_vars_dir}/proxmox_servers.yml",
        "${local.group_vars_dir}/suburban_servers.yml",
      ]
      directories = local.mwan_directories
      files = concat(local.mwan_files, [
        {
          destination = "${local.unit_dir}/mwan-watchdog-testbed.service"
          source      = "template:proxmox/services/mwan-watchdog.service.j2"
          mode        = "0644"
          owner       = "root"
          group       = "root"
        },
        {
          destination = "${local.unit_dir}/mwan-ifmgr.service.d/suburban-host-ipv6-policy.conf"
          source      = "asset:agoodkind/configs:proxmox/services/mwan-ifmgr-suburban-host-ipv6-policy.conf"
          mode        = "0644"
          owner       = "root"
          group       = "root"
        },
      ])
      secret_fields = [merge(local.smtp2go_secret_field, {
        consumer_units = ["mwan-ifmgr.service", "mwan-watchdog-testbed.service"]
      })]
    }
    "vault/mwan" = {
      node                   = "vault"
      environment_group_vars = "${local.group_vars_dir}/vault_servers.yml"
      group_vars = [
        "${local.group_vars_dir}/all/vars.yml",
        "${local.group_vars_dir}/mwan_prod_all.yml",
        "${local.group_vars_dir}/proxmox_servers.yml",
        "${local.group_vars_dir}/vault_servers.yml",
      ]
      directories = local.mwan_directories
      files = concat(local.mwan_files, [
        {
          destination = "${local.unit_dir}/mwan-watchdog.service"
          source      = "template:proxmox/services/mwan-watchdog.service.j2"
          mode        = "0644"
          owner       = "root"
          group       = "root"
        },
      ])
      secret_fields = [merge(local.smtp2go_secret_field, {
        consumer_units = ["mwan-ifmgr.service", "mwan-watchdog.service"]
      })]
    }
  }

  smtp2go_secret_field = {
    name         = "SMTP2GO_API_KEY"
    storage_path = "/etc/mwan/secrets.env"
    mode         = "0600"
    owner        = "root"
    group        = "root"
    pattern      = "^[A-Za-z0-9-]+$"
    min_length   = 16
    max_length   = 128
  }

  # The module reads group variable files only for the selected workspace.
  # A missing group file of another target does not fail the build of the
  # selected target.
  selected = {
    for workspace, entry in local.catalog : workspace => merge(entry, {
      environment = yamldecode(file("${var.repository_root}/${entry.environment_group_vars}")).mwan_environment
      vars = merge([
        for group_file in entry.group_vars : yamldecode(file("${var.repository_root}/${group_file}"))
      ]...)
    }) if workspace == var.workspace
  }
}
