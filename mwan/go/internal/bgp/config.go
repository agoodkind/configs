package bgp

// Config holds BGP speaker configuration.
type Config struct {
	Enabled          bool
	ASN              uint32
	RouterID         string
	KeepaliveSeconds uint32
	HoldSeconds      uint32
	ListenPort       int32

	Neighbors   []NeighborConfig
	NeighborsV6 []NeighborConfig

	Announce AnnounceConfig
}

// NeighborConfig identifies a single BGP peer.
type NeighborConfig struct {
	Address string
}

// AnnounceConfig specifies prefixes to originate.
type AnnounceConfig struct {
	IPv4 []string
	IPv6 []string
}
