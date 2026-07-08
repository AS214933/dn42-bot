package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
)

type ParsedWGConfig struct {
	Port         int
	MTU          int
	MyLLA        string
	PeerLLA      string
	MyULA        string
	PeerULA      string
	PeerIPv4     string
	PublicKey    string
	PresharedKey string
	Clearnet     string
}

func GenerateConfig(peer model.PeerInfo, cfg *config.Config) string {
	ula, ll, ipv4 := classifyPeerAddresses(peer.IPv6, peer.IPv4)

	myLLA := cfg.MyDN42LinkLocalAddress.String()
	if peer.RequestLinkLocal != "" {
		myLLA = peer.RequestLinkLocal
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %d - %s\n", peer.ASN, peer.Contact))
	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("ListenPort = %d\n", peer.Port))
	sb.WriteString("Table = off\n")
	sb.WriteString(fmt.Sprintf("MTU = %d\n", peer.MTU))
	sb.WriteString("PostUp = wg set %i private-key /etc/wireguard/dn42-privatekey\n")

	llPeer := ""
	if ll != "" {
		llPeer = fmt.Sprintf(" peer %s/64", ll)
	}
	sb.WriteString(fmt.Sprintf("PostUp = ip addr add %s/64%s dev %%i\n", myLLA, llPeer))

	ulaPeer := ""
	if ula != "" {
		ulaPeer = fmt.Sprintf(" peer %s/128", ula)
	}
	sb.WriteString(fmt.Sprintf("PostUp = ip addr add %s/128%s dev %%i\n", cfg.MyDN42ULAAddress.String(), ulaPeer))

	ipv4Peer := ""
	if ipv4 != "" {
		ipv4Peer = fmt.Sprintf(" peer %s/32", ipv4)
	}
	sb.WriteString(fmt.Sprintf("PostUp = ip addr add %s/32%s dev %%i\n", cfg.MyDN42IPv4Address.String(), ipv4Peer))

	sb.WriteString("[Peer]\n")
	sb.WriteString(fmt.Sprintf("PublicKey = %s\n", peer.PublicKey))
	if peer.PresharedKey != "" {
		sb.WriteString(fmt.Sprintf("PresharedKey = %s\n", peer.PresharedKey))
	}
	if peer.Clearnet != nil && strings.TrimSpace(*peer.Clearnet) != "" {
		sb.WriteString(fmt.Sprintf("Endpoint = %s\n", strings.TrimSpace(*peer.Clearnet)))
	}
	sb.WriteString("AllowedIPs = 172.20.0.0/14, 10.0.0.0/8, 172.31.0.0/16, fd00::/8, fe80::/64\n")

	return sb.String()
}

func classifyPeerAddresses(ipv6, ipv4 string) (ula, ll, ipv4Addr string) {
	if ip := net.ParseIP(ipv6); ip != nil {
		if ip.To4() == nil {
			ipv6Net := net.IPNet{
				IP:   ip,
				Mask: net.CIDRMask(128, 128),
			}
			_, ulaNet, _ := net.ParseCIDR("fc00::/7")
			_, llNet, _ := net.ParseCIDR("fe80::/64")
			if ulaNet.Contains(ipv6Net.IP) {
				ula = ip.String()
			} else if llNet.Contains(ipv6Net.IP) {
				ll = ip.String()
			}
		}
	}
	if ip := net.ParseIP(ipv4); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			_, net14, _ := net.ParseCIDR("172.20.0.0/14")
			_, net16, _ := net.ParseCIDR("10.127.0.0/16")
			if net14.Contains(ip4) || net16.Contains(ip4) {
				ipv4Addr = ip4.String()
			}
		}
	}
	return
}

