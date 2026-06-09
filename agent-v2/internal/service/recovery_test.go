package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

func TestIsDNSError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"empty", "", false},
		{"no error", "Some unrelated output", false},
		{"name or service not known", "Error: Name or service not known", true},
		{"temporary failure", "Temporary failure in name resolution", true},
		{"no address associated", "no address associated with hostname", true},
		{"nodename nor servname", "nodename nor servname provided", true},
		{"non-recoverable", "non-recoverable failure in name resolution", true},
		{"could not resolve", "could not resolve host.example.com", true},
		{"case insensitive", "NAME OR SERVICE NOT KNOWN", true},
		{"mixed case", "Could Not Resolve endpoint.example.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isDNSError(tc.output)
			if got != tc.want {
				t.Errorf("isDNSError(%q) = %v, want %v", tc.output, got, tc.want)
			}
		})
	}
}

func TestHasHostnameEndpoint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	writeFile := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("hostname endpoint", func(t *testing.T) {
		t.Parallel()
		p := writeFile("hostname.conf", "[Peer]\nEndpoint = peer.example.com:51820\nAllowedIPs = ...\n")
		if !hasHostnameEndpoint(p) {
			t.Error("expected true for hostname endpoint")
		}
	})

	t.Run("ipv4 endpoint", func(t *testing.T) {
		t.Parallel()
		p := writeFile("ipv4.conf", "[Peer]\nEndpoint = 1.2.3.4:51820\nAllowedIPs = ...\n")
		if hasHostnameEndpoint(p) {
			t.Error("expected false for IPv4 endpoint")
		}
	})

	t.Run("ipv6 endpoint", func(t *testing.T) {
		t.Parallel()
		p := writeFile("ipv6.conf", "[Peer]\nEndpoint = [fd42::1]:51820\nAllowedIPs = ...\n")
		if hasHostnameEndpoint(p) {
			t.Error("expected false for IPv6 endpoint")
		}
	})

	t.Run("no endpoint", func(t *testing.T) {
		t.Parallel()
		p := writeFile("noep.conf", "[Peer]\nPublicKey = AAA=\nAllowedIPs = ...\n")
		if hasHostnameEndpoint(p) {
			t.Error("expected false when no Endpoint line")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		t.Parallel()
		if hasHostnameEndpoint("/nonexistent/path/xyz.conf") {
			t.Error("expected false for nonexistent file")
		}
	})
}

func TestRemoveEndpointFromConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	t.Run("removes endpoint", func(t *testing.T) {
		t.Parallel()
		content := "[Interface]\nListenPort = 51820\n[Peer]\nEndpoint = host.example.com:51820\nAllowedIPs = ...\n"
		p := filepath.Join(dir, "with_ep.conf")
		os.WriteFile(p, []byte(content), 0644)

		ok := removeEndpointFromConfig(p)
		if !ok {
			t.Fatal("expected true")
		}
		data, _ := os.ReadFile(p)
		result := string(data)
		if strings.Contains(result, "Endpoint") {
			t.Errorf("Endpoint line still present: %q", result)
		}
		if !strings.Contains(result, "AllowedIPs") {
			t.Error("AllowedIPs should still be present")
		}
	})

	t.Run("no endpoint to remove", func(t *testing.T) {
		t.Parallel()
		content := "[Interface]\nListenPort = 51820\n[Peer]\nPublicKey = AAA=\n"
		p := filepath.Join(dir, "no_ep.conf")
		os.WriteFile(p, []byte(content), 0644)

		ok := removeEndpointFromConfig(p)
		if !ok {
			t.Fatal("expected true even with no endpoint")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		t.Parallel()
		if removeEndpointFromConfig("/nonexistent/xyz.conf") {
			t.Error("expected false for nonexistent file")
		}
	})
}

func testConfig() *config.Config {
	return &config.Config{
		ServerURL:              "https://server.example.com",
		Secret:                 "test-secret",
		DefaultMTU:             1420,
		MyDN42LinkLocalAddress: net.ParseIP("fe80::1"),
		MyDN42ULAAddress:       net.ParseIP("fd42:d42:d42:1::1"),
		MyDN42IPv4Address:      net.ParseIP("172.22.167.1"),
	}
}

func TestEnsureWG_NoConfigsDir(t *testing.T) {
	t.Parallel()
	deps := testRecoveryDeps()
	deps.ReadDir = func(string) ([]os.DirEntry, error) {
		return nil, os.ErrNotExist
	}
	cfg := testConfig()

	EnsureWGInterfacesUp(context.Background(), cfg, deps)
}

