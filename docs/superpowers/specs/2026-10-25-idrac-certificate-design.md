# PowerEdge iDRAC DNS and certificate design

Publish `idrac.poweredge.management.collingwood.goodkind.io` in Cloudflare DNS and install a publicly trusted certificate on iDRAC. A daily job on the PowerEdge Proxmox host renews the certificate. The job uses the internal USB connection to iDRAC.

## Required behavior

The public DNS record returns the existing management address, `3d06:bad:b01:300::efb`, without Cloudflare proxying. WARP configuration remains outside this change. Browser and firmware clients use the selected hostname and verify its certificate.

iDRAC generates the HTTPS private key and certificate signing request. The private key remains inside iDRAC. The unchanged `vancluever/acme` provider obtains the certificate from Let's Encrypt using Cloudflare DNS verification. Its separate account key remains in encrypted OpenTofu state. The existing `magodo/restful` provider imports the signed certificate directly. The configuration never invokes `SSLResetCfg` to replace a certificate.

The OS-to-iDRAC USB connection uses `usb-p2p` mode. The controller uses `169.254.1.1/24`, and the host uses `169.254.1.2/24` on the `idrac` interface. Configure only that interface. Preserve the management LAN, default routes, bridges, and VLANs.

The job resolves the certificate hostname to the USB address within its own service environment. Public DNS continues to return the management LAN address. Internet access remains necessary for Cloudflare and Let's Encrypt.

## Renewal ownership

Use a separate OpenTofu configuration containing the certificate's DNS record, signing request, ACME account, certificate, and import operation. The job must not plan or apply BIOS, NIC, router, guest, or application resources.

A persistent systemd timer starts the job daily and checks for a missed run after startup. Renewal pauses while Proxmox is shut down. Run `configsctl` and OpenTofu from fixed versions. Keep the runtime configuration independent from the operator's full repository checkout.

Use an encrypted local state file under the job's private state directory on PowerEdge. Preserve this directory during package updates. OpenTofu's state lock prevents overlapping certificate applies.

The job also acquires the existing PowerEdge deployment lock. Its inventory places `poweredge` in `tofu_lock_hosts` with `ansible_connection: local`. Configsctl accepts the local target only when its name matches the actual operating-system hostname. The local lock implementation does not require SSH.

## Credentials and initial installation

The job uses the dedicated `tofu-certificate` iDRAC account with login and manager configuration permissions. Do not give it user-management or server power-control permissions. Generate the account password automatically. Use the existing PowerEdge ACME DNS credential or a credential with equivalent limited DNS scope. Do not copy the full repository vault to PowerEdge.

Package only this job's secrets in a separate encrypted vault. Use systemd's protected runtime credentials to unlock it for the service. Supply the iDRAC password as an ephemeral input and Cloudflare credentials through the provider environment. Keep password values out of resource state, command arguments, source files, terminal output, and logs. The unchanged ACME provider stores its account key in encrypted state.

Initial installation verifies iDRAC using the pinned factory certificate and its name, `idrac-J9P5L44`. Remove the temporary factory-certificate connection after installing the signed certificate. The renewal job and firmware clients then use the selected hostname and system trust. Do not disable TLS verification for authenticated requests.

iDRAC requires nonempty organization and location fields in its signing request. Use placeholders for unused identity fields. Let's Encrypt excludes those fields from the issued certificate.

Configure the USB connection and dedicated login through the authenticated Redfish API. Retain only the selected values as source configuration. Certificate installation can briefly interrupt iDRAC access; it must not reset the host operating system.

## Completion evidence

Verify the public DNS answer against Cloudflare's authoritative servers. Verify the certificate's hostname, chain, fingerprint, and validity through both the management address and the USB connection. Confirm that the HTTPS private key matches by completing a verified TLS connection.

The job compares canonical PEM text for the issued and installed leaf certificates. Its digest output hashes PEM text. TLS tools report a fingerprint of the binary DER certificate.

Run the installed renewal service. Its next plan must not request another certificate or reset iDRAC when the certificate does not require renewal. Confirm timer enablement, daily scheduling, missed-run handling, and successful service exit. Inspect the job's staged inputs and state permissions. Report source changes, runtime deployment, and remaining limitations separately.

## Supporting references

- [Let's Encrypt DNS verification](https://letsencrypt.org/docs/challenge-types/#dns-01-challenge) permits certificate issuance without exposing iDRAC to public HTTP requests.
- [Let's Encrypt certificate subject checks](https://github.com/letsencrypt/boulder/blob/main/ra/ra.go) reject issued certificates that include country, organization, organizational unit, locality, or province fields.
- [Dell signing requests](https://www.dell.com/support/kbdoc/en-us/000197810/how-to-generate-a-csr-using-idrac9-redfish) use iDRAC's Redfish certificate service.
- [Dell certificate resource implementation](https://github.com/dell/terraform-provider-redfish/blob/v1.6.2/redfish/provider/resource_certificate.go) requires replacement and resets certificates during deletion. Use direct import instead.
- [REST operation resource](https://registry.terraform.io/providers/magodo/restful/0.25.2/docs/resources/operation) supports an import request without a delete operation.
- [OpenTofu state encryption](https://opentofu.org/docs/language/state/encryption/) protects stored state but does not conceal its values from an authorized OpenTofu process.
- [Persistent systemd timers](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html#Persistent=) check for missed runs after startup.
