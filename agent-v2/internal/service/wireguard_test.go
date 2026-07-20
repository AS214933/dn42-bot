package service

import (
	"net"
	"strings"
	"testing"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
)

func strPtr(s string) *string { return &s }

func defaultTestPeer() model.PeerInfo {
	return model.PeerInfo{
		ASN:              4242421234,
		Contact:          "test@example.com",
		Port:             21234,
		IPv4:             "172.22.167.101",
		IPv6:             "fe80::1234",
		PublicKey:        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		PresharedKey:     "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		Clearnet:         strPtr("peer.example.com:51820"),
		Channel:          "IPv6 & IPv4",
		MPBGP:            "IPv6",
		MTU:              1420,
		RequestLinkLocal: "",
	}
}

func defaultTestConfig() *config.Config {
	return &config.Config{
		DefaultMTU:             1420,
		MyDN42LinkLocalAddress: net.ParseIP("fe80::1"),
		MyDN42ULAAddress:       net.ParseIP("fd42:d42:d42:1::1"),
		MyDN42IPv4Address:      net.ParseIP("172.22.167.1"),
	}
}

func TestGenerateConfigFull(t *testing.T) {
	t.Parallel()
	peer := defaultTestPeer()
	cfg := defaultTestConfig()

	result := GenerateConfig(peer, cfg)

	expected := "" +
		"# 4242421234 - test@example.com\n" +
		"[Interface]\n" +
		"ListenPort = 21234\n" +
		"Table = off\n" +
		"MTU = 1420\n" +
		"PostUp = wg set %i private-key /etc/wireguard/dn42-privatekey\n" +
		"PostUp = ip addr add fe80::1/64 peer fe80::1234/64 dev %i\n" +
		"PostUp = ip addr add fd42:d42:d42:1::1/128 dev %i\n" +
		"PostUp = ip addr add 172.22.167.1/32 peer 172.22.167.101/32 dev %i\n" +
		"[Peer]\n" +
		"PublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n" +
		"PresharedKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=\n" +
		"Endpoint = peer.example.com:51820\n" +
		"AllowedIPs = 172.20.0.0/14, 10.0.0.0/8, 172.31.0.0/16, fd00::/8, fe80::/64\n"

	if result != expected {
		t.Errorf("GenerateConfig mismatch.\nGot:\n%s\nExpected:\n%s", result, expected)
	}
}

func TestGenerateConfigNoPresharedKey(t *testing.T) {
	t.Parallel()
	peer := defaultTestPeer()
	peer.PresharedKey = ""
	cfg := defaultTestConfig()

	result := GenerateConfig(peer, cfg)

	if strings.Contains(result, "PresharedKey") {
		t.Error("expected no PresharedKey line when PresharedKey is empty")
	}
	if !strings.Contains(result, "PublicKey = ") {
		t.Error("expected PublicKey line to be present")
	}
}

func TestGenerateConfigNoClearnet(t *testing.T) {
	t.Parallel()
	peer := defaultTestPeer()
	peer.Clearnet = nil
	cfg := defaultTestConfig()

	result := GenerateConfig(peer, cfg)

	if strings.Contains(result, "Endpoint") {
		t.Error("expected no Endpoint line when Clearnet is nil")
	}
}

func TestGenerateConfigEmptyClearnet(t *testing.T) {
	t.Parallel()
	peer := defaultTestPeer()
	peer.Clearnet = strPtr("  ")
	cfg := defaultTestConfig()

	result := GenerateConfig(peer, cfg)

	if strings.Contains(result, "Endpoint") {
		t.Error("expected no Endpoint line when Clearnet is empty")
	}
}

func TestGenerateConfigNoPeerAddresses(t *testing.T) {
	t.Parallel()
	peer := defaultTestPeer()
	peer.IPv6 = "2001:db8::1"
	peer.IPv4 = "192.168.1.1"
	cfg := defaultTestConfig()

	result := GenerateConfig(peer, cfg)

	if strings.Contains(result, "peer fe80") {
		t.Error("expected no peer link-local in PostUp")
	}
	if strings.Contains(result, "peer fd") {
		t.Error("expected no peer ULA in PostUp")
	}
	if strings.Contains(result, "peer fe80") {
		t.Error("expected no peer link-local when IPv6 is ULA")
	}
	if strings.Contains(result, "peer 192.168") {
		t.Error("expected no peer IPv4 in PostUp for non-DN42 address")
	}
	if !strings.Contains(result, "fe80::1/64 dev %i") {
		t.Error("expected local link-local address")
	}
	if !strings.Contains(result, "fd42:d42:d42:1::1/128 dev %i") {
		t.Error("expected local ULA address")
	}
	if !strings.Contains(result, "172.22.167.1/32 dev %i") {
		t.Error("expected local IPv4 address")
	}
}

