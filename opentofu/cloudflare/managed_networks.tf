# A managed network tells the Cloudflare One Client which physical location a
# device is sitting on. The client opens TLS to the configured host and port and
# compares the certificate fingerprint. A match sets the current network name,
# which the device profiles then match on.
#
# The beacon address must be unreachable from anywhere else, otherwise the check
# passes while the device is away and the wrong profile applies. Cloudflare
# removes the beacon address from every device profile to enforce that, so it
# must be an address no remote user needs.

resource "cloudflare_zero_trust_device_managed_networks" "berylax_lan" {
  account_id = var.account_id
  name       = "Berylax LAN"
  type       = "tls"

  config = {
    tls_sockaddr = var.berylax_beacon_sockaddr
    sha256       = var.berylax_beacon_sha256
  }

  lifecycle {
    prevent_destroy = true
  }
}
