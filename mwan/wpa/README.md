# AT&T 802.1X Certificates

Certificates are not stored in this git repository for security reasons.

They must be manually copied from OPNsense to the mwan VM before `wpa_supplicant` can
authenticate. On OPNsense, the certs live under `/conf/opnatt/wpa/`. On the mwan VM,
they belong in `/etc/wpa_supplicant/` with `600` permissions. Copy them using `scp`
from the OPNsense console via the direct IPv6 address.

The three required files are the AT&T CA certificate, the client certificate, and the
private key. The Ansible playbook checks for their presence and warns if any are missing.
