package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
)

func infoTestConfig() *config.Config {
	return &config.Config{
		Secret:                 "test-secret",
		Open:                   true,
		MaxPeers:               0,
		DefaultMTU:             1420,
		MyDN42LinkLocalAddress: net.ParseIP("fe80::1"),
		MyDN42ULAAddress:       net.ParseIP("fd42:d42:d42:1::1"),
		MyDN42IPv4Address:      net.ParseIP("172.22.167.1"),
		MyWGPublicKey:          "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		BirdCtlPath:            "/var/run/bird/bird.ctl",
		VnstatAutoAdd:          false,
		VnstatAutoRemove:       false,
		NetSupport: config.NetSupport{
			IPv4: true,
			IPv6: true,
			CN:   false,
		},
	}
}

func newInfoHandler(cfg *config.Config, wgDir, birdDir string, runner CmdRunner) *InfoHandler {
	return &InfoHandler{
		Cfg:         cfg,
		WGConfDir:   wgDir,
		BirdConfDir: birdDir,
		RunCmd:      runner,
	}
}

func sampleWGConfig() string {
	return "# 4242421234 - test@example.com\n" +
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
}

func sampleBirdConfigMPBGPv6() string {
	return "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
		"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"}\n"
}

func sampleBirdConfigIPv6Only() string {
	return "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
		"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"    ipv4 {\n" +
		"        import none;\n" +
		"        export none;\n" +
		"    };\n" +
		"}\n"
}

func sampleBirdConfigIPv4Only() string {
	return "protocol bgp DN42_4242421234_v4 from dn42_peers {\n" +
		"    neighbor 172.22.167.101 % 'dn42-4242421234' external;\n" +
		"    description \"test@example.com\";\n" +
		"    ipv6 {\n" +
		"        import none;\n" +
		"        export none;\n" +
		"    };\n" +
		"}\n"
}

func sampleBirdConfigDualSeparate() string {
	return "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
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
}

func TestInfoHandler_InvalidASN(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	rec := doRequest(handler, http.MethodPost, "/info", "not-a-number", nil)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestInfoHandler_NoConfigFiles(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	rec := doRequest(handler, http.MethodPost, "/info", "4242421234", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestInfoHandler_WGOnly(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	writeWGConfig(t, wgDir, 4242421234, sampleWGConfig())

	rec := doRequest(handler, http.MethodPost, "/info", "4242421234", nil)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "wg only") {
		t.Errorf("expected 'wg only' error, got: %s", rec.Body.String())
	}
}

func TestInfoHandler_BIRDOnly(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	writeBirdConfig(t, birdDir, 4242421234, sampleBirdConfigMPBGPv6())

	rec := doRequest(handler, http.MethodPost, "/info", "4242421234", nil)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bird only") {
		t.Errorf("expected 'bird only' error, got: %s", rec.Body.String())
	}
}

func TestInfoHandler_FullResponseMPBGP(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	writeWGConfig(t, wgDir, 4242421234, sampleWGConfig())
	writeBirdConfig(t, birdDir, 4242421234, sampleBirdConfigMPBGPv6())

	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "latest-handshakes"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 1622500000")
	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "transfer"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 12345678 87654321")

	birdProtoOutput := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "DN42_4242421234_v6"}, birdProtoOutput)

	birdAllOutput := "DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n" +
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
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "all", "DN42_4242421234_v6"}, birdAllOutput)

	rec := doRequest(handler, http.MethodPost, "/info", "4242421234", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp InfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if resp.Port != "21234" {
		t.Errorf("Port = %q, want %q", resp.Port, "21234")
	}
	if resp.MTU != 1420 {
		t.Errorf("MTU = %d, want %d", resp.MTU, 1420)
	}
	if resp.V6 != "fe80::1234" {
		t.Errorf("V6 = %q, want %q", resp.V6, "fe80::1234")
	}
	if resp.V4 != "172.22.167.101" {
		t.Errorf("V4 = %q, want %q", resp.V4, "172.22.167.101")
	}
	if resp.Clearnet != "peer.example.com:51820" {
		t.Errorf("Clearnet = %q, want %q", resp.Clearnet, "peer.example.com:51820")
	}
	if resp.PublicKey != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" {
		t.Errorf("PublicKey = %q", resp.PublicKey)
	}
	if resp.PresharedKey != "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=" {
		t.Errorf("PresharedKey = %q", resp.PresharedKey)
	}
	if resp.MyPublicKey != cfg.MyWGPublicKey {
		t.Errorf("MyPublicKey = %q, want %q", resp.MyPublicKey, cfg.MyWGPublicKey)
	}
	if resp.WGLastHandshake != 1622500000 {
		t.Errorf("WGLastHandshake = %d, want %d", resp.WGLastHandshake, 1622500000)
	}
	if len(resp.WGTransfer) != 2 || resp.WGTransfer[0] != 12345678 || resp.WGTransfer[1] != 87654321 {
		t.Errorf("WGTransfer = %v, want [12345678 87654321]", resp.WGTransfer)
	}
	if resp.LLA != cfg.MyDN42LinkLocalAddress.String() {
		t.Errorf("LLA = %q, want %q", resp.LLA, cfg.MyDN42LinkLocalAddress.String())
	}

	if len(resp.SessionName) != 1 || resp.SessionName[0] != "DN42_4242421234_v6" {
		t.Errorf("SessionName = %v, want [DN42_4242421234_v6]", resp.SessionName)
	}
	if !strings.Contains(resp.Session, "IPv6 Session") {
		t.Errorf("Session = %q, want contains 'IPv6 Session'", resp.Session)
	}
	if !strings.Contains(resp.Session, "IPv6 & IPv4 Channels") {
		t.Errorf("Session = %q, want contains 'IPv6 & IPv4 Channels'", resp.Session)
	}

	if status, ok := resp.BirdStatus["DN42_4242421234_v6"]; !ok {
		t.Error("expected bird_status to contain DN42_4242421234_v6")
	} else {
		if status.State != "Established" {
			t.Errorf("bird_status.State = %q, want %q", status.State, "Established")
		}
		if len(status.Routes) == 0 {
			t.Error("expected routes in bird_status")
		}
	}
}

