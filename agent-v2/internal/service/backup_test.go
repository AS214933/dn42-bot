package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

// requireGit skips git-dependent integration tests when no git binary exists.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

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

func TestBackupManagerMigrateLegacy(t *testing.T) {
	dir := t.TempDir()
	legacyConfig := filepath.Join(dir, "bgp-backup.conf")
	legacyService := filepath.Join(dir, "bgp-backup.service")
	legacyTimer := filepath.Join(dir, "bgp-backup.timer")
	legacyScript := filepath.Join(dir, "bgp-backup-sync.sh")
	for _, path := range []string{legacyConfig, legacyService, legacyTimer, legacyScript} {
		if err := os.WriteFile(path, []byte("legacy"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(legacyConfig, []byte(`NODE_NAME="cn01"
WORK_DIR="/var/lib/bgp-backup/repo"
BIRD_DIR="/etc/bird"
WG_DIR="/etc/wireguard"
GIT_INSTANCE="https://git.example.com"
GIT_ORG="dn42-backup"
REPO_NAME="cn01"
REPO_URL="https://alice:secret-token@git.example.com/dn42-backup/cn01.git"
INSTALL_DATE="2026-08-12 10:00:00"
`), 0600); err != nil {
		t.Fatal(err)
	}

	stateFile := filepath.Join(dir, "backup.yaml")
	var commands []string
	manager := NewBackupManager(config.BackupConfig{
		StateFile:    stateFile,
		WorkDir:      filepath.Join(dir, "new-repo"),
		BirdDir:      filepath.Join(dir, "bird"),
		WireGuardDir: filepath.Join(dir, "wireguard"),
	}, BackupDeps{
		LegacyConfigPath: legacyConfig,
		LegacyPaths:      []string{legacyConfig, legacyService, legacyTimer, legacyScript},
		RunCommand: func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return "", nil
		},
	})

	result, err := manager.MigrateLegacy(context.Background())
	if err != nil {
		t.Fatalf("MigrateLegacy() returned error: %v", err)
	}
	if !result.Detected || !result.Migrated || !result.Uninstalled {
		t.Fatalf("unexpected migration result: %+v", result)
	}

	state, err := manager.readState()
	if err != nil {
		t.Fatalf("readState() returned error: %v", err)
	}
	if state == nil || state.NodeName != "cn01" || state.APIToken != "secret-token" || state.GitUser != "alice" {
		t.Fatalf("migrated state = %+v", state)
	}
	if state.WorkDir != "/var/lib/bgp-backup/repo" {
		t.Fatalf("legacy work dir not preserved: %+v", state)
	}
	for _, path := range []string{legacyConfig, legacyService, legacyTimer, legacyScript} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy file %s still exists: %v", path, err)
		}
	}
	if !manager.IsEnabled() {
		t.Fatal("manager should be enabled after migration")
	}
	joined := strings.Join(commands, "\n")
	if !strings.Contains(joined, "systemctl stop bgp-backup.timer bgp-backup.service") {
		t.Fatalf("legacy timer was not stopped: %q", joined)
	}
}

