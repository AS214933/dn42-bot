package service

import (
	"testing"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
)

func defaultBirdPeer() model.PeerInfo {
	return model.PeerInfo{
		ASN:      4242421234,
		Contact:  "test@example.com",
		Port:     21234,
		IPv4:     "172.22.167.101",
		IPv6:     "fd42:d42:d42::1234",
		PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		Channel:  "IPv6 & IPv4",
		MPBGP:    "IPv6",
		MTU:      1420,
	}
}

func TestGenerateProtocolIPv6Only(t *testing.T) {
	t.Parallel()
	peer := defaultBirdPeer()
	peer.Channel = "IPv6 only"

	result := GenerateProtocol(6, true, peer)

	expected := "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
		"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"    ipv4 {\n" +
		"        import none;\n" +
		"        export none;\n" +
		"    };\n" +
		"}\n"

	if result != expected {
		t.Errorf("GenerateProtocol mismatch.\nGot:\n%s\nExpected:\n%s", result, expected)
	}
}

func TestGenerateProtocolIPv4Only(t *testing.T) {
	t.Parallel()
	peer := defaultBirdPeer()
	peer.Channel = "IPv4 only"

	result := GenerateProtocol(4, true, peer)

	expected := "protocol bgp DN42_4242421234_v4 from dn42_peers {\n" +
		"    neighbor 172.22.167.101 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"    ipv6 {\n" +
		"        import none;\n" +
		"        export none;\n" +
		"    };\n" +
		"}\n"

	if result != expected {
		t.Errorf("GenerateProtocol mismatch.\nGot:\n%s\nExpected:\n%s", result, expected)
	}
}

func TestGenerateProtocolIPv6AndIPv4MPBGP(t *testing.T) {
	t.Parallel()
	peer := defaultBirdPeer()
	peer.Channel = "IPv6 & IPv4"
	peer.MPBGP = "IPv6"

	result := GenerateProtocol(6, false, peer)

	expected := "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
		"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"}\n"

	if result != expected {
		t.Errorf("GenerateProtocol mismatch.\nGot:\n%s\nExpected:\n%s", result, expected)
	}
}

func TestGenerateProtocolIPv6AndIPv4MPBGPv4(t *testing.T) {
	t.Parallel()
	peer := defaultBirdPeer()
	peer.Channel = "IPv6 & IPv4"
	peer.MPBGP = "IPv4"

	result := GenerateProtocol(4, false, peer)

	expected := "protocol bgp DN42_4242421234_v4 from dn42_peers {\n" +
		"    neighbor 172.22.167.101 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"}\n"

	if result != expected {
		t.Errorf("GenerateProtocol mismatch.\nGot:\n%s\nExpected:\n%s", result, expected)
	}
}

