package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
auto_update:
  enabled: true
  channel: "stable"
  check_interval: "6h"
  repository: "AS214933/dn42-bot"
  data_dir: "/var/lib/dn42-agent"
  agent_path: "/var/lib/dn42-agent/agent"
  service_name: "custom-agent.service"
  service_path: "/etc/systemd/system/custom-agent.service"
backup:
  enabled: true
  state_file: "/var/lib/dn42-agent/backup.yaml"
  work_dir: "/var/lib/dn42-agent/backup"
  bird_dir: "/etc/bird"
  wireguard_dir: "/etc/wireguard"
  interval: "10m"
  on_boot_delay: "1m"
  random_delay: "10s"
looking_glass:
  enabled: true
  allowed_cidrs:
    - "public"
    - "172.20.0.0/14"
  disallowed_cidrs:
    - "192.0.2.1"
  traceroute_enabled: true
  bird_max_concurrent: 32
  traceroute_max_concurrent: 8
  request_timeout: "20s"
  max_query_length: 2048
  max_output_bytes: 32768
peerfinder:
  enabled: true
  host: "::1"
  port: 9001
  secret_key: "` + strings.Repeat("ab", 32) + `"
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
	if !cfg.AutoUpdate.Enabled {
		t.Error("AutoUpdate.Enabled = false, want true")
	}
	if cfg.AutoUpdate.Channel != "stable" {
		t.Errorf("AutoUpdate.Channel = %q, want stable", cfg.AutoUpdate.Channel)
	}
	if cfg.AutoUpdate.CheckInterval != 6*time.Hour {
		t.Errorf("AutoUpdate.CheckInterval = %v, want 6h", cfg.AutoUpdate.CheckInterval)
	}
	if cfg.AutoUpdate.Repository != "AS214933/dn42-bot" {
		t.Errorf("AutoUpdate.Repository = %q", cfg.AutoUpdate.Repository)
	}
	if cfg.AutoUpdate.DataDir != "/var/lib/dn42-agent" {
		t.Errorf("AutoUpdate.DataDir = %q", cfg.AutoUpdate.DataDir)
	}
	if cfg.AutoUpdate.AgentPath != "/var/lib/dn42-agent/agent" {
		t.Errorf("AutoUpdate.AgentPath = %q", cfg.AutoUpdate.AgentPath)
	}
	if cfg.AutoUpdate.ServiceName != "custom-agent.service" {
		t.Errorf("AutoUpdate.ServiceName = %q", cfg.AutoUpdate.ServiceName)
	}
	if cfg.AutoUpdate.ServicePath != "/etc/systemd/system/custom-agent.service" {
		t.Errorf("AutoUpdate.ServicePath = %q", cfg.AutoUpdate.ServicePath)
	}
	if !cfg.Backup.Enabled {
		t.Error("Backup.Enabled = false, want true")
	}
	if cfg.Backup.StateFile != "/var/lib/dn42-agent/backup.yaml" || cfg.Backup.WorkDir != "/var/lib/dn42-agent/backup" {
		t.Errorf("Backup paths = %+v", cfg.Backup)
	}
	if cfg.Backup.Interval != 10*time.Minute || cfg.Backup.OnBootDelay != time.Minute || cfg.Backup.RandomDelay != 10*time.Second {
		t.Errorf("Backup durations = %+v", cfg.Backup)
	}
	if !cfg.LookingGlass.Enabled || !cfg.LookingGlass.TracerouteEnabled {
		t.Errorf("LookingGlass enable flags = %+v", cfg.LookingGlass)
	}
	if len(cfg.LookingGlass.AllowedCIDRs) != 2 || cfg.LookingGlass.AllowedCIDRs[0] != "public" {
		t.Errorf("LookingGlass.AllowedCIDRs = %v", cfg.LookingGlass.AllowedCIDRs)
	}
	if len(cfg.LookingGlass.DisallowedCIDRs) != 1 || cfg.LookingGlass.DisallowedCIDRs[0] != "192.0.2.1" {
		t.Errorf("LookingGlass.DisallowedCIDRs = %v", cfg.LookingGlass.DisallowedCIDRs)
	}
	if cfg.LookingGlass.BirdMaxConcurrent != 32 || cfg.LookingGlass.TracerouteMaxConcurrent != 8 {
		t.Errorf("LookingGlass concurrency limits = %+v", cfg.LookingGlass)
	}
	if cfg.LookingGlass.RequestTimeout != 20*time.Second || cfg.LookingGlass.MaxQueryLength != 2048 || cfg.LookingGlass.MaxOutputBytes != 32768 {
		t.Errorf("LookingGlass request limits = %+v", cfg.LookingGlass)
	}
	if !cfg.PeerFinder.Enabled {
		t.Error("PeerFinder.Enabled = false, want true")
	}
	if cfg.PeerFinder.Host != "::1" || cfg.PeerFinder.Port != 9001 {
		t.Errorf("PeerFinder bind = %s:%d, want ::1:9001", cfg.PeerFinder.Host, cfg.PeerFinder.Port)
	}
	if len(cfg.PeerFinder.HMACKey) != 32 || cfg.PeerFinder.HMACKey[0] != 0xab {
		t.Errorf("PeerFinder.HMACKey = %x, want decoded 32-byte key", cfg.PeerFinder.HMACKey)
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
	if cfg.Backup.Enabled {
		t.Error("Backup.Enabled = true, want false")
	}
	if cfg.Backup.StateFile != "/etc/dn42-agent/backup.yaml" || cfg.Backup.WorkDir != "/var/lib/dn42-agent/backup" {
		t.Errorf("Backup defaults = %+v", cfg.Backup)
	}
	if cfg.Backup.Interval != 5*time.Minute || cfg.Backup.OnBootDelay != 3*time.Minute || cfg.Backup.RandomDelay != 30*time.Second {
		t.Errorf("Backup default durations = %+v", cfg.Backup)
	}
	if len(cfg.DNSServers) != 0 {
		t.Errorf("DNSServers = %v, want empty", cfg.DNSServers)
	}
	if cfg.AutoUpdate.Enabled {
		t.Error("AutoUpdate.Enabled = true, want default false")
	}
	if cfg.AutoUpdate.Channel != "candidate" {
		t.Errorf("AutoUpdate.Channel = %q, want candidate", cfg.AutoUpdate.Channel)
	}
	if cfg.AutoUpdate.CheckInterval != 24*time.Hour {
		t.Errorf("AutoUpdate.CheckInterval = %v, want 24h", cfg.AutoUpdate.CheckInterval)
	}
	if cfg.AutoUpdate.Repository != "AS214933/dn42-bot" {
		t.Errorf("AutoUpdate.Repository = %q, want AS214933/dn42-bot", cfg.AutoUpdate.Repository)
	}
	if cfg.AutoUpdate.DataDir != "/etc/dn42-agent" {
		t.Errorf("AutoUpdate.DataDir = %q, want /etc/dn42-agent", cfg.AutoUpdate.DataDir)
	}
	if cfg.AutoUpdate.AgentPath != "/etc/dn42-agent/agent" {
		t.Errorf("AutoUpdate.AgentPath = %q, want /etc/dn42-agent/agent", cfg.AutoUpdate.AgentPath)
	}
	if cfg.AutoUpdate.ServiceName != "dn42-agent.service" {
		t.Errorf("AutoUpdate.ServiceName = %q, want dn42-agent.service", cfg.AutoUpdate.ServiceName)
	}
	if cfg.AutoUpdate.ServicePath != "/etc/systemd/system/dn42-agent.service" {
		t.Errorf("AutoUpdate.ServicePath = %q, want /etc/systemd/system/dn42-agent.service", cfg.AutoUpdate.ServicePath)
	}
	if cfg.LookingGlass.Enabled || !cfg.LookingGlass.TracerouteEnabled {
		t.Errorf("LookingGlass enable flags = %+v, want LG disabled and traceroute default true", cfg.LookingGlass)
	}
	if len(cfg.LookingGlass.AllowedCIDRs) != 0 || len(cfg.LookingGlass.DisallowedCIDRs) != 0 {
		t.Errorf("LookingGlass source policy = %+v, want empty", cfg.LookingGlass)
	}
	if cfg.LookingGlass.BirdMaxConcurrent != 16 || cfg.LookingGlass.TracerouteMaxConcurrent != 10 {
		t.Errorf("LookingGlass concurrency defaults = %+v", cfg.LookingGlass)
	}
	if cfg.LookingGlass.RequestTimeout != 15*time.Second || cfg.LookingGlass.MaxQueryLength != 4096 || cfg.LookingGlass.MaxOutputBytes != 64*1024 {
		t.Errorf("LookingGlass request defaults = %+v", cfg.LookingGlass)
	}
	if cfg.PeerFinder.Enabled || cfg.PeerFinder.Host != "::" || cfg.PeerFinder.Port != 9000 || len(cfg.PeerFinder.HMACKey) != 0 {
		t.Errorf("PeerFinder defaults = %+v, want disabled on [::]:9000 without key", cfg.PeerFinder)
	}
}

