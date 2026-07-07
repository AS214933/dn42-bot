package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return path
}

func TestLoadValidConfig(t *testing.T) {
	yaml := `
host: "127.0.0.1"
port: 8080
secret: "my-secret"
open: true
max_peers: 10
min_peer_requirement: 2
net_support:
  ipv4: true
  ipv6: true
  ipv4_nat: false
  cn: true
extra_msg: "Hello"
my_dn42_link_local_address: "fe80::1816"
my_dn42_ula_address: "fd2c:1323:4042::1"
my_dn42_ipv4_address: "172.23.246.1"
my_wg_public_key: "abc123"
sentry_dsn: "https://sentry.io/123"
bird_ctl_path: "/custom/bird.ctl"
bird_table_4: "table4"
bird_table_6: "table6"
vnstat_auto_add: true
vnstat_auto_remove: true
default_mtu: 1500
server_url: "https://server.example.com"
dns_servers:
  - "172.20.0.53"
  - "1.1.1.1:5353"
  - "fd00::53"
`
	path := writeTempConfig(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", cfg.Host, "127.0.0.1")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want %d", cfg.Port, 8080)
	}
	if cfg.Secret != "my-secret" {
		t.Errorf("Secret = %q, want %q", cfg.Secret, "my-secret")
	}
	if !cfg.Open {
		t.Error("Open = false, want true")
	}
	if cfg.MaxPeers != 10 {
		t.Errorf("MaxPeers = %d, want %d", cfg.MaxPeers, 10)
	}
	if cfg.MinPeerRequirement != 2 {
		t.Errorf("MinPeerRequirement = %d, want %d", cfg.MinPeerRequirement, 2)
	}
	if !cfg.NetSupport.IPv4 {
		t.Error("NetSupport.IPv4 = false, want true")
	}
	if !cfg.NetSupport.IPv6 {
		t.Error("NetSupport.IPv6 = false, want true")
	}
	if cfg.NetSupport.IPv4NAT {
		t.Error("NetSupport.IPv4NAT = true, want false")
	}
	if !cfg.NetSupport.CN {
		t.Error("NetSupport.CN = false, want true")
	}
	if cfg.ExtraMsg != "Hello" {
		t.Errorf("ExtraMsg = %q, want %q", cfg.ExtraMsg, "Hello")
	}
	if cfg.MyDN42LinkLocalAddress.String() != "fe80::1816" {
		t.Errorf("MyDN42LinkLocalAddress = %q, want %q", cfg.MyDN42LinkLocalAddress, "fe80::1816")
	}
	if cfg.MyDN42ULAAddress.String() != "fd2c:1323:4042::1" {
		t.Errorf("MyDN42ULAAddress = %q, want %q", cfg.MyDN42ULAAddress, "fd2c:1323:4042::1")
	}
	if cfg.MyDN42IPv4Address.String() != "172.23.246.1" {
		t.Errorf("MyDN42IPv4Address = %q, want %q", cfg.MyDN42IPv4Address, "172.23.246.1")
	}
	if cfg.MyWGPublicKey != "abc123" {
		t.Errorf("MyWGPublicKey = %q, want %q", cfg.MyWGPublicKey, "abc123")
	}
	if cfg.SentryDSN != "https://sentry.io/123" {
		t.Errorf("SentryDSN = %q, want %q", cfg.SentryDSN, "https://sentry.io/123")
	}
	if cfg.BirdCtlPath != "/custom/bird.ctl" {
		t.Errorf("BirdCtlPath = %q, want %q", cfg.BirdCtlPath, "/custom/bird.ctl")
	}
	if cfg.BirdTable4 != "table4" {
		t.Errorf("BirdTable4 = %q, want %q", cfg.BirdTable4, "table4")
	}
	if cfg.BirdTable6 != "table6" {
		t.Errorf("BirdTable6 = %q, want %q", cfg.BirdTable6, "table6")
	}
	if !cfg.VnstatAutoAdd {
		t.Error("VnstatAutoAdd = false, want true")
	}
	if !cfg.VnstatAutoRemove {
		t.Error("VnstatAutoRemove = false, want true")
	}
	if cfg.DefaultMTU != 1500 {
		t.Errorf("DefaultMTU = %d, want %d", cfg.DefaultMTU, 1500)
	}
	if cfg.ServerURL != "https://server.example.com" {
		t.Errorf("ServerURL = %q, want %q", cfg.ServerURL, "https://server.example.com")
	}
	wantDNSServers := []string{"172.20.0.53:53", "1.1.1.1:5353", "[fd00::53]:53"}
	if len(cfg.DNSServers) != len(wantDNSServers) {
		t.Fatalf("DNSServers = %v, want %v", cfg.DNSServers, wantDNSServers)
	}
	for i, want := range wantDNSServers {
		if cfg.DNSServers[i] != want {
			t.Errorf("DNSServers[%d] = %q, want %q", i, cfg.DNSServers[i], want)
		}
	}
}

