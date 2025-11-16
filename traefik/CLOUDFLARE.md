# Cloudflare Integration & IPv6 Configuration

This guide explains how Traefik is configured to work with Cloudflare and prioritize IPv6.

## Overview

Your Traefik setup is configured to:

1. **Only accept traffic from Cloudflare IPs** - Blocks direct access, forcing all traffic through Cloudflare
2. **Prioritize IPv6** - IPv6 ranges are listed first everywhere
3. **Get real client IPs** - Properly handles X-Forwarded-For headers from Cloudflare
4. **Automatic DDoS protection** - Cloudflare filters malicious traffic before it reaches your server

## Architecture

```
Internet User
    ↓
Cloudflare Edge (IPv6 preferred)
    ↓ (Only Cloudflare IPs allowed)
Your Traefik Server
    ↓
Backend Services
```

## How It Works

### 1. IP Whitelisting Middleware

**Location:** `traefik/dynamic/middlewares.yml.j2`

```yaml
cloudflare-only:
  ipWhiteList:
    sourceRange:
      # IPv6 first (Cloudflare prefers IPv6)
      - "2400:cb00::/32"
      - "2606:4700::/32"
      # ... more IPv6 ranges
      # IPv4 ranges
      - "173.245.48.0/20"
      # ... more IPv4 ranges
```

**What it does:**
- Rejects any connection NOT from a Cloudflare IP
- Lists IPv6 ranges first (Cloudflare will use IPv6 if available)
- Returns HTTP 403 Forbidden for non-Cloudflare IPs

**Why this matters:**
- Prevents attackers from bypassing Cloudflare by accessing your server directly
- Forces all traffic through Cloudflare's DDoS protection
- Hides your real server IP

### 2. Trusted Forwarded Headers

**Location:** `traefik/traefik.yml.j2`

```yaml
entryPoints:
  websecure:
    forwardedHeaders:
      trustedIPs:
        # IPv6 first
        - "2400:cb00::/32"
        # ... Cloudflare IP ranges
```