func TestEnsureWG_NoDn42Configs(t *testing.T) {
	t.Parallel()
	deps := testRecoveryDeps()
	deps.ReadDir = func(string) ([]os.DirEntry, error) {
		return []os.DirEntry{}, nil
	}
	cfg := testConfig()

	EnsureWGInterfacesUp(context.Background(), cfg, deps)
}

func TestEnsureWG_AllInterfacesUp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "dn42-1234.conf"), []byte("[Peer]\n"), 0644)
	os.WriteFile(filepath.Join(dir, "dn42-5678.conf"), []byte("[Peer]\n"), 0644)

	deps := testRecoveryDeps()
	deps.WGDir = dir
	deps.ReadDir = func(dirname string) ([]os.DirEntry, error) {
		return os.ReadDir(dirname)
	}
	deps.RunCommand = func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		if name == "wg" && len(args) >= 2 && args[0] == "show" && args[1] == "interfaces" {
			return "dn42-1234 dn42-5678\n", nil
		}
		return "", nil
	}
	cfg := testConfig()

	EnsureWGInterfacesUp(context.Background(), cfg, deps)
}

func TestEnsureWG_BringsUpMissingInterface(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var wgQuickCalls []string

	deps := testRecoveryDeps()
	deps.ReadDir = func(string) ([]os.DirEntry, error) {
		return []os.DirEntry{
			&fakeDirEntry{name: "dn42-9999.conf"},
		}, nil
	}
	deps.RunCommand = func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		if name == "wg" && len(args) >= 2 && args[0] == "show" && args[1] == "interfaces" {
			return "", nil
		}
		if name == "wg-quick" && len(args) >= 2 && args[0] == "up" {
			mu.Lock()
			wgQuickCalls = append(wgQuickCalls, args[1])
			mu.Unlock()
			return "", nil
		}
		return "", nil
	}
	deps.ReadFile = func(filename string) ([]byte, error) {
		return []byte("[Peer]\nEndpoint = 1.2.3.4:51820\n"), nil
	}

	EnsureWGInterfacesUp(context.Background(), testConfig(), deps)

	mu.Lock()
	defer mu.Unlock()
	if len(wgQuickCalls) != 1 || wgQuickCalls[0] != "dn42-9999" {
		t.Errorf("expected 1 wg-quick up call for dn42-9999, got %v", wgQuickCalls)
	}
}

func TestEnsureWG_DNSFailureRemovesEndpoint(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var wgQuickCalls []string
	var writeFiles []string
	var postURL string
	var postBody []byte

	dir := t.TempDir()
	configContent := "[Interface]\nListenPort = 51820\n[Peer]\nEndpoint = broken.example.com:51820\nAllowedIPs = 0.0.0.0/0\n"
	configPath := filepath.Join(dir, "dn42-4242421234.conf")
	os.WriteFile(configPath, []byte(configContent), 0644)

	deps := testRecoveryDeps()
	deps.WGDir = dir
	deps.ReadDir = func(string) ([]os.DirEntry, error) {
		return []os.DirEntry{
			&fakeDirEntry{name: "dn42-4242421234.conf"},
		}, nil
	}
	deps.RunCommand = func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		if name == "wg" && len(args) >= 2 && args[0] == "show" && args[1] == "interfaces" {
			return "", nil
		}
		if name == "wg-quick" && len(args) >= 2 && args[0] == "up" {
			mu.Lock()
			wgQuickCalls = append(wgQuickCalls, args[1])
			mu.Unlock()
			return "Error: Name or service not known", errors.New("exit status 1")
		}
		return "", nil
	}
	deps.ReadFile = func(filename string) ([]byte, error) {
		return os.ReadFile(filename)
	}
	deps.WriteFile = func(filename string, data []byte, perm os.FileMode) error {
		mu.Lock()
		writeFiles = append(writeFiles, filename)
		mu.Unlock()
		return os.WriteFile(filename, data, perm)
	}
	deps.HTTPPost = func(_ context.Context, url string, body []byte, _ map[string]string) error {
		mu.Lock()
		postURL = url
		postBody = body
		mu.Unlock()
		return nil
	}

	cfg := testConfig()
	EnsureWGInterfacesUp(context.Background(), cfg, deps)

	mu.Lock()
	defer mu.Unlock()

	if len(wgQuickCalls) != 2 {
		t.Errorf("expected 2 wg-quick up calls, got %d: %v", len(wgQuickCalls), wgQuickCalls)
	}

	if len(writeFiles) != 1 {
		t.Errorf("expected 1 config write, got %d", len(writeFiles))
	}

	data, _ := os.ReadFile(configPath)
	if strings.Contains(string(data), "Endpoint") {
		t.Errorf("Endpoint should have been removed from config: %s", string(data))
	}

	if postURL != "https://server.example.com/internal/broadcast" {
		t.Errorf("unexpected POST URL: %s", postURL)
	}

	var payload map[string]interface{}
	json.Unmarshal(postBody, &payload)
	if payload["type"] != "dns_failure" {
		t.Errorf("expected type=dns_failure, got %v", payload["type"])
	}
	failures, ok := payload["failures"].([]interface{})
	if !ok || len(failures) != 1 {
		t.Fatalf("expected 1 failure entry, got %v", payload["failures"])
	}
	f0 := failures[0].(map[string]interface{})
	if f0["asn"] != "4242421234" {
		t.Errorf("expected ASN 4242421234, got %v", f0["asn"])
	}
	if f0["endpoint"] != "broken.example.com:51820" {
		t.Errorf("expected endpoint broken.example.com:51820, got %v", f0["endpoint"])
	}
}