func TestGenerateConfigRequestLinkLocalOverride(t *testing.T) {
	t.Parallel()
	peer := defaultTestPeer()
	peer.RequestLinkLocal = "  FE80::ABCD  "
	cfg := defaultTestConfig()

	result := GenerateConfig(peer, cfg)

	if !strings.Contains(result, "fe80::abcd/64") {
		t.Errorf("expected overridden link-local fe80::abcd/64, got: %s", result)
	}
	if strings.Contains(result, "fe80::1/64") {
		t.Error("expected original fe80::1 to be overridden")
	}
}

func TestGenerateConfigRequestLinkLocalSentinelFallsBack(t *testing.T) {
	t.Parallel()
	peer := defaultTestPeer()
	peer.IPv6 = "fda2:e173:6ea4::"
	peer.IPv4 = "172.23.232.64"
	peer.RequestLinkLocal = "Not required due to not use LLA as IPv6"
	cfg := defaultTestConfig()

	result := GenerateConfig(peer, cfg)

	if strings.Contains(result, peer.RequestLinkLocal) {
		t.Fatalf("generated config contains Request-LinkLocal sentinel:\n%s", result)
	}
	if !strings.Contains(result, "PostUp = ip addr add fe80::1/64 dev %i") {
		t.Fatalf("expected configured link-local fallback, got:\n%s", result)
	}

	parsed, err := ParseConfig(peer.ASN, result)
	if err != nil {
		t.Fatalf("generated config should remain parseable: %v\n%s", err, result)
	}
	assertField(t, "MyLLA", parsed.MyLLA, "fe80::1")
	assertField(t, "PeerULA", parsed.PeerULA, "fda2:e173:6ea4::")
	assertField(t, "PeerIPv4", parsed.PeerIPv4, "172.23.232.64")
}

func TestGenerateConfigULAPeerAddress(t *testing.T) {
	t.Parallel()
	peer := defaultTestPeer()
	peer.IPv6 = "fd42:d42:d42::1234"
	cfg := defaultTestConfig()

	result := GenerateConfig(peer, cfg)

	if !strings.Contains(result, "peer fd42:d42:d42::1234/128") {
		t.Errorf("expected ULA peer address, got: %s", result)
	}
	// Link-local should have no peer
	if strings.Contains(result, "peer fe80") {
		t.Error("expected no peer link-local when IPv6 is ULA")
	}
}