func TestGenerateProtocolIPv6AndIPv4NoMPBGP(t *testing.T) {
	t.Parallel()
	peer := defaultBirdPeer()
	peer.Channel = "IPv6 & IPv4"
	peer.MPBGP = "Not supported"

	result6 := GenerateProtocol(6, true, peer)
	result4 := GenerateProtocol(4, true, peer)

	expected6 := "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
		"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"    ipv4 {\n" +
		"        import none;\n" +
		"        export none;\n" +
		"    };\n" +
		"}\n"

	expected4 := "protocol bgp DN42_4242421234_v4 from dn42_peers {\n" +
		"    neighbor 172.22.167.101 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"    ipv6 {\n" +
		"        import none;\n" +
		"        export none;\n" +
		"    };\n" +
		"}\n"

	if result6 != expected6 {
		t.Errorf("GenerateProtocol v6 mismatch.\nGot:\n%s\nExpected:\n%s", result6, expected6)
	}
	if result4 != expected4 {
		t.Errorf("GenerateProtocol v4 mismatch.\nGot:\n%s\nExpected:\n%s", result4, expected4)
	}
}

func TestParseProtocol(t *testing.T) {
	t.Parallel()
	input := "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
		"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"}\n"

	parsed, err := ParseProtocol(4242421234, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertField(t, "Version", parsed.Version, 6)
	assertField(t, "Neighbor", parsed.Neighbor, "fd42:d42:d42::1234")
	assertField(t, "Description", parsed.Description, "test@example.com")
	assertField(t, "Only", parsed.Only, false)
}

func TestParseProtocolIPv4Only(t *testing.T) {
	t.Parallel()
	input := "protocol bgp DN42_4242421234_v4 from dn42_peers {\n" +
		"    neighbor 172.22.167.101 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"    ipv6 {\n" +
		"        import none;\n" +
		"        export none;\n" +
		"    };\n" +
		"}\n"

	parsed, err := ParseProtocol(4242421234, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertField(t, "Version", parsed.Version, 4)
	assertField(t, "Neighbor", parsed.Neighbor, "172.22.167.101")
	assertField(t, "Description", parsed.Description, "test@example.com")
	assertField(t, "Only", parsed.Only, true)
}

func TestParseProtocolIPv6Only(t *testing.T) {
	t.Parallel()
	input := "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
		"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"    ipv4 {\n" +
		"        import none;\n" +
		"        export none;\n" +
		"    };\n" +
		"}\n"

	parsed, err := ParseProtocol(4242421234, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertField(t, "Version", parsed.Version, 6)
	assertField(t, "Only", parsed.Only, true)
}

func TestParseProtocolBothVersions(t *testing.T) {
	t.Parallel()
	input := "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
		"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"    ipv4 {\n" +
		"        import none;\n" +
		"        export none;\n" +
		"    };\n" +
		"}\n" +
		"\n" +
		"protocol bgp DN42_4242421234_v4 from dn42_peers {\n" +
		"    neighbor 172.22.167.101 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"    ipv6 {\n" +
		"        import none;\n" +
		"        export none;\n" +
		"    };\n" +
		"}\n"

	parsed, err := ParseProtocol(4242421234, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertField(t, "Version", parsed.Version, 6)
	assertField(t, "Description", parsed.Description, "test@example.com")
	assertField(t, "Only", parsed.Only, true)
}

func TestGenerateAndParseBirdRoundTripMPBGP(t *testing.T) {
	t.Parallel()
	peer := defaultBirdPeer()
	peer.Channel = "IPv6 & IPv4"
	peer.MPBGP = "IPv6"

	generated := GenerateProtocol(6, false, peer)
	parsed, err := ParseProtocol(peer.ASN, generated)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	assertField(t, "Version", parsed.Version, 6)
	assertField(t, "Neighbor", parsed.Neighbor, peer.IPv6)
	assertField(t, "Description", parsed.Description, peer.Contact)
	assertField(t, "Only", parsed.Only, false)
}

func TestGenerateAndParseBirdRoundTripOnly(t *testing.T) {
	t.Parallel()
	peer := defaultBirdPeer()
	peer.Channel = "IPv6 only"

	generated := GenerateProtocol(6, true, peer)
	parsed, err := ParseProtocol(peer.ASN, generated)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	assertField(t, "Version", parsed.Version, 6)
	assertField(t, "Neighbor", parsed.Neighbor, peer.IPv6)
	assertField(t, "Description", parsed.Description, peer.Contact)
	assertField(t, "Only", parsed.Only, true)
}

func TestParseBirdStatusEstablished(t *testing.T) {
	t.Parallel()
	output := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n"

	status, err := ParseBirdStatus("DN42_4242421234_v6", output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertField(t, "State", status.State, "Established")
}

func TestParseBirdStatusStart(t *testing.T) {
	t.Parallel()
	output := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v4 BGP        ---        start  2024-01-01  Active Socket: Connection refused\n"

	status, err := ParseBirdStatus("DN42_4242421234_v4", output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertField(t, "State", status.State, "Active")
	assertField(t, "Info", status.Info, "Socket: Connection refused")
}

func TestParseBirdAllEstablished(t *testing.T) {
	t.Parallel()
	output := "DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n" +
		"  Channel ipv6\n" +
		"    State:          UP\n" +
		"    Table:          master6\n" +
		"    Preference:     100\n" +
		"    Output filter:  (unnamed)\n" +
		"    Routes:         10 imported, 20 exported, 5 preferred\n" +
		"  Channel ipv4\n" +
		"    State:          UP\n" +
		"    Table:          master4\n" +
		"    Preference:     100\n" +
		"    Output filter:  (unnamed)\n" +
		"    Routes:         15 imported, 25 exported, 8 preferred\n"

	channels, err := ParseBirdAll("DN42_4242421234_v6", output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(channels))
	}

	ch6, ok := channels["ipv6"]
	if !ok {
		t.Fatal("expected ipv6 channel")
	}
	assertField(t, "ipv6 State", ch6["State"], "UP")
	assertField(t, "ipv6 Routes", ch6["Routes"], "10 imported, 20 exported, 5 preferred")

	ch4, ok := channels["ipv4"]
	if !ok {
		t.Fatal("expected ipv4 channel")
	}
	assertField(t, "ipv4 State", ch4["State"], "UP")
	assertField(t, "ipv4 Routes", ch4["Routes"], "15 imported, 25 exported, 8 preferred")
}