// initBareRepo creates a bare remote repository seeded with an initial commit.
func initBareRepo(t *testing.T, dir, seedContent string) string {
	t.Helper()
	requireGit(t)
	remote := filepath.Join(dir, "remote.git")
	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=seed", "GIT_AUTHOR_EMAIL=seed@test",
			"GIT_COMMITTER_NAME=seed", "GIT_COMMITTER_EMAIL=seed@test",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	run("git", "init", "-q", "--bare", "-b", "main", remote)
	work := filepath.Join(dir, "seed-work")
	run("git", "init", "-q", "-b", "main", work)
	if err := os.MkdirAll(filepath.Join(work, "bird"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "bird", "bird.conf"), []byte(seedContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(work, "wireguard"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "wireguard", "wg0.conf"), []byte("seed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("git", "-C", work, "add", "-A")
	run("git", "-C", work, "commit", "-q", "-m", "seed")
	run("git", "-C", work, "push", "-q", remote, "main")
	os.RemoveAll(work)
	return remote
}

func gitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestBackupSyncConflictKeepsRemote is the regression test for the inverted
// conflict resolution: when the node and the remote both changed the same
// file, the remote (human) edit must survive in /etc after sync.
func TestBackupSyncConflictKeepsRemote(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()

	birdDir := filepath.Join(dir, "bird")
	wgDir := filepath.Join(dir, "wireguard")
	for _, d := range []string{birdDir, wgDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Node's local /etc/bird/bird.conf diverges from what the human pushed.
	if err := os.WriteFile(filepath.Join(birdDir, "bird.conf"), []byte("NODE-EDIT\n"), 0644); err != nil {
		t.Fatal(err)
	}

	remote := initBareRepo(t, dir, "original\n")

	// Human edits bird.conf on the remote via a separate clone.
	human := filepath.Join(dir, "human")
	if _, err := gitIn(dir, "clone", "-q", remote, human); err != nil {
		t.Fatalf("clone: %v", err)
	}
	humanEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=human", "GIT_AUTHOR_EMAIL=h@test",
		"GIT_COMMITTER_NAME=human", "GIT_COMMITTER_EMAIL=h@test",
	)
	if err := os.WriteFile(filepath.Join(human, "bird", "bird.conf"), []byte("HUMAN-EDIT\n"), 0644); err != nil {
		t.Fatal(err)
	}
	commitCmd := exec.Command("git", "-C", human, "add", "-A")
	commitCmd.Env = humanEnv
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	commitCmd = exec.Command("git", "-C", human, "commit", "-q", "-m", "human edit")
	commitCmd.Env = humanEnv
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	pushCmd := exec.Command("git", "-C", human, "push", "-q", "origin", "main")
	if out, err := pushCmd.CombinedOutput(); err != nil {
		t.Fatalf("git push: %v\n%s", err, out)
	}

	// The node's work repo still sits at the seed commit (stale clone).
	state := &BackupState{
		NodeName:     "cn01",
		GitInstance:  "file://" + remote,
		GitOrg:       "org",
		RepoName:     "cn01",
		GitUser:      "node",
		APIToken:     "token",
		WorkDir:      filepath.Join(dir, "work"),
		BirdDir:      birdDir,
		WireGuardDir: wgDir,
		InstalledAt:  time.Now().UTC().Format(time.RFC3339),
	}
	manager := NewBackupManager(config.BackupConfig{
		StateFile:    filepath.Join(dir, "state.json"),
		WorkDir:      state.WorkDir,
		BirdDir:      birdDir,
		WireGuardDir: wgDir,
	}, BackupDeps{})
	manager.mu.Lock()
	manager.state = state
	manager.enabled = true
	manager.mu.Unlock()
	// Pre-seed the work repo at the seed revision so histories diverge.
	if err := os.MkdirAll(state.WorkDir, 0750); err != nil {
		t.Fatal(err)
	}
	if _, err := gitIn(state.WorkDir, "init", "-q"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	remoteURL := "file://" + remote
	if _, err := gitIn(state.WorkDir, "remote", "add", "origin", remoteURL); err != nil {
		t.Fatalf("git remote add: %v", err)
	}
	if _, err := gitIn(state.WorkDir, "fetch", "-q", "origin", "main"); err != nil {
		t.Fatalf("git fetch: %v", err)
	}
	// Check out the seed revision (two commits behind the human edit).
	seedRev, err := gitIn(state.WorkDir, "rev-list", "--max-parents=0", "origin/main")
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	if _, err := gitIn(state.WorkDir, "checkout", "-q", "-b", "main", strings.TrimSpace(seedRev)); err != nil {
		t.Fatalf("checkout: %v", err)
	}

	result, err := manager.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync() returned error: %v", err)
	}
	if !result.RemoteMerged {
		t.Fatalf("expected RemoteMerged=true, got %+v", result)
	}

	// The authoritative content must be back in /etc.
	data, err := os.ReadFile(filepath.Join(birdDir, "bird.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "HUMAN-EDIT\n" {
		t.Fatalf("after sync /etc/bird/bird.conf = %q, want HUMAN-EDIT (remote wins conflicts)", string(data))
	}

	// The remote history must contain the human edit as the tip: the node
	// re-sampled the restored content, so no conflicting local commit remains.
	logOut, err := gitIn(state.WorkDir, "log", "--oneline", "-3")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logOut, "human edit") {
		t.Fatalf("remote history lost human edit:\n%s", logOut)
	}
}

// gitHTTPServer serves real git smart-HTTP for the given repos root using
// git-http-backend, plus Forgejo-style /api/v1 endpoints driven by api.
func gitHTTPServer(t *testing.T, reposRoot string, api func(w http.ResponseWriter, r *http.Request) bool) *httptest.Server {
	t.Helper()
	requireGit(t)
	backendOut, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Fatalf("git --exec-path: %v", err)
	}
	execPath := strings.TrimSpace(string(backendOut))

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			api(w, r)
			return
		}
		// Delegate everything else to git-http-backend (smart HTTP). The
		// backend emits CGI-style responses; translate the header block.
		cmd := exec.Command(filepath.Join(execPath, "git-http-backend"))
		env := append(os.Environ(),
			"GIT_PROJECT_ROOT="+reposRoot,
			"GIT_HTTP_EXPORT_ALL=1",
			"GIT_COMMITTER_NAME=agent-test", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_AUTHOR_NAME=agent-test", "GIT_AUTHOR_EMAIL=t@t",
			"PATH_INFO="+r.URL.Path,
			"QUERY_STRING="+r.URL.RawQuery,
			"REQUEST_METHOD="+r.Method,
			"CONTENT_TYPE="+r.Header.Get("Content-Type"),
			"REMOTE_ADDR="+r.RemoteAddr,
		)
		if proto := r.Header.Get("Git-Protocol"); proto != "" {
			env = append(env, "GIT_PROTOCOL="+proto)
		}
		cmd.Env = env
		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		if r.Body != nil {
			cmd.Stdin = r.Body
		}
		runErr := cmd.Run()
		head := outBuf.Bytes()
		body := []byte(nil)
		if idx := bytes.Index(head, []byte("\r\n\r\n")); idx >= 0 {
			for _, line := range strings.Split(string(head[:idx]), "\r\n") {
				if kv := strings.SplitN(line, ":", 2); len(kv) == 2 {
					if strings.EqualFold(kv[0], "Status") {
						continue
					}
					w.Header().Set(strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]))
				}
			}
			body = head[idx+4:]
		} else {
			body = head
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		if runErr != nil {
			t.Logf("git-http-backend %s %s failed: %v\n%s", r.Method, r.URL.Path, runErr, errBuf.String())
		}
	}))
}

