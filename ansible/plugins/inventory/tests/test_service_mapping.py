from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

import yaml
from ansible import constants
from ansible.errors import AnsibleError
from ansible.inventory.manager import InventoryManager
from ansible.parsing.dataloader import DataLoader
from ansible.plugins.loader import inventory_loader
from pydantic import BaseModel, ConfigDict

PLUGIN_DIRECTORY = Path(__file__).resolve().parents[1]
CANONICAL_HOST = "tack-prod-001.vault.goodkind.io"
SHORT_HOST = "tack-prod-001"
GUEST_ADDRESS = "2001:db8::117"
GUEST_VMID = 117
NODE_NAME = "vault"
LEGACY_HOST = "legacy.example.test"


class Service(BaseModel):
    model_config = ConfigDict(strict=True)

    hostname: str = CANONICAL_HOST
    ipv6: str = GUEST_ADDRESS
    vmid: int | str | bool | None = GUEST_VMID
    proxmox_node: str | None = NODE_NAME
    kind: str | None = "lxc"


def load_inventory(directory: Path, service: Service) -> InventoryManager:
    mapping = directory / "mapping.yml"
    mapping.write_text(
        yaml.safe_dump({"service_mapping": {"tack": service.model_dump(exclude_none=True)}}),
        encoding="utf-8",
    )
    source = directory / "service_mapping.yml"
    source.write_text(
        yaml.safe_dump({"plugin": "service_mapping", "mapping_file": mapping.name}),
        encoding="utf-8",
    )
    short_source = directory / "short.yml"
    short_source.write_text(
        yaml.safe_dump({"all": {"hosts": {SHORT_HOST: {"api_host": True}}}}),
        encoding="utf-8",
    )
    return InventoryManager(loader=DataLoader(), sources=[str(short_source), str(source)])


class ServiceMappingInventoryTests(unittest.TestCase):
    def setUp(self) -> None:
        inventory_loader.add_directory(str(PLUGIN_DIRECTORY))
        self.previous_plugins = constants.INVENTORY_ENABLED
        self.previous_strict = constants.INVENTORY_ANY_UNPARSED_IS_FAILED
        constants.INVENTORY_ENABLED = ["service_mapping", "yaml"]
        constants.INVENTORY_ANY_UNPARSED_IS_FAILED = True
        self.addCleanup(setattr, constants, "INVENTORY_ENABLED", self.previous_plugins)
        self.addCleanup(
            setattr, constants, "INVENTORY_ANY_UNPARSED_IS_FAILED", self.previous_strict
        )

    def test_canonical_host_retains_proxmox_metadata_beside_short_host(self) -> None:
        for kind in ("lxc", "qemu"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                inventory = load_inventory(Path(directory), Service(kind=kind))
                host = inventory.get_host(CANONICAL_HOST)
                if host is None:
                    self.fail("The inventory did not create the canonical guest host.")
                variables = host.get_vars()
                self.assertEqual(variables["ansible_host"], GUEST_ADDRESS)
                self.assertEqual(variables["ansible_proxmox_host"], NODE_NAME)
                self.assertEqual(variables["ansible_proxmox_vmid"], GUEST_VMID)
                self.assertIsInstance(variables["ansible_proxmox_vmid"], int)
                self.assertEqual(variables["service_vmid"], GUEST_VMID)
                self.assertEqual(variables["proxmox_vmtype"], kind)
                self.assertIn(host, inventory.groups["tack_servers"].hosts)
                self.assertIsNotNone(inventory.get_host(SHORT_HOST))

    def test_guest_without_explicit_node_keeps_existing_inventory_variables(self) -> None:
        service = Service(hostname=LEGACY_HOST, proxmox_node=None, kind=None)
        with tempfile.TemporaryDirectory() as directory:
            inventory = load_inventory(Path(directory), service)
            host = inventory.get_host(LEGACY_HOST)
            if host is None:
                self.fail("The inventory did not create the existing guest host.")
            variables = host.get_vars()
            self.assertEqual(variables["service_vmid"], GUEST_VMID)
            self.assertNotIn("ansible_proxmox_host", variables)
            self.assertNotIn("ansible_proxmox_vmid", variables)
            self.assertNotIn("proxmox_vmtype", variables)

    def test_explicit_node_rejects_missing_or_invalid_guest_metadata(self) -> None:
        services = [
            Service(proxmox_node=""),
            Service(vmid=None),
            Service(vmid="117"),
            Service(vmid=True),
            Service(vmid=0),
            Service(kind=None),
            Service(kind="container"),
            Service(hostname=""),
            Service(ipv6=""),
        ]
        for service in services:
            with self.subTest(service=service), tempfile.TemporaryDirectory() as directory:
                with self.assertRaises(AnsibleError):
                    load_inventory(Path(directory), service)


if __name__ == "__main__":
    unittest.main()