func TestLoadPeerFinderSecretKeyFileRelativeToConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "peerfinder.key"), []byte(strings.Repeat("cd", 32)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
secret: "s"
open: false
my_dn42_link_local_address: "fe80::1"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "10.0.0.1"
my_wg_public_key: "key"
bird_table_4: "t4"
bird_table_6: "t6"
peerfinder:
  enabled: true
  port: 9001
  secret_key_file: "peerfinder.key"
`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.PeerFinder.HMACKey) != 32 || cfg.PeerFinder.HMACKey[0] != 0xcd {
		t.Fatalf("PeerFinder.HMACKey = %x", cfg.PeerFinder.HMACKey)
	}
	if cfg.PeerFinder.SecretKeyFile != "peerfinder.key" {
		t.Fatalf("SecretKeyFile = %q", cfg.PeerFinder.SecretKeyFile)
	}
}

func TestLoadRejectsInvalidPeerFinderConfig(t *testing.T) {
	t.Parallel()
	base := `
secret: "s"
port: 54321
my_dn42_link_local_address: "fe80::1"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "172.20.0.1"
bird_table_4: "master4"
bird_table_6: "master6"
peerfinder:
  enabled: true
%s
`
	tests := []struct {
		name  string
		block string
	}{
		{name: "missing key", block: "  port: 9001"},
		{name: "bad key", block: "  port: 9001\n  secret_key: not-hex"},
		{name: "short key", block: "  port: 9001\n  secret_key: abcd"},
		{name: "bad port", block: "  port: 70000\n  secret_key: " + strings.Repeat("ab", 32)},
		{name: "api port reuse", block: "  port: 54321\n  secret_key: " + strings.Repeat("ab", 32)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := writeTempConfig(t, fmt.Sprintf(base, test.block))
			if _, err := Load(path); err == nil {
				t.Fatal("Load() succeeded, want peerfinder validation error")
			}
		})
	}
}

func TestLoadLookingGlassTracerouteExplicitlyDisabled(t *testing.T) {
	t.Parallel()
	path := writeTempConfig(t, `
secret: "s"
my_dn42_link_local_address: "fe80::1"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "172.20.0.1"
bird_table_4: "master4"
bird_table_6: "master6"
looking_glass:
  enabled: true
  allowed_cidrs: [dn42]
  traceroute_enabled: false
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.LookingGlass.Enabled || cfg.LookingGlass.TracerouteEnabled {
		t.Fatalf("LookingGlass = %+v", cfg.LookingGlass)
	}
}