func TestInfoHandler_SessionIPv6Only(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	writeWGConfig(t, wgDir, 4242421234, sampleWGConfig())
	writeBirdConfig(t, birdDir, 4242421234, sampleBirdConfigIPv6Only())

	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "latest-handshakes"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 1622500000")
	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "transfer"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 0 0")

	birdProtoOutput := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "DN42_4242421234_v6"}, birdProtoOutput)

	birdAllOutput := "DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n" +
		"  Channel ipv6\n" +
		"    State:          UP\n" +
		"    Table:          master6\n" +
		"    Output filter:  (unnamed)\n" +
		"    Routes:         5 imported, 10 exported, 3 preferred\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "all", "DN42_4242421234_v6"}, birdAllOutput)

	rec := doRequest(handler, http.MethodPost, "/info", "4242421234", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp InfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if !strings.Contains(resp.Session, "IPv6 Session with IPv6 channel only") {
		t.Errorf("Session = %q, want contains 'IPv6 Session with IPv6 channel only'", resp.Session)
	}
}

func TestInfoHandler_SessionIPv4Only(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	writeWGConfig(t, wgDir, 4242421234, sampleWGConfig())
	writeBirdConfig(t, birdDir, 4242421234, sampleBirdConfigIPv4Only())

	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "latest-handshakes"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 1622500000")
	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "transfer"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 0 0")

	birdProtoOutput := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v4 BGP        ---        up     2024-01-01  Established\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "DN42_4242421234_v4"}, birdProtoOutput)

	birdAllOutput := "DN42_4242421234_v4 BGP        ---        up     2024-01-01  Established\n" +
		"  Channel ipv4\n" +
		"    State:          UP\n" +
		"    Table:          master4\n" +
		"    Output filter:  (unnamed)\n" +
		"    Routes:         5 imported, 10 exported, 3 preferred\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "all", "DN42_4242421234_v4"}, birdAllOutput)

	rec := doRequest(handler, http.MethodPost, "/info", "4242421234", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp InfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if !strings.Contains(resp.Session, "IPv4 Session with IPv4 channel only") {
		t.Errorf("Session = %q, want contains 'IPv4 Session with IPv4 channel only'", resp.Session)
	}
}