**What it does:**
- Tells Traefik to trust the `X-Forwarded-For` header from Cloudflare IPs
- Extracts the real client IP address
- Enables proper logging of actual visitor IPs (not Cloudflare's IP)

**Why this matters:**
- Your logs show the real visitor IP, not Cloudflare's edge server IP
- Rate limiting works correctly per actual user
- GeoIP and access controls work with real IPs

### 3. Route Configuration

**Location:** `traefik/dynamic/routes.yml.j2`

```yaml
ansible-public:
  middlewares:
    - cloudflare-only  # Apply IP restriction
    - secure-headers
```

**What it does:**
- Applies the `cloudflare-only` middleware to all public routes
- Ensures every public service only accepts Cloudflare traffic

## IPv6 Priority

IPv6 ranges are listed **first** in all configurations:

1. **Middlewares** - IPv6 IPs listed before IPv4
2. **ForwardedHeaders** - IPv6 trusted IPs first
3. **Connection preference** - Cloudflare uses IPv6 when available

**Benefits:**
- Better performance (IPv6 often has lower latency)
- Future-proof (IPv6 is the modern standard)
- More IP addresses available
- Better routing in many cases

## Cloudflare DNS Setup

### Required DNS Configuration

**For proxied (orange cloud) services:**

```
ansible.public.home.goodkind.io   AAAA  2001:db8::1   [Proxied]
ansible.public.home.goodkind.io   A     203.0.113.1   [Proxied]
```

**Or use wildcard:**

```
*.public.home.goodkind.io   AAAA  2001:db8::1   [Proxied]
*.public.home.goodkind.io   A     203.0.113.1   [Proxied]
```

**Important:**
- Enable "Proxied" (orange cloud) in Cloudflare
- List AAAA (IPv6) record first for IPv6 preference
- Both IPv4 and IPv6 recommended for compatibility

## Testing Your Configuration

### 1. Verify Cloudflare is Proxying

```bash
# Should show Cloudflare IPs, not your server IP
dig +short ansible.public.home.goodkind.io AAAA
dig +short ansible.public.home.goodkind.io A
```

### 2. Test Direct Access (Should Fail)

```bash
# Direct access to your server IP should be blocked
curl -I http://YOUR_SERVER_IP
# Should get: 403 Forbidden
```

### 3. Test Through Cloudflare (Should Work)

```bash
# Access via domain should work
curl -I https://ansible.public.home.goodkind.io
# Should get: 200 OK
```

### 4. Verify Real IP Logging

```bash
# Check Traefik logs - should show real visitor IPs
ansible traefik_servers -m shell -a "tail -20 /var/log/traefik/access.log | jq .ClientAddr"
```

### 5. Test IPv6 Preference

```bash
# Force IPv6 connection
curl -6 -I https://ansible.public.home.goodkind.io

# Force IPv4 connection
curl -4 -I https://ansible.public.home.goodkind.io
```

## Different Middleware Usage

### Public Services (via Cloudflare)

```yaml
ansible-public:
  middlewares:
    - cloudflare-only  # Only Cloudflare IPs
    - secure-headers
```

Use for: Services exposed to the internet through Cloudflare

### Internal Services (direct access)

```yaml
internal-dashboard:
  middlewares:
    - internal-only  # Only internal network IPs
    - dashboard-auth
```

Use for: Services only accessible from your LAN

### Mixed Access (advanced)

```yaml
# Option 1: Public but with auth
public-with-auth:
  middlewares:
    - cloudflare-only
    - dashboard-auth  # Still require password
    - secure-headers

# Option 2: Both Cloudflare and internal
flexible-access:
  # Don't use middleware - let both through
  # Handle auth at application level
```

## Updating Cloudflare IP Ranges

Cloudflare occasionally updates their IP ranges. Update your config periodically:

### Manual Update

```bash
cd /usr/share/configs/traefik
./update-cloudflare-ips.sh
```

This fetches current ranges from Cloudflare and displays them formatted for copying into your config.

### Automatic Update (Recommended)

Add to crontab:

```bash
# Check monthly for Cloudflare IP updates
0 0 1 * * /usr/share/configs/traefik/update-cloudflare-ips.sh | mail -s "Cloudflare IP Update Available" admin@example.com
```

### Update Process

1. Run `./update-cloudflare-ips.sh`
2. Copy the output
3. Update both files:
   - `traefik/dynamic/middlewares.yml.j2` (cloudflare-only middleware)
   - `traefik/traefik.yml.j2` (forwardedHeaders.trustedIPs)
4. Commit and deploy:
   ```bash
   git add .
   git commit -m "Update Cloudflare IP ranges"
   git push
   ansible-playbook ../ansible/playbooks/update-traefik-config.yml
   ```

## Cloudflare Settings Recommendations

### SSL/TLS

**Cloudflare Dashboard → SSL/TLS**
- Mode: **Full (strict)**
- Edge Certificates: Enabled
- Always Use HTTPS: On
- Minimum TLS Version: TLS 1.2

### Security

**Security → Settings**
- Security Level: Medium or High
- Challenge Passage: 30 minutes
- Browser Integrity Check: On

### Speed

**Speed → Optimization**
- Auto Minify: HTML, CSS, JS
- Brotli: On
- HTTP/2: On
- HTTP/3 (with QUIC): On
- IPv6 Compatibility: On

### Network

**Network**
- IPv6 Compatibility: **On** (Important!)
- WebSockets: On
- gRPC: On (if needed)

## Troubleshooting

### Issue: 403 Forbidden on all requests

**Cause:** IP whitelist blocking legitimate traffic

**Fix:**
1. Check if you're accessing through Cloudflare (not direct IP)
2. Verify Cloudflare proxy is enabled (orange cloud)
3. Update Cloudflare IP ranges

```bash
# Temporarily remove cloudflare-only middleware to test
# Edit routes.yml.j2, remove cloudflare-only from middlewares
```

### Issue: Logs show Cloudflare IPs, not real visitors

**Cause:** `forwardedHeaders.trustedIPs` not configured

**Fix:**
1. Verify `traefik.yml.j2` has forwardedHeaders section
2. Ensure Cloudflare IPs are listed
3. Redeploy configuration

### Issue: IPv4 being used instead of IPv6

**Cause:** IPv6 not prioritized or not available

**Fix:**
1. Ensure your server has IPv6 connectivity
2. Add AAAA record in Cloudflare DNS
3. List AAAA record before A record
4. Enable IPv6 in Cloudflare settings

```bash
# Test IPv6 connectivity
ping6 google.com
```

### Issue: Direct IP access still works

**Cause:** Middleware not applied to route

**Fix:**
1. Verify route has `cloudflare-only` in middlewares list
2. Check middleware is defined in middlewares.yml.j2
3. Redeploy configuration

### Issue: Some Cloudflare regions failing

**Cause:** Outdated Cloudflare IP ranges

**Fix:**
```bash
./update-cloudflare-ips.sh
# Update config files with new ranges
```

## Security Best Practices

1. ✅ **Always use Cloudflare proxy** (orange cloud)
2. ✅ **Enable Full (strict) SSL** in Cloudflare
3. ✅ **Block direct IP access** with cloudflare-only middleware
4. ✅ **Update Cloudflare IPs monthly**
5. ✅ **Use strong passwords** for basic auth
6. ✅ **Enable rate limiting** for sensitive endpoints
7. ✅ **Monitor access logs** for suspicious activity
8. ✅ **Set up Cloudflare firewall rules** for additional protection

## Advanced: Cloudflare WAF Rules

Add custom rules in Cloudflare:

```
# Block specific countries
(ip.geoip.country in {"CN" "RU"}) and http.request.uri.path contains "/admin"

# Rate limit aggressive bots
(cf.threat_score gt 50) and http.request.uri.path contains "/api"

# Allow only specific ASNs for admin
(http.request.uri.path contains "/admin") and not (ip.geoip.asnum in {AS15169 AS16509})
```

## Reference

### Cloudflare IP Lists

- IPv4: https://www.cloudflare.com/ips-v4
- IPv6: https://www.cloudflare.com/ips-v6

### Traefik Documentation

- ForwardedHeaders: https://doc.traefik.io/traefik/routing/entrypoints/#forwarded-headers
- IPWhiteList: https://doc.traefik.io/traefik/middlewares/http/ipwhitelist/

### Cloudflare Documentation

- Restore Original IP: https://developers.cloudflare.com/fundamentals/get-started/reference/cloudflare-ip-addresses/
- SSL Modes: https://developers.cloudflare.com/ssl/origin-configuration/ssl-modes/

## Summary

Your Traefik configuration:

✅ Only accepts Cloudflare IPs (blocks direct access)
✅ Prioritizes IPv6 connections
✅ Properly handles real client IPs
✅ Integrates with Cloudflare DDoS protection
✅ Maintains audit trails with real visitor IPs
✅ Easy to update when Cloudflare changes IPs

This setup gives you enterprise-grade security and performance! 🚀