func TestLoadRejectsInvalidLookingGlassConfig(t *testing.T) {
	t.Parallel()
	base := `
secret: "s"
my_dn42_link_local_address: "fe80::1"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "172.20.0.1"
bird_table_4: "master4"
bird_table_6: "master6"
looking_glass:
%s
`
	tests := []struct {
		name  string
		block string
	}{
		{name: "invalid allowed selector", block: "  allowed_cidrs: [not-a-selector]"},
		{name: "invalid disallowed selector", block: "  disallowed_cidrs: [bad-prefix]"},
		{name: "zero bird concurrency", block: "  bird_max_concurrent: 0"},
		{name: "excessive traceroute concurrency", block: "  traceroute_max_concurrent: 65"},
		{name: "invalid timeout", block: "  request_timeout: forever"},
		{name: "excessive timeout", block: "  request_timeout: 3m"},
		{name: "excessive query", block: "  max_query_length: 4097"},
		{name: "excessive output", block: "  max_output_bytes: 65537"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := writeTempConfig(t, fmt.Sprintf(base, test.block))
			if _, err := Load(path); err == nil {
				t.Fatal("Load() succeeded, want looking glass validation error")
			}
		})
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

func TestLoadInvalidAutoUpdateChannel(t *testing.T) {
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
auto_update:
  channel: "nightly"
`
	path := writeTempConfig(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Error("Load() should return error for invalid auto_update.channel")
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

func TestLoadBackupBootstrapFields(t *testing.T) {
	t.Parallel()
	base := `
secret: "s"
my_dn42_link_local_address: "fe80::1"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "10.0.0.1"
bird_table_4: "t4"
bird_table_6: "t6"
backup:
%s
`
	full := `  enabled: true
  node_name: "cn01"
  git_instance: "https://git.example.com"
  git_org: "dn42-backup"
  api_token: "tok"
`
	path := writeTempConfig(t, fmt.Sprintf(base, full))
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Backup.NodeName != "cn01" || cfg.Backup.GitOrg != "dn42-backup" || cfg.Backup.APIToken != "tok" {
		t.Fatalf("Backup bootstrap fields = %+v", cfg.Backup)
	}
	if cfg.Backup.GitInstance != "https://git.example.com" {
		t.Fatalf("GitInstance = %q", cfg.Backup.GitInstance)
	}

	// Partial bootstrap blocks are rejected so a typo can never silently
	// disable the remote-authoritative restore on a migrated node.
	for _, partial := range []string{
		`  enabled: true
  node_name: "cn01"
`,
		`  enabled: true
  node_name: "cn01"
  git_instance: "https://git.example.com"
`,
		`  enabled: true
  git_instance: "https://git.example.com"
  git_org: "dn42-backup"
`,
	} {
		path := writeTempConfig(t, fmt.Sprintf(base, partial))
		if _, err := Load(path); err == nil {
			t.Error("Load() succeeded, want bootstrap validation error for partial credentials")
		}
	}
}
