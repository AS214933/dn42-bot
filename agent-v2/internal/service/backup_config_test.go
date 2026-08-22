package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSaveBackupBootstrapToConfigCreatesKeysPreservingComments(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := `# agent config
host: "0.0.0.0"
port: 54321
secret: "s" # keep me
my_dn42_link_local_address: "fe80::1816"
my_dn42_ula_address: "fd00::1"
my_dn42_ipv4_address: "172.20.0.1"

# backup section with a leading comment
backup:
  enabled: true # inline comment survives too
  interval: "5m"

auto_update:
  enabled: false
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	saved, err := SaveBackupBootstrapToConfig(path, "cn01", "https://git.example.com", "dn42-backup", "tok-123")
	if err != nil {
		t.Fatalf("SaveBackupBootstrapToConfig() error: %v", err)
	}
	if !saved {
		t.Fatal("expected saved=true for a fresh edit")
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)

	// Comments and unrelated keys must survive.
	for _, want := range []string{
		"# agent config",
		"secret: \"s\" # keep me",
		"# backup section with a leading comment",
		"enabled: true # inline comment survives too",
		"interval: \"5m\"",
		"auto_update:",
		"enabled: false",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lost %q:\n%s", want, text)
		}
	}
	// The four bootstrap keys must be present under backup.
	var cfg struct {
		Backup struct {
			NodeName    string `yaml:"node_name"`
			GitInstance string `yaml:"git_instance"`
			GitOrg      string `yaml:"git_org"`
			APIToken    string `yaml:"api_token"`
			Interval    string `yaml:"interval"`
			Enabled     bool   `yaml:"enabled"`
		} `yaml:"backup"`
	}
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Backup.NodeName != "cn01" || cfg.Backup.GitInstance != "https://git.example.com" ||
		cfg.Backup.GitOrg != "dn42-backup" || cfg.Backup.APIToken != "tok-123" {
		t.Fatalf("bootstrap fields not written: %+v", cfg.Backup)
	}
	if cfg.Backup.Interval != "5m" || !cfg.Backup.Enabled {
		t.Fatalf("existing backup keys damaged: %+v", cfg.Backup)
	}
}

func TestSaveBackupBootstrapToConfigIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := `secret: "s"
backup:
  node_name: "cn01"
  git_instance: "https://git.example.com"
  git_org: "dn42-backup"
  api_token: "tok-123"
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	saved, err := SaveBackupBootstrapToConfig(path, "cn01", "https://git.example.com", "dn42-backup", "tok-123")
	if err != nil {
		t.Fatalf("SaveBackupBootstrapToConfig() error: %v", err)
	}
	if saved {
		t.Error("expected saved=false when values already match")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("file changed despite matching values:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestSaveBackupBootstrapToConfigUpdatesExistingValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := `backup:
  enabled: false
  node_name: "old-node"
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	saved, err := SaveBackupBootstrapToConfig(path, "new-node", "https://git.example.com", "org", "tok")
	if err != nil {
		t.Fatalf("SaveBackupBootstrapToConfig() error: %v", err)
	}
	if !saved {
		t.Fatal("expected saved=true when updating stale values")
	}

	out, _ := os.ReadFile(path)
	var cfg struct {
		Backup struct {
			Enabled  bool   `yaml:"enabled"`
			NodeName string `yaml:"node_name"`
		} `yaml:"backup"`
	}
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Backup.NodeName != "new-node" || cfg.Backup.Enabled {
		t.Fatalf("unexpected backup block: %+v", cfg.Backup)
	}
}

func TestSaveBackupBootstrapToConfigCreatesBackupBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := `secret: "s"
open: true
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	saved, err := SaveBackupBootstrapToConfig(path, "cn01", "https://git.example.com", "org", "tok")
	if err != nil {
		t.Fatalf("SaveBackupBootstrapToConfig() error: %v", err)
	}
	if !saved {
		t.Fatal("expected saved=true when creating the backup block")
	}

	out, _ := os.ReadFile(path)
	var cfg struct {
		Secret string `yaml:"secret"`
		Backup struct {
			NodeName string `yaml:"node_name"`
		} `yaml:"backup"`
	}
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Secret != "s" || cfg.Backup.NodeName != "cn01" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestSaveBackupBootstrapToConfigRejectsBadInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Empty path.
	if _, err := SaveBackupBootstrapToConfig("", "n", "i", "o", "t"); err == nil {
		t.Error("empty path should fail")
	}

	// Missing file.
	if _, err := SaveBackupBootstrapToConfig(filepath.Join(dir, "missing.yaml"), "n", "i", "o", "t"); err == nil {
		t.Error("missing file should fail")
	}

	// Non-mapping top level.
	listPath := filepath.Join(dir, "list.yaml")
	if err := os.WriteFile(listPath, []byte("- a\n- b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveBackupBootstrapToConfig(listPath, "n", "i", "o", "t"); err == nil {
		t.Error("non-mapping config should fail")
	}
}
