"""
Ansible Dynamic Inventory Plugin: service_mapping

Reads service_mapping from group_vars/all/service_mapping.yml and creates:
  - A host for each service using its hostname
  - A group named {service}_servers containing that host
  - Host vars: ansible_host (IPv6), service_ipv4 (if defined)

Usage:
  Create inventory/service_mapping.yml with:
    plugin: service_mapping
    mapping_file: group_vars/all/service_mapping.yml  # relative to inventory/

This eliminates the need for manual group membership and allows playbooks to
target groups like `proxy_servers` without relying on Proxmox discovery.
"""

from __future__ import absolute_import, division, print_function

__metaclass__ = type

DOCUMENTATION = """
name: service_mapping
plugin_type: inventory
short_description: Creates inventory groups from service_mapping.yml
description:
  - Reads a YAML file containing service definitions
  - Creates {service}_servers groups with the service hostname as a member
  - Sets ansible_host to the IPv6 address for each host
options:
  plugin:
    description: Token that ensures this is a source file for the plugin.
    required: true
    choices: ['service_mapping']
  mapping_file:
    description: Path to service_mapping.yml (relative to inventory directory)
    required: false
    default: group_vars/all/service_mapping.yml
  create_all_services_group:
    description: Create an 'all_services' group containing all service hosts
    required: false
    type: bool
    default: true
"""

EXAMPLES = """
# inventory/service_mapping.yml
plugin: service_mapping
mapping_file: group_vars/all/service_mapping.yml
"""

import os
import yaml

from ansible.errors import AnsibleParserError
from ansible.plugins.inventory import BaseInventoryPlugin


class InventoryModule(BaseInventoryPlugin):
    NAME = "service_mapping"

    def verify_file(self, path):
        """Verify that the config file is valid for this plugin."""
        valid = False
        if super().verify_file(path):
            if path.endswith((".yml", ".yaml")):
                valid = True
        return valid

    def parse(self, inventory, loader, path, cache=True):
        """Parse the inventory source and populate the inventory."""
        super().parse(inventory, loader, path, cache)

        # Read the plugin configuration
        self._read_config_data(path)

        # Get the mapping file path (relative to inventory directory)
        mapping_file = self.get_option("mapping_file")
        if mapping_file is None:
            mapping_file = "group_vars/all/service_mapping.yml"

        # Resolve the absolute path
        inventory_dir = os.path.dirname(path)
        mapping_path = os.path.join(inventory_dir, mapping_file)

        if not os.path.exists(mapping_path):
            raise AnsibleParserError(
                f"service_mapping file not found: {mapping_path}"
            )

        # Load the service mapping
        with open(mapping_path, "r") as f:
            try:
                data = yaml.safe_load(f)
            except yaml.YAMLError as e:
                raise AnsibleParserError(
                    f"Failed to parse {mapping_path}: {e}"
                )

        if not data or "service_mapping" not in data:
            raise AnsibleParserError(
                f"No 'service_mapping' key found in {mapping_path}"
            )

        service_mapping = data["service_mapping"]

        # Optionally create all_services group
        create_all_group = self.get_option("create_all_services_group")
        if create_all_group is None:
            create_all_group = True

        if create_all_group:
            self.inventory.add_group("all_services")

        # Process each service
        for service_name, service_data in service_mapping.items():
            hostname = service_data.get("hostname")
            ipv6 = service_data.get("ipv6")
            ipv4 = service_data.get("ipv4")

            if not hostname:
                self.display.warning(
                    f"Service '{service_name}' has no hostname, skipping"
                )
                continue

            if not ipv6:
                self.display.warning(
                    f"Service '{service_name}' has no ipv6, skipping"
                )
                continue

            # Create the group: {service_name}_servers
            group_name = f"{service_name}_servers"
            self.inventory.add_group(group_name)

            # Add the host using the hostname
            self.inventory.add_host(hostname, group=group_name)

            # Set host variables
            self.inventory.set_variable(hostname, "ansible_host", ipv6)
            self.inventory.set_variable(hostname, "service_name", service_name)
            self.inventory.set_variable(hostname, "service_ipv6", ipv6)

            if ipv4:
                self.inventory.set_variable(hostname, "service_ipv4", ipv4)

            # Add to all_services group
            if create_all_group:
                self.inventory.add_host(hostname, group="all_services")
