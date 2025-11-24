# PowerDNS Secondary Setup Plan

## MVP (Current)

- [x] Deploy PowerDNS as secondary/slave
- [x] Configure zone transfers from BIND primary
- [x] Set up TSIG authentication
- [x] Add DNS records (NS, A, AAAA) for pdns hostname
- [x] Configure update-policy for nsupdate

## Post-MVP (Later)
- Make PowerDNS primary:
- pdnsutil zone load to migrate from BIND
- create a very minimal UI to interact with the API https://raw.githubusercontent.com/PowerDNS/pdns/master/docs/http-api/swagger/authoritative-api-swagger.yaml (in svelte?)