func TestInfoHandler_SessionDualSeparate(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	writeWGConfig(t, wgDir, 4242421234, sampleWGConfig())
	writeBirdConfig(t, birdDir, 4242421234, sampleBirdConfigDualSeparate())

	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "latest-handshakes"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 1622500000")
	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "transfer"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 0 0")

	birdV6Output := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "DN42_4242421234_v6"}, birdV6Output)

	birdV6AllOutput := "DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n" +
		"  Channel ipv6\n" +
		"    State:          UP\n" +
		"    Table:          master6\n" +
		"    Output filter:  (unnamed)\n" +
		"    Routes:         5 imported, 10 exported, 3 preferred\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "all", "DN42_4242421234_v6"}, birdV6AllOutput)

	birdV4Output := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v4 BGP        ---        up     2024-01-01  Established\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "DN42_4242421234_v4"}, birdV4Output)

	birdV4AllOutput := "DN42_4242421234_v4 BGP        ---        up     2024-01-01  Established\n" +
		"  Channel ipv4\n" +
		"    State:          UP\n" +
		"    Table:          master4\n" +
		"    Output filter:  (unnamed)\n" +
		"    Routes:         3 imported, 5 exported, 2 preferred\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "all", "DN42_4242421234_v4"}, birdV4AllOutput)

	rec := doRequest(handler, http.MethodPost, "/info", "4242421234", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp InfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if !strings.Contains(resp.Session, "IPv6 & IPv4 Session with their own channels") {
		t.Errorf("Session = %q, want contains 'IPv6 & IPv4 Session with their own channels'", resp.Session)
	}

	if len(resp.SessionName) != 2 {
		t.Errorf("SessionName count = %d, want 2", len(resp.SessionName))
	}
}

func TestInfoHandler_RoundTrip(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	peerHandler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)
	infoHandler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "172.22.167.101",
		"IPv6": "fe80::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		"Clearnet": "peer.example.com:51820",
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv6",
		"MTU": 1420,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(peerHandler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("peer creation failed: %d: %s", rec.Code, rec.Body.String())
	}

	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "latest-handshakes"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 1622500000")
	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "transfer"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 12345678 87654321")

	birdProtoOutput := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "DN42_4242421234_v6"}, birdProtoOutput)

	birdAllOutput := "DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n" +
		"  Channel ipv6\n" +
		"    State:          UP\n" +
		"    Table:          master6\n" +
		"    Output filter:  (unnamed)\n" +
		"    Routes:         10 imported, 20 exported, 5 preferred\n" +
		"  Channel ipv4\n" +
		"    State:          UP\n" +
		"    Table:          master4\n" +
		"    Output filter:  (unnamed)\n" +
		"    Routes:         15 imported, 25 exported, 8 preferred\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "all", "DN42_4242421234_v6"}, birdAllOutput)

	infoRec := doRequest(infoHandler, http.MethodPost, "/info", "4242421234", nil)
	if infoRec.Code != http.StatusOK {
		t.Fatalf("info request failed: %d: %s", infoRec.Code, infoRec.Body.String())
	}

	var resp InfoResponse
	if err := json.Unmarshal(infoRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse info response: %v", err)
	}

	if resp.Port != fmt.Sprintf("%d", 21234) {
		t.Errorf("Port = %q, want %q", resp.Port, "21234")
	}
	if resp.MTU != 1420 {
		t.Errorf("MTU = %d, want 1420", resp.MTU)
	}
	if resp.V6 != "fe80::1234" {
		t.Errorf("V6 = %q, want %q", resp.V6, "fe80::1234")
	}
	if resp.V4 != "172.22.167.101" {
		t.Errorf("V4 = %q, want %q", resp.V4, "172.22.167.101")
	}
	if resp.PublicKey != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" {
		t.Errorf("PublicKey mismatch")
	}
	if resp.PresharedKey != "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=" {
		t.Errorf("PresharedKey mismatch")
	}
	if resp.Clearnet != "peer.example.com:51820" {
		t.Errorf("Clearnet = %q, want %q", resp.Clearnet, "peer.example.com:51820")
	}
	if resp.WGLastHandshake != 1622500000 {
		t.Errorf("WGLastHandshake = %d, want 1622500000", resp.WGLastHandshake)
	}
	if len(resp.WGTransfer) != 2 || resp.WGTransfer[0] != 12345678 || resp.WGTransfer[1] != 87654321 {
		t.Errorf("WGTransfer = %v", resp.WGTransfer)
	}
}

