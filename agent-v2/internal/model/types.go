package model

// NetSupport describes which network protocols an agent supports.
type NetSupport struct {
	IPv4    bool `json:"ipv4"`
	IPv6    bool `json:"ipv6"`
	IPv4NAT bool `json:"ipv4_nat"`
	CN      bool `json:"cn"`
}

// PeerInfo holds the data for a single WireGuard/BGP peer.
type PeerInfo struct {
	ASN              int     `json:"ASN"`
	Contact          string  `json:"Contact"`
	Port             int     `json:"Port"`
	IPv4             string  `json:"IPv4"`
	IPv6             string  `json:"IPv6"`
	PublicKey        string  `json:"PublicKey"`
	PresharedKey     string  `json:"PresharedKey"`
	Clearnet         *string `json:"Clearnet"`
	Channel          string  `json:"Channel"`
	MPBGP            string  `json:"MP-BGP"`
	MTU              int     `json:"MTU"`
	RequestLinkLocal string  `json:"Request-LinkLocal"`
}

// BirdSessionStatus represents a BIRD BGP session state.
type BirdSessionStatus struct {
	State  string            `json:"State"`
	Info   string            `json:"Info"`
	Routes map[string]string `json:"Routes"`
}

// PeerError holds validation issues for a specific peer.
type PeerError struct {
	ASN    int      `json:"ASN"`
	Issues []string `json:"Issues"`
}

// BabelNeighbor represents a Babel routing protocol neighbor.
type BabelNeighbor struct {
	Interface string `json:"interface"`
	Cost      int    `json:"cost"`
	Address   string `json:"address"`
}

// BabelInterface represents a Babel routing protocol interface.
type BabelInterface struct {
	Interface  string  `json:"interface"`
	NextHopV4  *string `json:"next_hop_v4"`
	NextHopV6  *string `json:"next_hop_v6"`
}

// IGPTopologyResult holds the result of an IGP topology query.
type IGPTopologyResult struct {
	Protocol   string           `json:"protocol"`
	Interfaces []BabelInterface `json:"interfaces"`
	Neighbors  []BabelNeighbor  `json:"neighbors"`
	Errors     []string         `json:"errors"`
}

// ConfigResponse holds the fields exposed via /config/get.
type ConfigResponse struct {
	DefaultMTU       int        `json:"DEFAULT_MTU"`
	Open             bool       `json:"OPEN"`
	MaxPeers         int        `json:"MAX_PEERS"`
	MinPeerReq       int        `json:"MIN_PEER_REQUIREMENT"`
	ExtraMsg         string     `json:"EXTRA_MSG"`
	MyDN42IPv4Addr   string     `json:"MY_DN42_IPv4_ADDRESS"`
	VnstatAutoAdd    bool       `json:"VNSTAT_AUTO_ADD"`
	VnstatAutoRemove bool       `json:"VNSTAT_AUTO_REMOVE"`
	BirdCtlPath      string     `json:"BIRD_CTL_PATH"`
	BirdTable4       string     `json:"BIRD_TABLE_4"`
	BirdTable6       string     `json:"BIRD_TABLE_6"`
	NetSupport       NetSupport `json:"NET_SUPPORT"`
}
