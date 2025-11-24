# PowerDNS Secondary Setup Plan

## MVP (Current)

- [x] Deploy PowerDNS as secondary/slave
- [x] Configure zone transfers from BIND primary
- [x] Set up TSIG authentication
- [x] Add DNS records (NS, A, AAAA) for pdns hostname
- [x] Configure update-policy for nsupdate

## Post-MVP (Later)

### PowerDNS-Admin Web Interface

- [ ] Install PowerDNS-Admin (Python Flask application)
- [ ] Configure PostgreSQL database for PowerDNS-Admin
- [ ] Set up reverse proxy (Traefik) for web UI access
- [ ] Configure authentication (LDAP/local users)
- [ ] Connect PowerDNS-Admin to PowerDNS API
- [ ] Test zone management via web UI
- [ ] Set up SSL/TLS certificates for web UI

#### PowerDNS-Admin Setup Details

- **Application**: PowerDNS-Admin (<https://github.com/PowerDNS-Admin/PowerDNS-Admin>)
- **Requirements**: Python 3, PostgreSQL, PowerDNS API access
- **Access**: Via Traefik reverse proxy
- **Features**: Zone management, record editing, DNSSEC management, user management