// TestBootstrapRestoresFromExistingRepo covers node migration: a fresh node
// with credentials in config.yaml and no local state adopts an existing
// remote repository, pulls its content back onto /etc, and reports restore.
func TestBootstrapRestoresFromExistingRepo(t *testing.T) {
	dir := t.TempDir()

	birdDir := filepath.Join(dir, "bird")
	wgDir := filepath.Join(dir, "wireguard")
	if err := os.MkdirAll(birdDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wgDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Fresh node: /etc is empty (nothing to lose) — this is why remote wins.

	// Seed an existing backup repo whose bird/bird.conf = REMOTE-BIRD.
	reposRoot := filepath.Join(dir, "repos")
	repoPath := filepath.Join(reposRoot, "dn42-backup", "cn01.git")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-a", initBareRepo(t, dir, "REMOTE-BIRD\n")+"/.", repoPath).CombinedOutput(); err != nil {
		t.Fatalf("copy seed repo: %v\n%s", err, out)
	}

	var server *httptest.Server
	server = gitHTTPServer(t, reposRoot, func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/api/v1/user":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"login":"alice"}`))
		case "/api/v1/repos/dn42-backup/cn01":
			// Remote repo exists → bootstrap must choose restore.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"cn01"}`))
		default:
			http.NotFound(w, r)
		}
		return false
	})
	defer server.Close()

	manager := NewBackupManager(config.BackupConfig{
		StateFile:    filepath.Join(dir, "state.json"),
		WorkDir:      filepath.Join(dir, "work"),
		BirdDir:      birdDir,
		WireGuardDir: wgDir,
		NodeName:     "cn01",
		GitInstance:  server.URL,
		GitOrg:       "dn42-backup",
		APIToken:     "secret-token",
	}, BackupDeps{
		HTTPClient: server.Client(),
	})

	result, err := manager.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap() returned error: %v", err)
	}
	if !result.Installed || result.ActionTaken != "restore" || !result.RepoExisted {
		t.Fatalf("unexpected bootstrap result: %+v", result)
	}

	// The remote backup was pulled back onto /etc.
	data, err := os.ReadFile(filepath.Join(birdDir, "bird.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "REMOTE-BIRD\n" {
		t.Fatalf("bird.conf after bootstrap = %q, want REMOTE-BIRD", string(data))
	}

	state, err := manager.readState()
	if err != nil || state == nil || state.NodeName != "cn01" || state.GitUser != "alice" {
		t.Fatalf("state not persisted after bootstrap: %+v err=%v", state, err)
	}
	if !manager.IsEnabled() {
		t.Fatal("manager should be enabled after bootstrap")
	}
}

