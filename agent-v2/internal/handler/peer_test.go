package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/service"
)

func peerTestConfig() *config.Config {
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

type cmdRecord struct {
	mu      sync.Mutex
	calls   [][]string
	outputs map[string]string
}

func newCmdRecord() *cmdRecord {
	return &cmdRecord{outputs: make(map[string]string)}
}

func (c *cmdRecord) Run(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, append([]string{name}, args...))
	key := name + " " + strings.Join(args, " ")
	if out, ok := c.outputs[key]; ok {
		return out, nil
	}
	return "", nil
}

func (c *cmdRecord) setOutput(name string, args []string, output string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := name + " " + strings.Join(args, " ")
	c.outputs[key] = output
}

func (c *cmdRecord) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *cmdRecord) callsMatching(name string, args ...string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, call := range c.calls {
		if len(call) >= 1+len(args) && call[0] == name {
			match := true
			for i, a := range args {
				if call[1+i] != a {
					match = false
					break
				}
			}
			if match {
				count++
			}
		}
	}
	return count
}

func setupDirs(t *testing.T) (wgDir, birdDir string) {
	t.Helper()
	wgDir = filepath.Join(t.TempDir(), "wireguard")
	birdDir = filepath.Join(t.TempDir(), "bird_peers")
	if err := os.MkdirAll(wgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(birdDir, 0755); err != nil {
		t.Fatal(err)
	}
	return
}

func writeWGConfig(t *testing.T, dir string, asn int, content string) {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("dn42-%d.conf", asn))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeBirdConfig(t *testing.T, dir string, asn int, content string) {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("%d.conf", asn))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func defaultPeerJSON() string {
	return `{
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
}

func newPeerHandler(cfg *config.Config, wgDir, birdDir string, runner CmdRunner) *PeerHandler {
	return &PeerHandler{
		Cfg:         cfg,
		WGConfDir:   wgDir,
		BirdConfDir: birdDir,
		RunCmd:      runner,
	}
}

func doRequest(handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != "" {
		reqBody = bytes.NewBufferString(body)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestPeerHandler_InvalidJSON(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	rec := doRequest(handler, http.MethodPost, "/peer", "not json{{{", map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestPeerHandler_PeerCountMismatch(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	writeWGConfig(t, wgDir, 4242421001, "dummy")
	writeWGConfig(t, wgDir, 4242421002, "dummy")
	writeBirdConfig(t, birdDir, 4242421001, "dummy")

	rec := doRequest(handler, http.MethodPost, "/peer", defaultPeerJSON(), map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "config count mismatch") {
		t.Errorf("expected mismatch error, got: %s", rec.Body.String())
	}
}

func TestPeerHandler_MaxPeersReached(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	cfg.MaxPeers = 1
	cfg.Open = true
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	writeWGConfig(t, wgDir, 4242429999, "dummy")
	writeBirdConfig(t, birdDir, 4242429999, "dummy")

	rec := doRequest(handler, http.MethodPost, "/peer", defaultPeerJSON(), map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestPeerHandler_NotOpen(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	cfg.Open = false
	cfg.MaxPeers = 0
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	rec := doRequest(handler, http.MethodPost, "/peer", defaultPeerJSON(), map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestPeerHandler_ExistingPeerBypassesRestrictions(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	cfg.Open = false
	cfg.MaxPeers = 1
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	writeWGConfig(t, wgDir, 4242421234, "old config")
	writeBirdConfig(t, birdDir, 4242421234, "old config")

	rec := doRequest(handler, http.MethodPost, "/peer", defaultPeerJSON(), map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for existing peer modification, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPeerHandler_SuccessNewPeer(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	rec := doRequest(handler, http.MethodPost, "/peer", defaultPeerJSON(), map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	wgContent, err := os.ReadFile(filepath.Join(wgDir, "dn42-4242421234.conf"))
	if err != nil {
		t.Fatalf("WG config not written: %v", err)
	}
	wg := string(wgContent)
	if !strings.Contains(wg, "# 4242421234 - test@example.com") {
		t.Error("WG config missing comment")
	}
	if !strings.Contains(wg, "ListenPort = 21234") {
		t.Error("WG config missing ListenPort")
	}
	if !strings.Contains(wg, "MTU = 1420") {
		t.Error("WG config missing MTU")
	}
	if !strings.Contains(wg, "PublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=") {
		t.Error("WG config missing PublicKey")
	}
	if !strings.Contains(wg, "PresharedKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=") {
		t.Error("WG config missing PresharedKey")
	}
	if !strings.Contains(wg, "Endpoint = peer.example.com:51820") {
		t.Error("WG config missing Endpoint")
	}
	if !strings.Contains(wg, "AllowedIPs = 172.20.0.0/14, 10.0.0.0/8, 172.31.0.0/16, fd00::/8, fe80::/64") {
		t.Error("WG config missing AllowedIPs")
	}

	birdContent, err := os.ReadFile(filepath.Join(birdDir, "4242421234.conf"))
	if err != nil {
		t.Fatalf("BIRD config not written: %v", err)
	}
	bird := string(birdContent)
	if !strings.Contains(bird, "protocol bgp DN42_4242421234_v6 from dn42_peers") {
		t.Error("BIRD config missing v6 protocol")
	}

	if cmd.callsMatching("wg-quick", "up", "dn42-4242421234") != 1 {
		t.Error("expected wg-quick up to be called once")
	}
	if cmd.callsMatching("birdc", "-s", cfg.BirdCtlPath, "configure") != 1 {
		t.Error("expected birdc configure to be called once")
	}
}

func TestPeerHandler_WGConfigNoPresharedKey(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "172.22.167.101",
		"IPv6": "fe80::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "",
		"Clearnet": "peer.example.com:51820",
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv6",
		"MTU": 1420,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	wgContent, _ := os.ReadFile(filepath.Join(wgDir, "dn42-4242421234.conf"))
	if strings.Contains(string(wgContent), "PresharedKey") {
		t.Error("expected no PresharedKey when empty")
	}
}

func TestPeerHandler_WGConfigNoClearnet(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "172.22.167.101",
		"IPv6": "fe80::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "",
		"Clearnet": null,
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv6",
		"MTU": 1420,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	wgContent, _ := os.ReadFile(filepath.Join(wgDir, "dn42-4242421234.conf"))
	if strings.Contains(string(wgContent), "Endpoint") {
		t.Error("expected no Endpoint when Clearnet is null")
	}
}

func TestPeerHandler_WGConfigClearnetCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		clearnet string
	}{
		{name: "false", clearnet: `false`},
		{name: "empty string", clearnet: `""`},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := peerTestConfig()
			wgDir, birdDir := setupDirs(t)
			cmd := newCmdRecord()
			handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

			peerJSON := strings.Replace(defaultPeerJSON(), `"Clearnet": "peer.example.com:51820"`, `"Clearnet": `+tt.clearnet, 1)
			rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
				"Content-Type": "application/json",
			})

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}

			wgContent, _ := os.ReadFile(filepath.Join(wgDir, "dn42-4242421234.conf"))
			if strings.Contains(string(wgContent), "Endpoint") {
				t.Error("expected no Endpoint when Clearnet is empty-compatible")
			}
		})
	}
}

func TestPeerHandler_AdminNoEndpointPayloadCompatibility(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"Region": "can",
		"ASN": 4242420774,
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv6",
		"ENH": true,
		"IPv6": "fe80::774",
		"IPv4": "Not enabled",
		"Request-LinkLocal": "fe80::2999:233",
		"Clearnet": null,
		"PublicKey": "dvS+ggE92z2Edu76iwnRkzq9E+U7fh8HcsMemdYQKDE=",
		"PresharedKey": null,
		"Port": "23374",
		"MTU": "1420",
		"Contact": "XIEXILIN"
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	wgContent, _ := os.ReadFile(filepath.Join(wgDir, "dn42-4242420774.conf"))
	wg := string(wgContent)
	for _, want := range []string{
		"ListenPort = 23374",
		"MTU = 1420",
		"PostUp = ip addr add fe80::2999:233/64 peer fe80::774/64 dev %i",
		"PublicKey = dvS+ggE92z2Edu76iwnRkzq9E+U7fh8HcsMemdYQKDE=",
	} {
		if !strings.Contains(wg, want) {
			t.Fatalf("WG config missing %q:\n%s", want, wg)
		}
	}
	if strings.Contains(wg, "Endpoint") {
		t.Fatalf("expected no Endpoint when Clearnet is null:\n%s", wg)
	}
	if strings.Contains(wg, "PresharedKey") {
		t.Fatalf("expected no PresharedKey when PresharedKey is null:\n%s", wg)
	}

	birdContent, _ := os.ReadFile(filepath.Join(birdDir, "4242420774.conf"))
	bird := string(birdContent)
	if !strings.Contains(bird, "protocol bgp DN42_4242420774_v6") {
		t.Fatalf("BIRD config missing v6 protocol:\n%s", bird)
	}
	if strings.Contains(bird, "DN42_4242420774_v4") {
		t.Fatalf("BIRD config should use MP-BGP over v6 only:\n%s", bird)
	}
}

func TestPeerHandler_IPClassification_ULA(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "172.22.167.101",
		"IPv6": "fd42:d42:d42::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "",
		"Clearnet": null,
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv6",
		"MTU": 1420,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	wgContent, _ := os.ReadFile(filepath.Join(wgDir, "dn42-4242421234.conf"))
	wg := string(wgContent)
	if !strings.Contains(wg, "peer fd42:d42:d42::1234/128") {
		t.Error("expected ULA peer address in WG config")
	}
}

func TestPeerHandler_IPClassification_NonDN42IPv4(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "192.168.1.1",
		"IPv6": "fe80::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "",
		"Clearnet": null,
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv6",
		"MTU": 1420,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	wgContent, _ := os.ReadFile(filepath.Join(wgDir, "dn42-4242421234.conf"))
	wg := string(wgContent)
	if strings.Contains(wg, "peer 192.168.1.1/32") {
		t.Error("expected no peer for non-DN42 IPv4")
	}
}

func TestPeerHandler_RequestLinkLocalOverride(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "172.22.167.101",
		"IPv6": "fe80::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "",
		"Clearnet": null,
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv6",
		"MTU": 1420,
		"Request-LinkLocal": "fe80::abcd"
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	wgContent, _ := os.ReadFile(filepath.Join(wgDir, "dn42-4242421234.conf"))
	wg := string(wgContent)
	if !strings.Contains(wg, "fe80::abcd/64") {
		t.Error("expected overridden link-local address")
	}
}

func TestPeerHandler_RequestLinkLocalSentinelFallsBack(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"Region": "nld",
		"ASN": 4242422895,
		"Contact": "@drool_on_shusky",
		"Port": 22895,
		"IPv4": "172.23.232.64",
		"IPv6": "fda2:e173:6ea4::",
		"PublicKey": "Qsu/ZtSEtsL5EoFQtLe0uCu4a+x90u8nbLPiDhJ4MRA=",
		"PresharedKey": null,
		"Clearnet": "41.223.30.42:23374",
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv4",
		"MTU": 1420,
		"Request-LinkLocal": "Not required due to not use LLA as IPv6"
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	wgContent, err := os.ReadFile(filepath.Join(wgDir, "dn42-4242422895.conf"))
	if err != nil {
		t.Fatalf("WG config not written: %v", err)
	}
	wg := string(wgContent)
	if strings.Contains(wg, "Not required due to not use LLA as IPv6") {
		t.Fatalf("generated config contains Request-LinkLocal sentinel:\n%s", wg)
	}
	if !strings.Contains(wg, "PostUp = ip addr add fe80::1/64 dev %i") {
		t.Fatalf("expected configured link-local fallback, got:\n%s", wg)
	}
	if _, err := service.ParseConfig(4242422895, wg); err != nil {
		t.Fatalf("generated config should remain parseable: %v\n%s", err, wg)
	}
}

func TestPeerHandler_BirdConfigIPv6Only(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "",
		"IPv6": "fd42:d42:d42::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "",
		"Clearnet": null,
		"Channel": "IPv6 only",
		"MP-BGP": "",
		"MTU": 1420,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	birdContent, _ := os.ReadFile(filepath.Join(birdDir, "4242421234.conf"))
	bird := string(birdContent)
	if !strings.Contains(bird, "protocol bgp DN42_4242421234_v6 from dn42_peers") {
		t.Error("expected v6 protocol")
	}
	if !strings.Contains(bird, "ipv4 {") {
		t.Error("expected ipv4 disabled block for IPv6-only")
	}
	if strings.Contains(bird, "protocol bgp DN42_4242421234_v4") {
		t.Error("expected no v4 protocol for IPv6-only")
	}
}

func TestPeerHandler_BirdConfigIPv4Only(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "172.22.167.101",
		"IPv6": "",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "",
		"Clearnet": null,
		"Channel": "IPv4 only",
		"MP-BGP": "",
		"MTU": 1420,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	birdContent, _ := os.ReadFile(filepath.Join(birdDir, "4242421234.conf"))
	bird := string(birdContent)
	if !strings.Contains(bird, "protocol bgp DN42_4242421234_v4 from dn42_peers") {
		t.Error("expected v4 protocol")
	}
	if !strings.Contains(bird, "ipv6 {") {
		t.Error("expected ipv6 disabled block for IPv4-only")
	}
	if strings.Contains(bird, "protocol bgp DN42_4242421234_v6") {
		t.Error("expected no v6 protocol for IPv4-only")
	}
}

func TestPeerHandler_BirdConfigDualMPBGPv6(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "172.22.167.101",
		"IPv6": "fd42:d42:d42::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "",
		"Clearnet": null,
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv6",
		"MTU": 1420,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	birdContent, _ := os.ReadFile(filepath.Join(birdDir, "4242421234.conf"))
	bird := string(birdContent)
	if !strings.Contains(bird, "protocol bgp DN42_4242421234_v6 from dn42_peers") {
		t.Error("expected v6 protocol")
	}
	if strings.Contains(bird, "import none") {
		t.Error("expected no import none for MP-BGP")
	}
}

func TestPeerHandler_BirdConfigDualMPBGPv4(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "172.22.167.101",
		"IPv6": "fd42:d42:d42::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "",
		"Clearnet": null,
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv4",
		"MTU": 1420,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	birdContent, _ := os.ReadFile(filepath.Join(birdDir, "4242421234.conf"))
	bird := string(birdContent)
	if !strings.Contains(bird, "protocol bgp DN42_4242421234_v4 from dn42_peers") {
		t.Error("expected v4 protocol")
	}
	if strings.Contains(bird, "import none") {
		t.Error("expected no import none for MP-BGP")
	}
}

func TestPeerHandler_BirdConfigDualNoMPBGP(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "172.22.167.101",
		"IPv6": "fd42:d42:d42::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "",
		"Clearnet": null,
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "Not supported",
		"MTU": 1420,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	birdContent, _ := os.ReadFile(filepath.Join(birdDir, "4242421234.conf"))
	bird := string(birdContent)
	if !strings.Contains(bird, "protocol bgp DN42_4242421234_v6 from dn42_peers") {
		t.Error("expected v6 protocol")
	}
	if !strings.Contains(bird, "protocol bgp DN42_4242421234_v4 from dn42_peers") {
		t.Error("expected v4 protocol")
	}
	if !strings.Contains(bird, "ipv4 {") {
		t.Error("expected ipv4 disabled in v6 protocol")
	}
	if !strings.Contains(bird, "ipv6 {") {
		t.Error("expected ipv6 disabled in v4 protocol")
	}
}

func TestPeerHandler_VnstatAutoAdd(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	cfg.VnstatAutoAdd = true
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	rec := doRequest(handler, http.MethodPost, "/peer", defaultPeerJSON(), map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if cmd.callsMatching("vnstat", "--add", "-i", "dn42-4242421234") != 1 {
		t.Error("expected vnstat --add to be called once")
	}
}

func TestPeerHandler_NoVnstatWhenDisabled(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	cfg.VnstatAutoAdd = false
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	rec := doRequest(handler, http.MethodPost, "/peer", defaultPeerJSON(), map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if cmd.callsMatching("vnstat") != 0 {
		t.Error("expected vnstat not to be called when disabled")
	}
}

func TestPeerHandler_DefaultMTU(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	cfg.DefaultMTU = 1500
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "172.22.167.101",
		"IPv6": "fe80::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "",
		"Clearnet": null,
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv6",
		"MTU": 0,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	wgContent, _ := os.ReadFile(filepath.Join(wgDir, "dn42-4242421234.conf"))
	wg := string(wgContent)
	if !strings.Contains(wg, "MTU = 1500") {
		t.Errorf("expected MTU = 1500 (default), got: %s", wg)
	}
}

func TestPeerHandler_BirdDescription(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	rec := doRequest(handler, http.MethodPost, "/peer", defaultPeerJSON(), map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	birdContent, _ := os.ReadFile(filepath.Join(birdDir, "4242421234.conf"))
	bird := string(birdContent)
	if !strings.Contains(bird, `description "test@example.com";`) {
		t.Error("expected description in BIRD config")
	}
}

func TestPeerHandler_RoundTrip(t *testing.T) {
	t.Parallel()
	cfg := peerTestConfig()
	wgDir, birdDir := setupDirs(t)
	cmd := newCmdRecord()
	handler := newPeerHandler(cfg, wgDir, birdDir, cmd.Run)

	peerJSON := `{
		"ASN": 4242421234,
		"Contact": "test@example.com",
		"Port": 21234,
		"IPv4": "172.22.167.101",
		"IPv6": "fd42:d42:d42::1234",
		"PublicKey": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey": "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		"Clearnet": "peer.example.com:51820",
		"Channel": "IPv6 & IPv4",
		"MP-BGP": "IPv6",
		"MTU": 1420,
		"Request-LinkLocal": ""
	}`

	rec := doRequest(handler, http.MethodPost, "/peer", peerJSON, map[string]string{
		"Content-Type": "application/json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	wgContent, _ := os.ReadFile(filepath.Join(wgDir, "dn42-4242421234.conf"))
	wg := string(wgContent)
	expectedParts := []string{
		"# 4242421234 - test@example.com",
		"[Interface]",
		"ListenPort = 21234",
		"Table = off",
		"MTU = 1420",
		"PostUp = wg set %i private-key /etc/wireguard/dn42-privatekey",
		"[Peer]",
		"PublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"PresharedKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		"Endpoint = peer.example.com:51820",
		"AllowedIPs = 172.20.0.0/14, 10.0.0.0/8, 172.31.0.0/16, fd00::/8, fe80::/64",
	}
	for _, part := range expectedParts {
		if !strings.Contains(wg, part) {
			t.Errorf("WG config missing %q", part)
		}
	}

	if !strings.Contains(wg, "peer fd42:d42:d42::1234/128") {
		t.Error("expected ULA peer address")
	}
	if !strings.Contains(wg, "peer 172.22.167.101/32") {
		t.Error("expected IPv4 peer address")
	}

	birdContent, _ := os.ReadFile(filepath.Join(birdDir, "4242421234.conf"))
	bird := string(birdContent)
	if !strings.Contains(bird, "neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external") {
		t.Error("expected BIRD neighbor")
	}
	if !strings.Contains(bird, `description "test@example.com";`) {
		t.Error("expected BIRD description")
	}
}

var _ = model.PeerInfo{}
var _ = json.Marshal
