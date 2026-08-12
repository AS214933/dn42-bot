package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

func TestNormalizeNodeName(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"CN01":         "cn01",
		"us-west01":    "us-west01",
		"Node_Name 01": "node-name-01",
		"  abc!@#DEF ": "abc---def",
	}
	for in, want := range tests {
		if got := normalizeNodeName(in); got != want {
			t.Errorf("normalizeNodeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseLegacyConfigExtractsCredentials(t *testing.T) {
	t.Parallel()
	cfg := config.BackupConfig{
		WorkDir:      "/tmp/new-work",
		BirdDir:      "/tmp/new-bird",
		WireGuardDir: "/tmp/new-wg",
	}
	input := `# bgp-backup config
NODE_NAME="cn01"
WORK_DIR="/var/lib/bgp-backup/repo"
BIRD_DIR="/etc/bird"
WG_DIR="/etc/wireguard"
GIT_INSTANCE="https://git.example.com"
GIT_ORG="dn42-backup"
REPO_NAME="cn01"
REPO_URL="https://alice:secret-token@git.example.com/dn42-backup/cn01.git"
INSTALL_DATE="2026-08-12 10:00:00"
`
	state, err := parseLegacyConfig([]byte(input), cfg)
	if err != nil {
		t.Fatalf("parseLegacyConfig returned error: %v", err)
	}
	if state.NodeName != "cn01" || state.GitInstance != "https://git.example.com" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.GitOrg != "dn42-backup" || state.RepoName != "cn01" {
		t.Fatalf("unexpected repository: %+v", state)
	}
	if state.GitUser != "alice" || state.APIToken != "secret-token" {
		t.Fatalf("credentials not extracted: %+v", state)
	}
	if state.WorkDir != "/var/lib/bgp-backup/repo" || state.BirdDir != "/etc/bird" || state.WireGuardDir != "/etc/wireguard" {
		t.Fatalf("legacy paths not preserved: %+v", state)
	}
}

func TestParseLegacyConfigIncomplete(t *testing.T) {
	t.Parallel()
	cfg := config.BackupConfig{WorkDir: "/tmp/new-work", BirdDir: "/tmp/new-bird", WireGuardDir: "/tmp/new-wg"}
	_, err := parseLegacyConfig([]byte(`NODE_NAME="cn01"`), cfg)
	if err == nil {
		t.Fatal("parseLegacyConfig() expected error for incomplete config")
	}
}

func TestAuthRepoURL(t *testing.T) {
	t.Parallel()
	state := &BackupState{
		GitInstance: "https://git.example.com/",
		GitOrg:      "dn42-backup",
		RepoName:    "cn01",
		GitUser:     "alice",
		APIToken:    "secret-token",
	}
	got := authRepoURL(state)
	want := "https://alice:secret-token@git.example.com/dn42-backup/cn01.git"
	if got != want {
		t.Fatalf("authRepoURL() = %q, want %q", got, want)
	}
}

func TestCopyDirExcludesAndDeletesExtraneous(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dst := t.TempDir()

	writeFile := func(root, name, content string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(src, "bird.conf", "bird", 0644)
	writeFile(src, "peers/1.conf", "peer", 0600)
	writeFile(src, "runtime.sock", "socket", 0600)
	writeFile(dst, "stale.conf", "stale", 0644)

	var dirMode os.FileMode = 0755
	var fileMode os.FileMode = 0644
	if err := copyDir(src, dst, []string{"*.sock"}, &dirMode, &fileMode); err != nil {
		t.Fatalf("copyDir() returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "bird.conf")); err != nil {
		t.Fatalf("bird.conf not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "peers", "1.conf")); err != nil {
		t.Fatalf("nested peer not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "runtime.sock")); !os.IsNotExist(err) {
		t.Fatalf("runtime.sock should be excluded, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "stale.conf")); !os.IsNotExist(err) {
		t.Fatalf("stale.conf should be removed, stat err = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "peers", "1.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "peer" {
		t.Fatalf("copied content = %q", string(data))
	}
}

func TestUninstallLegacyRemovesFilesAndStopsUnits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "bgp-backup.service"),
		filepath.Join(dir, "bgp-backup.timer"),
		filepath.Join(dir, "bgp-backup-sync.sh"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var commands []string
	manager := NewBackupManager(config.BackupConfig{StateFile: filepath.Join(dir, "state.yaml")}, BackupDeps{
		RunCommand: func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return "", nil
		},
	})
	if err := manager.uninstallLegacy(context.Background(), paths); err != nil {
		t.Fatalf("uninstallLegacy() returned error: %v", err)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists: %v", path, err)
		}
	}
	joined := strings.Join(commands, "\n")
	for _, want := range []string{
		"systemctl stop bgp-backup.timer bgp-backup.service",
		"systemctl disable bgp-backup.timer bgp-backup.service",
		"systemctl daemon-reload",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing command %q in %q", want, joined)
		}
	}
}

func TestBackupManagerStatusRedactsToken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manager := NewBackupManager(config.BackupConfig{StateFile: filepath.Join(dir, "state.yaml")}, BackupDeps{})
	manager.mu.Lock()
	manager.state = &BackupState{
		NodeName:     "cn01",
		GitInstance:  "https://git.example.com",
		GitOrg:       "dn42-backup",
		RepoName:     "cn01",
		GitUser:      "alice",
		APIToken:     "secret-token",
		WorkDir:      "/tmp/work",
		BirdDir:      "/etc/bird",
		WireGuardDir: "/etc/wireguard",
	}
	manager.enabled = true
	manager.mu.Unlock()

	status := manager.Status()
	if status.RepoURL != "https://git.example.com/dn42-backup/cn01" {
		t.Fatalf("RepoURL = %q", status.RepoURL)
	}
	if strings.Contains(fmt.Sprint(status), "secret-token") {
		t.Fatalf("status leaked API token: %+v", status)
	}
}

func TestBackupManagerInstallCreate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	birdDir := filepath.Join(dir, "bird")
	wgDir := filepath.Join(dir, "wireguard")
	if err := os.MkdirAll(birdDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wgDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(birdDir, "bird.conf"), []byte("bird"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wgDir, "wg0.conf"), []byte("wg"), 0600); err != nil {
		t.Fatal(err)
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/user":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"login":"alice"}`))
		case r.URL.Path == "/api/v1/repos/dn42-backup/cn01":
			http.NotFound(w, r)
		case r.URL.Path == "/api/v1/orgs/dn42-backup/repos" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"cn01"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manager := NewBackupManager(config.BackupConfig{
		StateFile:    filepath.Join(dir, "state.yaml"),
		WorkDir:      filepath.Join(dir, "repo"),
		BirdDir:      birdDir,
		WireGuardDir: wgDir,
	}, BackupDeps{
		HTTPClient: server.Client(),
		RunCommand: func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
			if name == "git" && len(args) > 0 && args[0] == "-C" && args[2] == "status" {
				return "?? bird/\n", nil
			}
			return "", nil
		},
	})

	result, err := manager.Install(context.Background(), BackupInstallRequest{
		NodeName:    "CN01",
		GitInstance: server.URL,
		GitOrg:      "dn42-backup",
		APIToken:    "secret-token",
	})
	if err != nil {
		t.Fatalf("Install() returned error: %v", err)
	}
	if !result.Installed || result.ActionTaken != "create" || result.RepoName != "cn01" {
		t.Fatalf("unexpected result: %+v", result)
	}

	state, err := manager.readState()
	if err != nil {
		t.Fatalf("readState() returned error: %v", err)
	}
	if state == nil || state.APIToken != "secret-token" || state.GitUser != "alice" {
		t.Fatalf("state not persisted: %+v", state)
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]interface{}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted["api_token"] != "secret-token" {
		t.Fatalf("persisted api_token = %v", persisted["api_token"])
	}
}