func TestInfoHandler_BirdStatusNotEstablished(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	writeWGConfig(t, wgDir, 4242421234, sampleWGConfig())
	writeBirdConfig(t, birdDir, 4242421234, sampleBirdConfigMPBGPv6())

	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "latest-handshakes"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 0")
	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "transfer"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 0 0")

	birdProtoOutput := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v6 BGP        ---        start  2024-01-01  Active Socket: Connection refused\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "DN42_4242421234_v6"}, birdProtoOutput)

	rec := doRequest(handler, http.MethodPost, "/info", "4242421234", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp InfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if resp.WGLastHandshake != 0 {
		t.Errorf("WGLastHandshake = %d, want 0", resp.WGLastHandshake)
	}

	if status, ok := resp.BirdStatus["DN42_4242421234_v6"]; !ok {
		t.Error("expected bird_status to contain DN42_4242421234_v6")
	} else {
		if status.State != "Active" {
			t.Errorf("bird_status.State = %q, want %q", status.State, "Active")
		}
	}
}

func TestInfoHandler_ResponseJSONStructure(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	writeWGConfig(t, wgDir, 4242421234, sampleWGConfig())
	writeBirdConfig(t, birdDir, 4242421234, sampleBirdConfigMPBGPv6())

	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "latest-handshakes"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 1622500000")
	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "transfer"},
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= 100 200")

	birdProtoOutput := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "DN42_4242421234_v6"}, birdProtoOutput)

	birdAllOutput := "DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n" +
		"  Channel ipv6\n" +
		"    State:          UP\n" +
		"    Table:          master6\n" +
		"    Output filter:  (unnamed)\n" +
		"    Routes:         10 imported, 20 exported, 5 preferred\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "all", "DN42_4242421234_v6"}, birdAllOutput)

	rec := doRequest(handler, http.MethodPost, "/info", "4242421234", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse response as map: %v", err)
	}

	requiredKeys := []string{
		"port", "mtu", "v6", "v4", "clearnet", "pubkey", "psk",
		"desc", "session", "session_name", "my_v6", "my_v4",
		"my_pubkey", "wg_last_handshake", "wg_transfer",
		"bird_status", "net_support", "lla",
	}
	for _, key := range requiredKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("response missing key %q", key)
		}
	}

	var wgTransfer []int64
	json.Unmarshal(raw["wg_transfer"], &wgTransfer)
	if len(wgTransfer) != 2 {
		t.Errorf("wg_transfer should be array of 2, got %d", len(wgTransfer))
	}

	var sessionName []string
	json.Unmarshal(raw["session_name"], &sessionName)
	if len(sessionName) == 0 {
		t.Error("session_name should not be empty")
	}

	var birdStatus map[string]json.RawMessage
	json.Unmarshal(raw["bird_status"], &birdStatus)
	if len(birdStatus) == 0 {
		t.Error("bird_status should not be empty")
	}
	for _, v := range birdStatus {
		var tuple []json.RawMessage
		if err := json.Unmarshal(v, &tuple); err != nil {
			t.Errorf("bird_status entry should be JSON array, got error: %v", err)
		}
		if len(tuple) != 3 {
			t.Errorf("bird_status entry should have 3 elements, got %d", len(tuple))
		}
	}
}

