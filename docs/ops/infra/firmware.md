# Configure PowerEdge NIC firmware

The firmware workspace manages the BCM57504 OEM attributes declared in the
[PowerEdge module](../../../opentofu/firmware/poweredge/main.tf). It uses a
separate encrypted state file and locks PowerEdge during an apply.

The Dell provider can reboot the host when applying NIC settings. This workspace
uses `InMaintenanceWindowOnReset`, which stages the settings for a separately
controlled reset within an explicit maintenance window. Do not substitute
`OnReset`: the provider reboots the host before submitting that mode's NIC patch.

## Prepare credentials and discovered identifiers

1. Supply the existing iDRAC password through the Configs vault key
   `vault_poweredge_idrac_password` or the environment variable
   `TF_VAR_vault_poweredge_idrac_password`. The password variable is sensitive
   and ephemeral. The provider uses the `poweredge` alias instead of storing
   credentials in each NIC resource. Follow the existing
   [secret input procedure](../../../opentofu/backend.md#add-a-secret).

2. Compare the authenticated Redfish network inventory with the function IDs in
   the workspace. Confirm that the functions belong to the BCM57504 and that
   each `NetworkAttributesRegistry` allows the declared values. Preserve the
   existing virtualization mode, advertised VF count, PF vector limit, and MAC addresses.

3. Prepare a private variable file. Set
   `poweredge_maintenance_window.start_time` to an RFC 3339
   timestamp with its UTC offset and set `duration` in seconds. The workspace
   does not supply a maintenance window default.

   The existing iDRAC certificate may require
   `poweredge_idrac_insecure = true`. Set that option explicitly only after
   confirming the target certificate.

## Import and review existing settings

Import reads the existing NIC resource without submitting a firmware job.
Coordinate the state import with the operator who controls PowerEdge maintenance.

1. Initialize the firmware workspace from the repository root:

   ```bash
   ./configsctl tofu firmware init
   ```

2. Import each discovered function with its adapter ID and the provider alias:

   ```bash
   ./configsctl tofu firmware import \
     -var-file=/secure/poweredge-firmware.tfvars \
     'module.poweredge.redfish_network_adapter.bcm57504["<function-id>"]' \
     '{"redfish_alias":"poweredge","system_id":"System.Embedded.1","network_adapter_id":"<adapter-id>","network_device_function_id":"<function-id>"}'
   ```

3. Save and inspect a plan before submitting any changes:

   ```bash
   ./configsctl tofu firmware plan \
     -var-file=/secure/poweredge-firmware.tfvars \
     -out=/secure/poweredge-firmware.tfplan
   ./configsctl tofu firmware show /secure/poweredge-firmware.tfplan
   ```

   Review the exact function IDs, changed OEM attributes, and maintenance window.
   Removing unrelated attributes from the provider's imported attribute map does
   not reset those attributes: the NIC update request contains only the declared
   map entries. Do not apply a plan that changes an unrelated NIC setting.

## Apply and verify active settings

The Dell resource preserves configured OEM values instead of refreshing them
from the NIC. Its resource diff cannot detect an external attribute change.
The plan's `observed_nic_attributes` output reads the actual DellNetworkAttributes
endpoints through a separate REST data source with provider-level credentials.
That data source stores only the selected NIC attributes.

The active-value check warns when the NIC differs from the declaration. It also
warns before a planned change becomes active. The warning does not block a
legitimate update or automatically repair the Dell resource's refresh behavior.

1. Apply the reviewed plan only during coordinated maintenance:

   ```bash
   ./configsctl tofu firmware apply /secure/poweredge-firmware.tfplan
   ```

2. Read the resulting iDRAC job and pending settings. Perform the separately
   controlled host reset within the configured maintenance window. A scheduled
   job does not prove active firmware settings.

3. Read active NIC attributes through the existing iDRAC SSH key:

   ```bash
   ssh -6 -o BatchMode=yes -o HostKeyAlias=10.230.0.113 \
     root@3d06:bad:b01:300::efb racadm get NIC.DeviceLevelConfig
   ```

   Confirm the requested settings on every BCM57504 function. Compare the
   virtualization mode, advertised VF count, PF vector limit, and MAC addresses
   with their values before maintenance. Read the NIC attributes again with a
   fresh OpenTofu plan; do not rely on the resource state alone.

The [tagged NIC implementation](https://github.com/dell/terraform-provider-redfish/blob/v1.6.2/redfish/provider/resource_redfish_nic.go)
defines the apply modes and reset behavior. The
[OEM state conversion](https://github.com/dell/terraform-provider-redfish/blob/v1.6.2/redfish/provider/resource_redfish_nic_schema_helper.go#L377-L414)
preserves configured values during refresh. The
[REST data source](https://github.com/magodo/terraform-provider-restful/blob/v0.25.2/docs/data-sources/resource.md)
supports direct GET requests and limits the stored response to selected attributes.
