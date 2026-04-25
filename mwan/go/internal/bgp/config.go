package bgp

// Config holds BGP speaker configuration.
type Config struct {
	Enabled          bool
	ASN              uint32
	RouterID         string
	NextHopV6        string // IPv6 next-hop for announced IPv6 routes (e.g. "3d06:bad:b01:fe::3")
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