func TestInfoHandler_WGNoDevice(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	writeWGConfig(t, wgDir, 4242421234, sampleWGConfig())
	writeBirdConfig(t, birdDir, 4242421234, sampleBirdConfigMPBGPv6())

	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "latest-handshakes"},
		"Unable to access interface: No such device")
	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "transfer"},
		"Unable to access interface: No such device")

	birdProtoOutput := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "DN42_4242421234_v6"}, birdProtoOutput)

	birdAllOutput := "DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n" +
		"  Channel ipv6\n" +
		"    State:          UP\n" +
		"    Output filter:  (unnamed)\n" +
		"    Routes:         5 imported, 10 exported, 3 preferred\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "all", "DN42_4242421234_v6"}, birdAllOutput)

	rec := doRequest(handler, http.MethodPost, "/info", "4242421234", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp InfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.WGLastHandshake != 0 {
		t.Errorf("WGLastHandshake = %d, want 0 for no device", resp.WGLastHandshake)
	}
	if len(resp.WGTransfer) != 2 || resp.WGTransfer[0] != 0 || resp.WGTransfer[1] != 0 {
		t.Errorf("WGTransfer = %v, want [0 0] for no device", resp.WGTransfer)
	}
}

func TestInfoHandler_MinimalWGConfig(t *testing.T) {
	t.Parallel()
	cfg := infoTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newInfoHandler(cfg, wgDir, birdDir, cmd.Run)

	minimalWG := "# 4242421234 - test@example.com\n" +
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

	writeWGConfig(t, wgDir, 4242421234, minimalWG)
	writeBirdConfig(t, birdDir, 4242421234, sampleBirdConfigMPBGPv6())

	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "latest-handshakes"}, "")
	cmd.setOutput("wg", []string{"show", "dn42-4242421234", "transfer"}, "")

	birdProtoOutput := "Name     Proto      Table      State  Since       Info\n" +
		"\n" +
		"DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "DN42_4242421234_v6"}, birdProtoOutput)

	birdAllOutput := "DN42_4242421234_v6 BGP        ---        up     2024-01-01  Established\n" +
		"  Channel ipv6\n" +
		"    State:          UP\n" +
		"    Output filter:  (unnamed)\n" +
		"    Routes:         5 imported, 10 exported, 3 preferred\n"
	cmd.setOutput("birdc", []string{"-s", cfg.BirdCtlPath, "show", "protocols", "all", "DN42_4242421234_v6"}, birdAllOutput)

	rec := doRequest(handler, http.MethodPost, "/info", "4242421234", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp InfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.MTU != cfg.DefaultMTU {
		t.Errorf("MTU = %d, want default %d", resp.MTU, cfg.DefaultMTU)
	}
	if resp.PresharedKey != "" {
		t.Errorf("PresharedKey = %q, want empty", resp.PresharedKey)
	}
	if resp.Clearnet != "" {
		t.Errorf("Clearnet = %q, want empty", resp.Clearnet)
	}
	if resp.V6 != "" {
		t.Errorf("V6 = %q, want empty (no peer LLA/ULA)", resp.V6)
	}
	if resp.V4 != "" {
		t.Errorf("V4 = %q, want empty (no peer IPv4)", resp.V4)
	}
}

var _ = model.BirdSessionStatus{}
var _ = context.Background
