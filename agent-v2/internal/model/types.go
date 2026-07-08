package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

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

func (p *PeerInfo) UnmarshalJSON(data []byte) error {
	var raw struct {
		ASN              int             `json:"ASN"`
		Contact          string          `json:"Contact"`
		Port             json.RawMessage `json:"Port"`
		IPv4             string          `json:"IPv4"`
		IPv6             string          `json:"IPv6"`
		PublicKey        string          `json:"PublicKey"`
		PresharedKey     json.RawMessage `json:"PresharedKey"`
		Clearnet         json.RawMessage `json:"Clearnet"`
		Channel          string          `json:"Channel"`
		MPBGP            string          `json:"MP-BGP"`
		MTU              json.RawMessage `json:"MTU"`
		RequestLinkLocal string          `json:"Request-LinkLocal"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	clearnet, err := decodeOptionalClearnet(raw.Clearnet)
	if err != nil {
		return err
	}
	port, err := decodeOptionalInt(raw.Port, "Port")
	if err != nil {
		return err
	}
	mtu, err := decodeOptionalInt(raw.MTU, "MTU")
	if err != nil {
		return err
	}
	presharedKey, err := decodeOptionalString(raw.PresharedKey, "PresharedKey")
	if err != nil {
		return err
	}

	*p = PeerInfo{
		ASN:              raw.ASN,
		Contact:          raw.Contact,
		Port:             port,
		IPv4:             raw.IPv4,
		IPv6:             raw.IPv6,
		PublicKey:        raw.PublicKey,
		PresharedKey:     presharedKey,
		Clearnet:         clearnet,
		Channel:          raw.Channel,
		MPBGP:            raw.MPBGP,
		MTU:              mtu,
		RequestLinkLocal: raw.RequestLinkLocal,
	}
	return nil
}

func decodeOptionalClearnet(raw json.RawMessage) (*string, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return nil, nil
	}

	var endpoint string
	if err := json.Unmarshal(raw, &endpoint); err == nil {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			return nil, nil
		}
		return &endpoint, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil && !enabled {
		return nil, nil
	}

	return nil, fmt.Errorf("Clearnet must be a string, null, or false")
}

func decodeOptionalString(raw json.RawMessage, field string) (string, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return "", nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", fmt.Errorf("%s must be a string or null", field)
	}
	return strings.TrimSpace(text), nil
}

func decodeOptionalInt(raw json.RawMessage, field string) (int, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return 0, nil
	}

	var number int
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, fmt.Errorf("%s must be an integer, quoted integer, or null", field)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	if _, err := fmt.Sscanf(text, "%d", &number); err != nil {
		return 0, fmt.Errorf("%s must be an integer, quoted integer, or null", field)
	}
	if fmt.Sprintf("%d", number) != text {
		return 0, fmt.Errorf("%s must be an integer, quoted integer, or null", field)
	}
	return number, nil
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
	Interface string  `json:"interface"`
	NextHopV4 *string `json:"next_hop_v4"`
	NextHopV6 *string `json:"next_hop_v6"`
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