// TestBootstrapCreatesMissingRepo covers first-time install via config: no
// remote repo exists yet, so the current local config becomes the initial
// backup pushed to a freshly created repository.
func TestBootstrapCreatesMissingRepo(t *testing.T) {
	dir := t.TempDir()

	birdDir := filepath.Join(dir, "bird")
	wgDir := filepath.Join(dir, "wireguard")
	if err := os.MkdirAll(birdDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wgDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(birdDir, "bird.conf"), []byte("LOCAL-CONFIG\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reposRoot := filepath.Join(dir, "repos")
	repoPath := filepath.Join(reposRoot, "dn42-backup", "cn02.git")

	var server *httptest.Server
	server = gitHTTPServer(t, reposRoot, func(w http.ResponseWriter, r *http.Request) bool {
		switch {
		case r.URL.Path == "/api/v1/user":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"login":"alice"}`))
		case r.URL.Path == "/api/v1/repos/dn42-backup/cn02":
			http.NotFound(w, r)
		case r.URL.Path == "/api/v1/orgs/dn42-backup/repos" && r.Method == http.MethodPost:
			// Simulate Forgejo creating the (empty) repository.
			if err := os.MkdirAll(repoPath, 0755); err != nil {
				t.Errorf("create repo dir: %v", err)
			}
			if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", repoPath).CombinedOutput(); err != nil {
				t.Errorf("git init --bare: %v\n%s", err, out)
			}
			_, _ = exec.Command("git", "-C", repoPath, "config", "http.receivepack", "true").CombinedOutput()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"cn02"}`))
		default:
			http.NotFound(w, r)
		}
		return false
	})
	defer server.Close()

	manager := NewBackupManager(config.BackupConfig{
		StateFile:    filepath.Join(dir, "state.json"),
		WorkDir:      filepath.Join(dir, "work"),
		BirdDir:      birdDir,
		WireGuardDir: wgDir,
		NodeName:     "cn02",
		GitInstance:  server.URL,
		GitOrg:       "dn42-backup",
		APIToken:     "secret-token",
	}, BackupDeps{
		HTTPClient: server.Client(),
	})

	result, err := manager.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap() returned error: %v", err)
	}
	if !result.Installed || result.ActionTaken != "create" || result.RepoExisted {
		t.Fatalf("unexpected bootstrap result: %+v", result)
	}

	// The created repo holds the node's initial configuration.
	workClone := filepath.Join(dir, "verify-clone")
	if _, err := gitIn(dir, "clone", "-q", repoPath, workClone); err != nil {
		t.Fatalf("verify clone: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(workClone, "bird", "bird.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "LOCAL-CONFIG\n" {
		t.Fatalf("pushed bird.conf = %q, want LOCAL-CONFIG", string(data))
	}
}