var wgConfigRegex = regexp.MustCompile(
	`\[Interface\]\n` +
		`ListenPort = (?P<port>[0-9]+)\n` +
		`Table = off\n` +
		`(?:MTU = (?P<mtu>[0-9]+)\n)?` +
		`PostUp = wg set %i private-key /etc/wireguard/dn42-privatekey\n` +
		`PostUp = ip addr add (?P<my_lla>fe80::[0-9a-f:]+)/64(?: peer (?P<peer_lla>fe80::[0-9a-f:]+)/64)? dev %i\n` +
		`PostUp = ip addr add (?P<my_ula>f[cd][0-9a-f:]+)/[0-9]+(?: peer (?P<peer_ula>f[cd][0-9a-f:]+)/[0-9]+)? dev %i\n` +
		`PostUp = ip addr add (?P<my_ipv4>[0-9.]+)/32(?: peer (?P<peer_ipv4>[0-9.]+)/32)? dev %i\n` +
		`\[Peer\]\n` +
		`PublicKey = (?P<pubkey>.{43}=)\n` +
		`(?:PresharedKey = (?P<psk>.{43}=)\n)?` +
		`(?:Endpoint = (?P<clearnet>.+:[0-9]{1,5})\n)?` +
		`AllowedIPs = `,
)

func ParseConfig(asn int, content string) (*ParsedWGConfig, error) {
	match := wgConfigRegex.FindStringSubmatch(content)
	if match == nil {
		return nil, fmt.Errorf("wireguard config for ASN %d does not match expected format", asn)
	}

	result := &ParsedWGConfig{}
	names := wgConfigRegex.SubexpNames()
	for i, name := range names {
		if i == 0 || name == "" || match[i] == "" {
			continue
		}
		switch name {
		case "port":
			result.Port, _ = strconv.Atoi(match[i])
		case "mtu":
			result.MTU, _ = strconv.Atoi(match[i])
		case "my_lla":
			result.MyLLA = match[i]
		case "peer_lla":
			result.PeerLLA = match[i]
		case "my_ula":
			result.MyULA = match[i]
		case "peer_ula":
			result.PeerULA = match[i]
		case "peer_ipv4":
			result.PeerIPv4 = match[i]
		case "pubkey":
			result.PublicKey = match[i]
		case "psk":
			result.PresharedKey = match[i]
		case "clearnet":
			result.Clearnet = match[i]
		}
	}
	return result, nil
}

func GetPeerNum() (int, int, error) {
	entries, err := os.ReadDir("/etc/wireguard")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read /etc/wireguard: %w", err)
	}
	wgCount := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "dn42-") && strings.HasSuffix(name, ".conf") {
			numStr := name[5 : len(name)-5]
			if _, err := strconv.Atoi(numStr); err == nil {
				wgCount++
			}
		}
	}

	birdEntries, err := os.ReadDir("/etc/bird/dn42_peers")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read /etc/bird/dn42_peers: %w", err)
	}
	birdCount := 0
	for _, e := range birdEntries {
		name := e.Name()
		if strings.HasSuffix(name, ".conf") {
			numStr := name[:len(name)-5]
			if _, err := strconv.Atoi(numStr); err == nil {
				birdCount++
			}
		}
	}

	if wgCount != birdCount {
		return 0, 0, fmt.Errorf("wireguard and bird config count mismatch: wg=%d bird=%d", wgCount, birdCount)
	}
	return wgCount, birdCount, nil
}

func ParseHandshake(output string) (int64, error) {
	output = strings.TrimSpace(output)
	if output == "" || output == "Unable to access interface: No such device" {
		return 0, nil
	}
	parts := strings.Fields(output)
	if len(parts) < 2 {
		return 0, nil
	}
	ts, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, nil
	}
	return ts, nil
}

func GetHandshake(ctx context.Context, iface string) (int64, error) {
	output, err := RunCommand(ctx, "wg", []string{"show", iface, "latest-handshakes"}, 10*time.Second)
	if err != nil {
		return 0, err
	}
	return ParseHandshake(output)
}

func ParseTransfer(output string) (int64, int64, error) {
	output = strings.TrimSpace(output)
	if output == "" || output == "Unable to access interface: No such device" {
		return 0, 0, nil
	}
	parts := strings.Fields(output)
	if len(parts) < 3 {
		return 0, 0, nil
	}
	rx, err1 := strconv.ParseInt(parts[1], 10, 64)
	tx, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, nil
	}
	return rx, tx, nil
}

func GetTransfer(ctx context.Context, iface string) (int64, int64, error) {
	output, err := RunCommand(ctx, "wg", []string{"show", iface, "transfer"}, 10*time.Second)
	if err != nil {
		return 0, 0, err
	}
	return ParseTransfer(output)
}
