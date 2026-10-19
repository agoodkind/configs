# Configure PowerEdge NIC firmware

The firmware workspace manages the BCM57504 OEM attributes declared in the
[PowerEdge module](../../../opentofu/firmware/poweredge/main.tf). It uses a
separate encrypted state file. configsctl locks PowerEdge while it rotates a
provider credential and runs an authenticated OpenTofu command.

The Dell provider can reboot the host when applying NIC settings. This workspace
uses `InMaintenanceWindowOnReset`, which stages the settings for a separately
controlled reset within an explicit maintenance window. Do not substitute
`OnReset`: the provider reboots the host before submitting that mode's NIC patch.

## Prepare credentials and discovered identifiers

The service accounts use separate credentials:

| Account | Permission | Credential |
| --- | --- | --- |
| `tofu-firmware` | Operator can configure server components and reset the host. iDRAC does not restrict this permission to individual NICs or attributes. | configsctl generates a new password for each authenticated OpenTofu command. It does not save this password. |
| `tofu-rotator` | Custom mask `0x5` grants Login and Configure Users. This account can modify any user, including administrator passwords, privileges, and SSH keys. | Only the encrypted vault key `vault_poweredge_idrac_rotator_password` persists. The credential adapter uses this password internally. |

1. Supply the rotator password through the encrypted Configs vault key
   `vault_poweredge_idrac_rotator_password`. Follow the existing
   [secret input procedure](../../../opentofu/backend.md#add-a-secret).
   Do not add a persistent firmware account password.

   configsctl acquires the PowerEdge lock before rotating the firmware account
   for `plan`, `refresh`, `import`, `apply`, or `destroy`. The adapter
   verifies the generated credential before starting OpenTofu and retains the
   lock until the command ends. OpenTofu receives only the firmware account
   credential through sensitive, ephemeral variables. The rotator password
   remains inside configsctl. `fmt`, `validate`, and `show` do not rotate.

   The adapter verifies the [pinned public iDRAC certificate](../../../opentofu/firmware/idrac-ca.pem)
   and its server name.
   Replace that certificate only after independently confirming a certificate
   change on the intended controller.

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

   Initialization warns that the REST provider's signing key has expired.
   Current OpenTofu accepts the signed release. Keep signature verification
   enabled.

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

   Initial imports include every OEM attribute. The first plan can reduce the
   imported map to the declared attributes and fill scheduling fields even when
   the declared values already match the NIC. Applying that normalization still
   submits a NIC job and requires coordinated maintenance.

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

3. Read the active attributes with a fresh OpenTofu plan. Confirm the requested
   settings on every BCM57504 function. Compare the virtualization mode,
   advertised VF count, PF vector limit, and MAC addresses with their values
   before maintenance. Do not rely on the Dell resource state alone.

The [tagged NIC implementation](https://github.com/dell/terraform-provider-redfish/blob/v1.6.2/redfish/provider/resource_redfish_nic.go)
defines the apply modes and reset behavior. The
[OEM state conversion](https://github.com/dell/terraform-provider-redfish/blob/v1.6.2/redfish/provider/resource_redfish_nic_schema_helper.go#L377-L414)
preserves configured values during refresh. The
[REST data source](https://github.com/magodo/terraform-provider-restful/blob/v0.25.2/docs/data-sources/resource.md)
supports direct GET requests and limits the stored response to selected attributes.

## Recover an interrupted command

The firmware password remains valid until the next rotation. Ending the command
removes its process memory; it does not revoke the BMC password.

Run the required OpenTofu command again after resolving its reported error.
configsctl uses the rotator password to replace the firmware password before
starting the next authenticated command. Recovery does not require the previous
firmware password. A saved-plan apply generates a new credential rather than
reusing the credential from its plan command.

A failed rotation or failed verification stops the command before OpenTofu
starts. The adapter does not change account privileges, root, NIC settings, or
host power.