func TestEnsureWG_IPEndpointNoDNSRecovery(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var wgQuickCalls int

	deps := testRecoveryDeps()
	deps.ReadDir = func(string) ([]os.DirEntry, error) {
		return []os.DirEntry{
			&fakeDirEntry{name: "dn42-1111.conf"},
		}, nil
	}
	deps.RunCommand = func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		if name == "wg" && len(args) >= 2 && args[0] == "show" && args[1] == "interfaces" {
			return "", nil
		}
		if name == "wg-quick" && len(args) >= 2 && args[0] == "up" {
			mu.Lock()
			wgQuickCalls++
			mu.Unlock()
			return "Error: Name or service not known", errors.New("exit status 1")
		}
		return "", nil
	}
	deps.ReadFile = func(string) ([]byte, error) {
		return []byte("[Peer]\nEndpoint = 1.2.3.4:51820\n"), nil
	}
	deps.HTTPPost = func(_ context.Context, _ string, _ []byte, _ map[string]string) error {
		t.Error("should not POST when endpoint is IP-based")
		return nil
	}

	EnsureWGInterfacesUp(context.Background(), testConfig(), deps)

	mu.Lock()
	defer mu.Unlock()
	if wgQuickCalls != 1 {
		t.Errorf("expected 1 wg-quick up call, got %d", wgQuickCalls)
	}
}

func TestEnsureWG_NoServerURLSkipsNotification(t *testing.T) {
	t.Parallel()
	deps := testRecoveryDeps()
	deps.ReadDir = func(string) ([]os.DirEntry, error) {
		return []os.DirEntry{
			&fakeDirEntry{name: "dn42-4242421234.conf"},
		}, nil
	}
	deps.RunCommand = func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		if name == "wg" && len(args) >= 2 && args[0] == "show" && args[1] == "interfaces" {
			return "", nil
		}
		if name == "wg-quick" {
			return "Name or service not known", errors.New("exit 1")
		}
		return "", nil
	}
	deps.ReadFile = func(string) ([]byte, error) {
		return []byte("[Peer]\nEndpoint = broken.example.com:51820\n"), nil
	}
	deps.HTTPPost = func(_ context.Context, _ string, _ []byte, _ map[string]string) error {
		t.Error("should not POST when ServerURL is empty")
		return nil
	}

	cfg := testConfig()
	cfg.ServerURL = ""
	EnsureWGInterfacesUp(context.Background(), cfg, deps)
}

