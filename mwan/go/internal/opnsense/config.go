package opnsense

// Config holds OPNsense API connection settings.
type Config struct {
	URL       string // e.g. "https://192.168.1.1"
	APIKey    string
	APISecret string
	Insecure  bool // skip TLS verify (testbed)
}

// BGPConfig describes the desired BGP configuration for OPNsense FRR.
type BGPConfig struct {
	ASN       uint32
	RouterID  string
	Neighbors []BGPNeighborConfig
}

// BGPNeighborConfig describes a single BGP neighbor to create.
type BGPNeighborConfig struct {
	Address     string
	RemoteAS    uint32
	Keepalive   int
	Holddown    int
	BFD         bool
	RouteMapIn  string // "PREFER-PRIMARY" or "PREFER-BACKUP"
	Description string
}
