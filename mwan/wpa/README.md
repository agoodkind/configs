# AT&T 802.1X Certificates

⚠️ **Certificates are NOT stored in this git repository for security.**

## Manual Upload Required

Certificates must be manually uploaded to the mwan VM at `/etc/wpa_supplicant/`

### Upload from OPNsense to mwan VM:

```bash
scp agoodkind@router:/conf/opnatt/wpa/*.pem root@mwan.home.goodkind.io:/etc/wpa_supplicant/
ssh root@mwan.home.goodkind.io "chmod 600 /etc/wpa_supplicant/*.pem"
```

## Required Files on mwan VM

- `/etc/wpa_supplicant/ca_cert.pem` - AT&T CA certificate
- `/etc/wpa_supplicant/client_cert.pem` - Client certificate  
- `/etc/wpa_supplicant/private_key.pem` - Private key

The Ansible playbook will check for these files and warn if they're missing.