func TestLoadDefaultValues(t *testing.T) {
	yaml := `
secret: "s"
open: false
my_dn42_link_local_address: "fe80::1"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "10.0.0.1"
my_wg_public_key: "key"
bird_table_4: "t4"
bird_table_6: "t6"
vnstat_auto_add: false
`
	path := writeTempConfig(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want default %q", cfg.Host, "0.0.0.0")
	}
	if cfg.Port != 54321 {
		t.Errorf("Port = %d, want default %d", cfg.Port, 54321)
	}
	if cfg.MaxPeers != 0 {
		t.Errorf("MaxPeers = %d, want default %d", cfg.MaxPeers, 0)
	}
	if cfg.MinPeerRequirement != 0 {
		t.Errorf("MinPeerRequirement = %d, want default %d", cfg.MinPeerRequirement, 0)
	}
	if cfg.ExtraMsg != "" {
		t.Errorf("ExtraMsg = %q, want empty", cfg.ExtraMsg)
	}
	if cfg.SentryDSN != "" {
		t.Errorf("SentryDSN = %q, want empty", cfg.SentryDSN)
	}
	if cfg.BirdCtlPath != "/var/run/bird/bird.ctl" {
		t.Errorf("BirdCtlPath = %q, want default %q", cfg.BirdCtlPath, "/var/run/bird/bird.ctl")
	}
	if cfg.DefaultMTU != 1420 {
		t.Errorf("DefaultMTU = %d, want default %d", cfg.DefaultMTU, 1420)
	}
	if cfg.ServerURL != "" {
		t.Errorf("ServerURL = %q, want empty", cfg.ServerURL)
	}
	if len(cfg.DNSServers) != 0 {
		t.Errorf("DNSServers = %v, want empty", cfg.DNSServers)
	}
}

func TestLoadInvalidIPAddress(t *testing.T) {
	tests := []struct {
		name  string
		field string
		yaml  string
	}{
		{
			name: "invalid link local",
			yaml: `
secret: "s"
open: false
my_dn42_link_local_address: "not-an-ip"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "10.0.0.1"
my_wg_public_key: "key"
bird_table_4: "t4"
bird_table_6: "t6"
vnstat_auto_add: false
`,
		},
		{
			name: "invalid ula",
			yaml: `
secret: "s"
open: false
my_dn42_link_local_address: "fe80::1"
my_dn42_ula_address: "invalid"
my_dn42_ipv4_address: "10.0.0.1"
my_wg_public_key: "key"
bird_table_4: "t4"
bird_table_6: "t6"
vnstat_auto_add: false
`,
		},
		{
			name: "invalid ipv4",
			yaml: `
secret: "s"
open: false
my_dn42_link_local_address: "fe80::1"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "999.999.999.999"
my_wg_public_key: "key"
bird_table_4: "t4"
bird_table_6: "t6"
vnstat_auto_add: false
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempConfig(t, tt.yaml)
			_, err := Load(path)
			if err == nil {
				t.Error("Load() should return error for invalid IP")
			}
		})
	}
}

func TestLoadMissingRequiredFields(t *testing.T) {
	yaml := `
host: "0.0.0.0"
`
	path := writeTempConfig(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Error("Load() should return error for missing required fields")
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("Load() should return error for missing file")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	path := writeTempConfig(t, `invalid: [yaml: {broken`)
	_, err := Load(path)
	if err == nil {
		t.Error("Load() should return error for invalid YAML")
	}
}

func TestLoadInvalidDNSServer(t *testing.T) {
	yaml := `
secret: "s"
open: false
my_dn42_link_local_address: "fe80::1"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "10.0.0.1"
my_wg_public_key: "key"
bird_table_4: "t4"
bird_table_6: "t6"
vnstat_auto_add: false
dns_servers:
  - "dns.example.com"
`
	path := writeTempConfig(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Error("Load() should return error for invalid DNS server")
	}
}

func TestVnstatAutoRemoveForcedFalse(t *testing.T) {
	yaml := `
secret: "s"
open: false
my_dn42_link_local_address: "fe80::1"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "10.0.0.1"
my_wg_public_key: "key"
bird_table_4: "t4"
bird_table_6: "t6"
vnstat_auto_add: false
vnstat_auto_remove: true
`
	path := writeTempConfig(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.VnstatAutoRemove {
		t.Error("VnstatAutoRemove should be forced false when VnstatAutoAdd is false")
	}
	if cfg.VnstatAutoAdd {
		t.Error("VnstatAutoAdd should be false")
	}
}

func TestMaxPeersNegativeBecomesZero(t *testing.T) {
	yaml := `
secret: "s"
open: false
max_peers: -5
min_peer_requirement: -3
my_dn42_link_local_address: "fe80::1"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "10.0.0.1"
my_wg_public_key: "key"
bird_table_4: "t4"
bird_table_6: "t6"
vnstat_auto_add: false
`
	path := writeTempConfig(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.MaxPeers != 0 {
		t.Errorf("MaxPeers = %d, want 0 (negative should become 0)", cfg.MaxPeers)
	}
	if cfg.MinPeerRequirement != 0 {
		t.Errorf("MinPeerRequirement = %d, want 0 (negative should become 0)", cfg.MinPeerRequirement)
	}
}