func TestEnsureWG_ParallelExecution(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	activeWorkers := 0
	maxActiveWorkers := 0

	entries := make([]os.DirEntry, 6)
	for i := range entries {
		entries[i] = &fakeDirEntry{name: "dn42-" + strings.Repeat("1", i+1) + ".conf"}
	}

	deps := testRecoveryDeps()
	deps.ReadDir = func(string) ([]os.DirEntry, error) {
		return entries, nil
	}
	deps.RunCommand = func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		if name == "wg" && len(args) >= 2 && args[0] == "show" && args[1] == "interfaces" {
			return "", nil
		}
		if name == "wg-quick" && len(args) >= 2 && args[0] == "up" {
			mu.Lock()
			activeWorkers++
			if activeWorkers > maxActiveWorkers {
				maxActiveWorkers = activeWorkers
			}
			mu.Unlock()

			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			activeWorkers--
			mu.Unlock()
			return "", nil
		}
		return "", nil
	}
	deps.ReadFile = func(string) ([]byte, error) {
		return []byte("[Peer]\nEndpoint = 1.2.3.4:51820\n"), nil
	}

	EnsureWGInterfacesUp(context.Background(), testConfig(), deps)

	mu.Lock()
	defer mu.Unlock()
	if maxActiveWorkers > 3 {
		t.Errorf("max concurrent workers = %d, want <= 3", maxActiveWorkers)
	}
	if maxActiveWorkers == 0 {
		t.Error("expected at least 1 worker to run")
	}
}

func TestEnsureWG_ContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	deps := testRecoveryDeps()
	deps.ReadDir = func(string) ([]os.DirEntry, error) {
		return []os.DirEntry{
			&fakeDirEntry{name: "dn42-1.conf"},
		}, nil
	}
	deps.RunCommand = func(ctx context.Context, name string, args []string, _ time.Duration) (string, error) {
		if name == "wg" && len(args) >= 2 && args[0] == "show" && args[1] == "interfaces" {
			return "", nil
		}
		return "", ctx.Err()
	}
	deps.ReadFile = func(string) ([]byte, error) {
		return []byte("[Peer]\nEndpoint = 1.2.3.4:51820\n"), nil
	}

	EnsureWGInterfacesUp(ctx, testConfig(), deps)
}

func TestExtractASNFromIfaceName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		iface string
		want  string
	}{
		{"dn42-4242421234", "4242421234"},
		{"dn42-1234", "1234"},
		{"custom-iface", "custom-iface"},
	}
	for _, tc := range cases {
		t.Run(tc.iface, func(t *testing.T) {
			t.Parallel()
			got := extractASNFromIfaceName(tc.iface)
			if got != tc.want {
				t.Errorf("extractASNFromIfaceName(%q) = %q, want %q", tc.iface, got, tc.want)
			}
		})
	}
}

func TestExtractEndpointFromConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	t.Run("with endpoint", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(dir, "ep.conf")
		os.WriteFile(p, []byte("[Peer]\nEndpoint = host.example.com:51820\nAllowedIPs = ...\n"), 0644)
		got := extractEndpointFromConfig(p)
		if got != "host.example.com:51820" {
			t.Errorf("got %q, want host.example.com:51820", got)
		}
	})

	t.Run("no endpoint", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(dir, "no_ep.conf")
		os.WriteFile(p, []byte("[Peer]\nPublicKey = AAA=\n"), 0644)
		got := extractEndpointFromConfig(p)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		t.Parallel()
		got := extractEndpointFromConfig("/nonexistent/xyz.conf")
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestBuildNotificationPayload(t *testing.T) {
	t.Parallel()
	failures := []dnsFailure{
		{ASN: "4242421234", Endpoint: "broken.example.com:51820"},
		{ASN: "4242425678", Endpoint: "other.example.com:51820"},
	}

	payload := buildNotificationPayload(failures)

	var parsed map[string]interface{}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["type"] != "dns_failure" {
		t.Errorf("type = %v, want dns_failure", parsed["type"])
	}
	f, ok := parsed["failures"].([]interface{})
	if !ok {
		t.Fatal("failures is not an array")
	}
	if len(f) != 2 {
		t.Errorf("len(failures) = %d, want 2", len(f))
	}
}

type fakeDirEntry struct {
	name string
}

func (e *fakeDirEntry) Name() string               { return e.name }
func (e *fakeDirEntry) IsDir() bool                 { return false }
func (e *fakeDirEntry) Type() os.FileMode           { return 0 }
func (e *fakeDirEntry) Info() (os.FileInfo, error)  { return nil, nil }

func testRecoveryDeps() RecoveryDeps {
	return RecoveryDeps{
		RunCommand: func(_ context.Context, _ string, _ []string, _ time.Duration) (string, error) {
			return "", nil
		},
		ReadDir: func(string) ([]os.DirEntry, error) {
			return nil, os.ErrNotExist
		},
		ReadFile: func(string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
		WriteFile: func(_ string, _ []byte, _ os.FileMode) error {
			return nil
		},
		HTTPPost: func(_ context.Context, _ string, _ []byte, _ map[string]string) error {
			return nil
		},
	}
}
