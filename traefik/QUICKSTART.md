# What You Need to Customize

Simple list of values to find/replace in your configuration.

## 1. File: `ansible/inventory/group_vars/traefik_servers.yml`

```yaml
traefik_ingress_domain: "home.public.goodkind.io"   # Client-facing URLs (what users access)
traefik_upstream_domain: "home.goodkind.io"          # Backend service URLs (where services run)
traefik_acme_email: "admin@goodkind.io"              # Your email for Let's Encrypt
cloudflare_api_token: "CHANGE_ME"                     # Your Cloudflare API token
```

## 2. File: `ansible/inventory/hosts`

```ini
[traefik_servers]
proxy.home.goodkind.io  # Your Traefik server hostname or IP
```

## 3. File: `traefik/dynamic/routes.yml.j2`

Add/modify routes for your services:

```yaml
# Example - customize for each service
myservice-public:
  rule: "Host(`myservice.{{ traefik_ingress_domain }}`)"  # Service name
  service: myservice

services:
  myservice:
    loadBalancer:
      servers:
        - url: "http://myservice.{{ traefik_upstream_domain }}:8080"  # Port number
```

## 4. File: `traefik/dynamic/middlewares.yml.j2`

Update IP ranges (lines 33-38):

```yaml
sourceRange:
  - "127.0.0.1/32"
  - "10.250.0.0/24"           # YOUR actual network range
  - "::1/128"
  - "3d06:bad:b01:0::/64"     # YOUR actual IPv6 range
```

## 5. Cloudflare DNS

Create DNS records:

```
*.public.home.goodkind.io   AAAA  YOUR_IPV6   [Proxied]
*.public.home.goodkind.io   A     YOUR_IPV4   [Proxied]
```

---

## That's It!

Everything else uses these variables automatically via Jinja2 templating.