func TestParseConfigFull(t *testing.T) {
	t.Parallel()
	input := "" +
		"# 4242421234 - test@example.com\n" +
		"[Interface]\n" +
		"ListenPort = 21234\n" +
		"Table = off\n" +
		"MTU = 1420\n" +
		"PostUp = wg set %i private-key /etc/wireguard/dn42-privatekey\n" +
		"PostUp = ip addr add fe80::1/64 peer fe80::1234/64 dev %i\n" +
		"PostUp = ip addr add fd42:d42:d42:1::1/128 peer fd42:d42:d42::1234/128 dev %i\n" +
		"PostUp = ip addr add 172.22.167.1/32 peer 172.22.167.101/32 dev %i\n" +
		"[Peer]\n" +
		"PublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n" +
		"PresharedKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=\n" +
		"Endpoint = peer.example.com:51820\n" +
		"AllowedIPs = 172.20.0.0/14, 10.0.0.0/8, 172.31.0.0/16, fd00::/8, fe80::/64\n"

	parsed, err := ParseConfig(4242421234, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertField(t, "Port", parsed.Port, 21234)
	assertField(t, "MTU", parsed.MTU, 1420)
	assertField(t, "MyLLA", parsed.MyLLA, "fe80::1")
	assertField(t, "PeerLLA", parsed.PeerLLA, "fe80::1234")
	assertField(t, "MyULA", parsed.MyULA, "fd42:d42:d42:1::1")
	assertField(t, "PeerULA", parsed.PeerULA, "fd42:d42:d42::1234")
	assertField(t, "PeerIPv4", parsed.PeerIPv4, "172.22.167.101")
	assertField(t, "PublicKey", parsed.PublicKey, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	assertField(t, "PresharedKey", parsed.PresharedKey, "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=")
	assertField(t, "Clearnet", parsed.Clearnet, "peer.example.com:51820")
}

func TestParseConfigMinimal(t *testing.T) {
	t.Parallel()
	input := "" +
		"# 4242421234 - test@example.com\n" +
		"[Interface]\n" +
		"ListenPort = 21234\n" +
		"Table = off\n" +
		"PostUp = wg set %i private-key /etc/wireguard/dn42-privatekey\n" +
		"PostUp = ip addr add fe80::1/64 dev %i\n" +
		"PostUp = ip addr add fd42:d42:d42:1::1/128 dev %i\n" +
		"PostUp = ip addr add 172.22.167.1/32 dev %i\n" +
		"[Peer]\n" +
		"PublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n" +
		"AllowedIPs = 172.20.0.0/14, 10.0.0.0/8, 172.31.0.0/16, fd00::/8, fe80::/64\n"

	parsed, err := ParseConfig(4242421234, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertField(t, "Port", parsed.Port, 21234)
	assertField(t, "MTU", parsed.MTU, 0)
	assertField(t, "PeerLLA", parsed.PeerLLA, "")
	assertField(t, "PeerULA", parsed.PeerULA, "")
	assertField(t, "PeerIPv4", parsed.PeerIPv4, "")
	assertField(t, "PublicKey", parsed.PublicKey, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	assertField(t, "PresharedKey", parsed.PresharedKey, "")
	assertField(t, "Clearnet", parsed.Clearnet, "")
}

func TestGenerateAndParseRoundTrip(t *testing.T) {
	t.Parallel()
	peer := defaultTestPeer()
	cfg := defaultTestConfig()

	generated := GenerateConfig(peer, cfg)
	parsed, err := ParseConfig(peer.ASN, generated)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	assertField(t, "Port", parsed.Port, peer.Port)
	assertField(t, "MTU", parsed.MTU, cfg.DefaultMTU)
	assertField(t, "MyLLA", parsed.MyLLA, cfg.MyDN42LinkLocalAddress.String())
	assertField(t, "PeerLLA", parsed.PeerLLA, "fe80::1234")
	assertField(t, "PeerIPv4", parsed.PeerIPv4, "172.22.167.101")
	assertField(t, "PublicKey", parsed.PublicKey, peer.PublicKey)
	assertField(t, "PresharedKey", parsed.PresharedKey, peer.PresharedKey)
	assertField(t, "Clearnet", parsed.Clearnet, *peer.Clearnet)
}

func TestGenerateAndParseRoundTripMinimal(t *testing.T) {
	t.Parallel()
	peer := defaultTestPeer()
	peer.PresharedKey = ""
	peer.Clearnet = nil
	cfg := defaultTestConfig()

	generated := GenerateConfig(peer, cfg)
	parsed, err := ParseConfig(peer.ASN, generated)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	assertField(t, "Port", parsed.Port, peer.Port)
	assertField(t, "MTU", parsed.MTU, cfg.DefaultMTU)
	assertField(t, "PresharedKey", parsed.PresharedKey, "")
	assertField(t, "Clearnet", parsed.Clearnet, "")
}

func TestParseHandshakeValid(t *testing.T) {
	t.Parallel()
	output := "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB= 1622500000"
	ts, err := ParseHandshake(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertField(t, "Handshake", ts, int64(1622500000))
}

func TestParseHandshakeNoDevice(t *testing.T) {
	t.Parallel()
	output := "Unable to access interface: No such device"
	ts, err := ParseHandshake(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertField(t, "Handshake", ts, int64(0))
}

func TestParseHandshakeEmpty(t *testing.T) {
	t.Parallel()
	ts, err := ParseHandshake("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertField(t, "Handshake", ts, int64(0))
}

func TestParseTransferValid(t *testing.T) {
	t.Parallel()
	output := "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB= 12345678 87654321"
	rx, tx, err := ParseTransfer(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertField(t, "RX", rx, int64(12345678))
	assertField(t, "TX", tx, int64(87654321))
}

func TestParseTransferNoDevice(t *testing.T) {
	t.Parallel()
	output := "Unable to access interface: No such device"
	rx, tx, err := ParseTransfer(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertField(t, "RX", rx, int64(0))
	assertField(t, "TX", tx, int64(0))
}

func TestParseTransferEmpty(t *testing.T) {
	t.Parallel()
	rx, tx, err := ParseTransfer("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertField(t, "RX", rx, int64(0))
	assertField(t, "TX", tx, int64(0))
}

func assertField[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}
