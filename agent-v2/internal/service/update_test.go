package service

import (
	"context"
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

func TestReleaseUpdaterCheckCandidateIncludesPrerelease(t *testing.T) {
	server := updateTestServer(t)
	updater := newTestReleaseUpdater(t, server.URL, "v2.0.0-alpha.3")

	status, err := updater.Check(context.Background(), "candidate")
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}

	if status.LatestVersion != "v2.0.0-alpha.4" {
		t.Fatalf("LatestVersion = %q, want v2.0.0-alpha.4", status.LatestVersion)
	}
	if !status.UpdateAvailable {
		t.Fatal("UpdateAvailable = false, want true")
	}
	if !status.Prerelease {
		t.Fatal("Prerelease = false, want true")
	}
}

func TestReleaseUpdaterCheckStableSkipsPrerelease(t *testing.T) {
	server := updateTestServer(t)
	updater := newTestReleaseUpdater(t, server.URL, "v1.9.0")

	status, err := updater.Check(context.Background(), "stable")
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}

	if status.LatestVersion != "v2.0.0" {
		t.Fatalf("LatestVersion = %q, want v2.0.0", status.LatestVersion)
	}
	if status.Prerelease {
		t.Fatal("Prerelease = true, want false")
	}
}

func TestReleaseUpdaterInstallReplacesConfiguredAgentPath(t *testing.T) {
	server := updateTestServer(t)
	updater := newTestReleaseUpdater(t, server.URL, "v2.0.0-alpha.3")

	if err := os.WriteFile(updater.cfg.AgentPath, []byte("old-agent"), 0755); err != nil {
		t.Fatalf("write old agent: %v", err)
	}

	status, err := updater.Install(context.Background(), "candidate", false)
	if err != nil {
		t.Fatalf("Install() returned error: %v", err)
	}
	if !status.Installed || !status.RestartRequired {
		t.Fatalf("status = %+v, want installed and restart required", status)
	}

	data, err := os.ReadFile(updater.cfg.AgentPath)
	if err != nil {
		t.Fatalf("read installed agent: %v", err)
	}
	if string(data) != "new-agent-binary" {
		t.Fatalf("installed agent = %q, want new-agent-binary", string(data))
	}
}

func TestReleaseUpdaterRestartUsesSystemctlService(t *testing.T) {
	dir := t.TempDir()
	servicePath := filepath.Join(dir, "dn42-agent.service")
	if err := os.WriteFile(servicePath, []byte("[Service]\n"), 0644); err != nil {
		t.Fatalf("write service file: %v", err)
	}

	var commands []string
	updater := NewReleaseUpdater(config.AutoUpdateConfig{
		Channel:       "candidate",
		CheckInterval: time.Hour,
		Repository:    "AS214933/dn42-bot",
		DataDir:       dir,
		AgentPath:     filepath.Join(dir, "agent"),
		ServiceName:   "dn42-agent.service",
		ServicePath:   servicePath,
	}, ReleaseUpdaterDeps{
		RunCommand: func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return "", nil
		},
	})

	if err := updater.Restart(context.Background()); err != nil {
		t.Fatalf("Restart() returned error: %v", err)
	}

	want := []string{
		"systemctl daemon-reload",
		"systemctl restart dn42-agent.service",
	}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands = %v, want %v", commands, want)
	}
}

func TestUpdateAvailableSemver(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{"v2.0.0-alpha.3", "v2.0.0-alpha.4", true},
		{"v2.0.0-alpha.4", "v2.0.0-alpha.4", false},
		{"v2.0.0-alpha.4", "v2.0.0", true},
		{"v2.0.0", "v2.0.0-alpha.4", false},
		{"dev", "v2.0.0-alpha.4", true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_to_%s", tt.current, tt.latest), func(t *testing.T) {
			if got := updateAvailable(tt.current, tt.latest); got != tt.want {
				t.Fatalf("updateAvailable(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func newTestReleaseUpdater(t *testing.T, apiBaseURL, current string) *ReleaseUpdater {
	t.Helper()
	dir := t.TempDir()
	servicePath := filepath.Join(dir, "dn42-agent.service")
	if err := os.WriteFile(servicePath, []byte("[Service]\n"), 0644); err != nil {
		t.Fatalf("write service file: %v", err)
	}
	return NewReleaseUpdater(config.AutoUpdateConfig{
		Channel:       "candidate",
		CheckInterval: time.Hour,
		Repository:    "AS214933/dn42-bot",
		DataDir:       dir,
		AgentPath:     filepath.Join(dir, "agent"),
		ServiceName:   "dn42-agent.service",
		ServicePath:   servicePath,
	}, ReleaseUpdaterDeps{
		HTTPClient:     http.DefaultClient,
		APIBaseURL:     apiBaseURL,
		CurrentVersion: current,
		BuildCommit:    "test",
		GOOS:           "linux",
		GOARCH:         "amd64",
	})
}

func updateTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/AS214933/dn42-bot/releases":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `[
				{
					"tag_name": "v2.0.0-alpha.4",
					"draft": false,
					"prerelease": true,
					"html_url": "%[1]s/releases/tag/v2.0.0-alpha.4",
					"assets": [
						{"name": "agent-v2-linux-amd64", "browser_download_url": "%[1]s/assets/agent-v2-linux-amd64"}
					]
				},
				{
					"tag_name": "v2.0.0",
					"draft": false,
					"prerelease": false,
					"html_url": "%[1]s/releases/tag/v2.0.0",
					"assets": [
						{"name": "agent-v2-linux-amd64", "browser_download_url": "%[1]s/assets/agent-v2-linux-amd64"}
					]
				}
			]`, server.URL)
		case r.URL.Path == "/assets/agent-v2-linux-amd64":
			_, _ = w.Write([]byte("new-agent-binary"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}
