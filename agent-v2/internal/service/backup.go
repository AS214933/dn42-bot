package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/birdctl"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

const (
	legacyBackupConfigPath  = "/etc/bgp-backup/bgp-backup.conf"
	legacyBackupServicePath = "/etc/systemd/system/bgp-backup.service"
	legacyBackupTimerPath   = "/etc/systemd/system/bgp-backup.timer"
	legacyBackupScriptPath  = "/usr/local/bin/bgp-backup-sync.sh"
)

type BackupState struct {
	NodeName     string `json:"node_name"`
	WorkDir      string `json:"work_dir"`
	BirdDir      string `json:"bird_dir"`
	WireGuardDir string `json:"wireguard_dir"`
	GitInstance  string `json:"git_instance"`
	GitOrg       string `json:"git_org"`
	RepoName     string `json:"repo_name"`
	GitUser      string `json:"git_user"`
	APIToken     string `json:"api_token"`
	InstalledAt  string `json:"installed_at"`
}

type BackupInstallRequest struct {
	NodeName           string `json:"node_name"`
	GitInstance        string `json:"git_instance"`
	GitOrg             string `json:"git_org"`
	APIToken           string `json:"api_token"`
	ExistingRepoAction string `json:"existing_repo_action"`
}

type BackupInstallResult struct {
	Installed   bool   `json:"installed"`
	NodeName    string `json:"node_name"`
	RepoName    string `json:"repo_name"`
	RepoURL     string `json:"repo_url"`
	RepoExisted bool   `json:"repo_existed"`
	ActionTaken string `json:"action_taken"`
}

type BackupStatus struct {
	Enabled        bool     `json:"enabled"`
	Installed      bool     `json:"installed"`
	LegacyDetected bool     `json:"legacy_detected"`
	LegacyMigrated bool     `json:"legacy_migrated"`
	NodeName       string   `json:"node_name,omitempty"`
	GitInstance    string   `json:"git_instance,omitempty"`
	GitOrg         string   `json:"git_org,omitempty"`
	RepoName       string   `json:"repo_name,omitempty"`
	RepoURL        string   `json:"repo_url,omitempty"`
	WorkDir        string   `json:"work_dir,omitempty"`
	BirdDir        string   `json:"bird_dir,omitempty"`
	WireGuardDir   string   `json:"wireguard_dir,omitempty"`
	LastSyncAt     string   `json:"last_sync_at,omitempty"`
	LastSyncError  string   `json:"last_sync_error,omitempty"`
	LegacyPaths    []string `json:"legacy_paths,omitempty"`
	BootstrapDone  bool     `json:"bootstrap_done,omitempty"`
}

type BackupSyncResult struct {
	Synced       bool   `json:"synced"`
	Committed    bool   `json:"committed"`
	Pushed       bool   `json:"pushed"`
	RemoteMerged bool   `json:"remote_merged"`
	Restored     bool   `json:"restored"`
	BirdReloaded bool   `json:"bird_reloaded"`
	WGRestarted  int    `json:"wg_restarted"`
	Message      string `json:"message,omitempty"`
}

type BackupMigrationResult struct {
	Detected    bool     `json:"detected"`
	Migrated    bool     `json:"migrated"`
	Uninstalled bool     `json:"uninstalled"`
	ConfigSaved bool     `json:"config_saved"`
	LegacyPaths []string `json:"legacy_paths"`
	Message     string   `json:"message,omitempty"`
}

type BackupDeps struct {
	RunCommand       func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error)
	HTTPClient       *http.Client
	Now              func() time.Time
	LegacyConfigPath string
	LegacyPaths      []string
	BirdCtlPath      string
}

type BackupManager struct {
	cfg  config.BackupConfig
	deps BackupDeps

	mu             sync.Mutex
	state          *BackupState
	enabled        bool
	legacyMigrated bool
	bootstrapDone  bool
	lastSyncAt     time.Time
	lastSyncErr    string
}

func NewBackupManager(cfg config.BackupConfig, deps BackupDeps) *BackupManager {
	runCmd := deps.RunCommand
	if runCmd == nil {
		runCmd = RunCommand
	}
	client := deps.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	legacyConfigPath := deps.LegacyConfigPath
	if legacyConfigPath == "" {
		legacyConfigPath = legacyBackupConfigPath
	}
	legacyPaths := append([]string{}, deps.LegacyPaths...)
	if len(legacyPaths) == 0 {
		legacyPaths = []string{legacyBackupConfigPath, legacyBackupServicePath, legacyBackupTimerPath, legacyBackupScriptPath}
	}
	birdCtlPath := deps.BirdCtlPath
	if birdCtlPath == "" {
		birdCtlPath = birdctl.DefaultSocketPath
	}

	m := &BackupManager{
		cfg: cfg,
		deps: BackupDeps{
			RunCommand:       runCmd,
			HTTPClient:       client,
			Now:              now,
			LegacyConfigPath: legacyConfigPath,
			LegacyPaths:      legacyPaths,
			BirdCtlPath:      birdCtlPath,
		},
	}

	if state, err := m.readState(); err == nil && state != nil {
		m.state = state
	}
	m.enabled = cfg.Enabled || m.state != nil
	return m
}

// SyncStateIntoConfig folds an existing backup.yaml state file into the
// agent's config.yaml: when the config's `backup:` block is missing any of
// the four bootstrap fields, the values from the state file are written in
// (comment-preserving) and the state file is renamed to "<state>.old" so the
// migration happens exactly once. The manager keeps operating from the
// in-memory state either way; a failure here only means the operator should
// add the fields by hand. Returns true when the fold was performed.
func (m *BackupManager) SyncStateIntoConfig(configPath string) bool {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state == nil || configPath == "" {
		return false
	}
	if cfgHasAllBootstrapFields(m.cfg) {
		return false
	}

	saved, err := SaveBackupBootstrapToConfig(configPath, state.NodeName, state.GitInstance, state.GitOrg, state.APIToken)
	if err != nil {
		log.Printf("WARN: could not record backup credentials from %s into %s: %v", m.cfg.StateFile, configPath, err)
		return false
	}
	if !saved {
		// Config already had everything under different capitalization of the
		// check above cannot happen, but stay safe: nothing to migrate.
		return false
	}
	oldPath := m.cfg.StateFile + ".old"
	if err := os.Rename(m.cfg.StateFile, oldPath); err != nil {
		log.Printf("WARN: wrote bootstrap fields into %s but could not retire %s: %v", configPath, m.cfg.StateFile, err)
		return true
	}
	log.Printf("backup credentials from %s recorded in %s; state file moved to %s", m.cfg.StateFile, configPath, oldPath)
	return true
}

func cfgHasAllBootstrapFields(cfg config.BackupConfig) bool {
	return cfg.NodeName != "" && cfg.GitInstance != "" && cfg.GitOrg != "" && cfg.APIToken != ""
}

func (m *BackupManager) IsEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

func (m *BackupManager) Status() BackupStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := BackupStatus{
		Enabled:        m.enabled,
		Installed:      m.state != nil,
		LegacyMigrated: m.legacyMigrated,
		BootstrapDone:  m.bootstrapDone,
	}
	legacyPaths := m.existingLegacyPaths()
	status.LegacyDetected = len(legacyPaths) > 0
	status.LegacyPaths = legacyPaths
	if m.state != nil {
		status.NodeName = m.state.NodeName
		status.GitInstance = m.state.GitInstance
		status.GitOrg = m.state.GitOrg
		status.RepoName = m.state.RepoName
		status.RepoURL = publicRepoURL(m.state)
		status.WorkDir = m.state.WorkDir
		status.BirdDir = m.state.BirdDir
		status.WireGuardDir = m.state.WireGuardDir
	}
	if !m.lastSyncAt.IsZero() {
		status.LastSyncAt = m.lastSyncAt.UTC().Format(time.RFC3339)
	}
	status.LastSyncError = m.lastSyncErr
	return status
}

func (m *BackupManager) Run(ctx context.Context) {
	m.mu.Lock()
	enabled := m.enabled
	interval := m.cfg.Interval
	onBootDelay := m.cfg.OnBootDelay
	randomDelay := m.cfg.RandomDelay
	needsBootstrap := m.state == nil && m.bootstrapRequest() != nil
	m.mu.Unlock()

	if !enabled && !needsBootstrap {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if onBootDelay < 0 {
		onBootDelay = 0
	}
	runBootstrap := func() {
		if _, err := m.Bootstrap(ctx); err != nil {
			log.Printf("bgp backup bootstrap failed: %v", err)
			m.mu.Lock()
			m.lastSyncAt = m.deps.Now().UTC()
			m.lastSyncErr = "bootstrap: " + err.Error()
			m.mu.Unlock()
		}
	}
	if needsBootstrap {
		// Bootstrap (install or restore-from-remote) must not wait out the
		// normal on-boot jitter: a migrated node wants its config back ASAP.
		runBootstrap()
	}
	delay := onBootDelay
	if randomDelay > 0 {
		delay += time.Duration(rand.Int63n(int64(randomDelay) + 1))
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			// Retry bootstrap first: until it succeeds the node has no state.
			m.mu.Lock()
			stillNeedsBootstrap := m.state == nil && m.bootstrapRequest() != nil
			m.mu.Unlock()
			if stillNeedsBootstrap {
				runBootstrap()
				m.mu.Lock()
				installed := m.state != nil
				m.mu.Unlock()
				if !installed {
					timer.Reset(interval)
					continue
				}
			}
			if _, err := m.Sync(ctx); err != nil {
				log.Printf("bgp backup sync failed: %v", err)
			}
			timer.Reset(interval)
		}
	}
}

func (m *BackupManager) Sync(ctx context.Context) (BackupSyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == nil {
		state, err := m.readState()
		if err != nil {
			return BackupSyncResult{}, err
		}
		if state == nil {
			return BackupSyncResult{}, fmt.Errorf("bgp backup is not installed")
		}
		m.state = state
	}

	result, err := m.syncLocked(ctx)
	if err != nil {
		m.lastSyncAt = m.deps.Now().UTC()
		m.lastSyncErr = err.Error()
		return result, err
	}
	m.lastSyncAt = m.deps.Now().UTC()
	m.lastSyncErr = ""
	return result, nil
}

// bootstrapRequest returns install credentials from config.yaml when the
// unattended bootstrap block is fully populated and no state exists yet.
func (m *BackupManager) bootstrapRequest() *BackupInstallRequest {
	if m.cfg.APIToken == "" || m.cfg.GitInstance == "" || m.cfg.GitOrg == "" || m.cfg.NodeName == "" {
		return nil
	}
	return &BackupInstallRequest{
		NodeName:    m.cfg.NodeName,
		GitInstance: m.cfg.GitInstance,
		GitOrg:      m.cfg.GitOrg,
		APIToken:    m.cfg.APIToken,
	}
}

// Bootstrap self-installs the backup on a node that has credentials in
// config.yaml but no local state yet — the node-migration scenario. The
// remote repository is always authoritative: when it already exists the
// remote content is pulled back onto /etc (restore) and daemons restart;
// only a missing repository is created from current local configuration.
func (m *BackupManager) Bootstrap(ctx context.Context) (BackupInstallResult, error) {
	m.mu.Lock()
	req := m.bootstrapRequest()
	installed := m.state != nil
	m.mu.Unlock()
	if req == nil {
		if installed {
			return BackupInstallResult{Installed: true, ActionTaken: "already-installed"}, nil
		}
		return BackupInstallResult{}, fmt.Errorf("backup bootstrap requires node_name, git_instance, git_org, and api_token in the config")
	}
	// Remote wins: a repo left over from the previous node installation must
	// never be silently overwritten by an empty local /etc.
	req.ExistingRepoAction = "restore"
	result, err := m.Install(ctx, *req)
	if err == nil {
		m.mu.Lock()
		m.bootstrapDone = true
		m.mu.Unlock()
		log.Printf("bgp backup bootstrap complete via %s", result.ActionTaken)
	}
	return result, err
}

func (m *BackupManager) Install(ctx context.Context, req BackupInstallRequest) (BackupInstallResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := validateBackupInstallRequest(&req); err != nil {
		return BackupInstallResult{}, err
	}

	gitUser, err := m.validateAPICredentials(ctx, req.GitInstance, req.APIToken)
	if err != nil {
		return BackupInstallResult{}, err
	}

	state := BackupState{
		NodeName:     req.NodeName,
		GitInstance:  strings.TrimRight(req.GitInstance, "/"),
		GitOrg:       req.GitOrg,
		RepoName:     req.NodeName,
		GitUser:      gitUser,
		APIToken:     req.APIToken,
		WorkDir:      m.cfg.WorkDir,
		BirdDir:      m.cfg.BirdDir,
		WireGuardDir: m.cfg.WireGuardDir,
		InstalledAt:  m.deps.Now().UTC().Format(time.RFC3339),
	}

	repoExists, err := m.repoExists(ctx, state.GitInstance, state.GitOrg, state.RepoName, state.APIToken)
	if err != nil {
		return BackupInstallResult{}, err
	}

	result := BackupInstallResult{
		Installed:   true,
		NodeName:    state.NodeName,
		RepoName:    state.RepoName,
		RepoURL:     publicRepoURL(&state),
		RepoExisted: repoExists,
	}

	if repoExists {
		switch strings.ToLower(strings.TrimSpace(req.ExistingRepoAction)) {
		case "restore":
			if err := m.restoreExisting(ctx, &state); err != nil {
				return BackupInstallResult{}, err
			}
			result.ActionTaken = "restore"
		case "overwrite":
			if err := m.overwriteExisting(ctx, &state); err != nil {
				return BackupInstallResult{}, err
			}
			result.ActionTaken = "overwrite"
		default:
			return BackupInstallResult{}, fmt.Errorf("existing_repo_action is required when the repository already exists: use restore or overwrite")
		}
	} else {
		if err := m.createRepo(ctx, &state); err != nil {
			return BackupInstallResult{}, err
		}
		if err := m.initialPush(ctx, &state); err != nil {
			return BackupInstallResult{}, err
		}
		result.ActionTaken = "create"
	}

	m.state = &state
	m.enabled = true
	if err := m.writeState(&state); err != nil {
		return BackupInstallResult{}, err
	}
	return result, nil
}

func (m *BackupManager) MigrateLegacy(ctx context.Context, configPath string) (BackupMigrationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	paths := m.existingLegacyPaths()
	result := BackupMigrationResult{
		Detected:    len(paths) > 0,
		LegacyPaths: append([]string{}, paths...),
	}
	if len(paths) == 0 {
		result.Message = "no legacy bgp-backup installation detected"
		return result, nil
	}

	if m.state == nil {
		legacyState, err := m.readLegacyState()
		if err != nil {
			result.Message = err.Error()
			return result, err
		}
		if legacyState == nil {
			result.Message = "legacy installation was detected but its config could not be read"
			return result, fmt.Errorf("%s", result.Message)
		}
		if err := m.writeState(legacyState); err != nil {
			return result, err
		}
		m.state = legacyState
		m.enabled = true
	}

	if err := m.uninstallLegacy(ctx, paths); err != nil {
		result.Message = err.Error()
		return result, err
	}
	m.legacyMigrated = true
	result.Migrated = true
	result.Uninstalled = true

	// One-time write-back: persist the migrated credentials into the agent's
	// own config.yaml so the bootstrap block survives even if backup.yaml is
	// lost. This is the only code path that ever edits the config file.
	state := m.state
	saved, err := SaveBackupBootstrapToConfig(configPath, state.NodeName, state.GitInstance, state.GitOrg, state.APIToken)
	if err != nil {
		// Non-fatal: the migration itself succeeded; the operator can add the
		// bootstrap fields by hand and backup.yaml already has everything.
		log.Printf("WARN: could not record migrated backup credentials in %s: %v", configPath, err)
		result.Message = "migrated legacy bgp-backup configuration and uninstalled the old systemd timer/script (config write-back failed: " + err.Error() + ")"
		return result, nil
	}
	result.ConfigSaved = saved
	result.Message = "migrated legacy bgp-backup configuration into " + configPath + " and uninstalled the old systemd timer/script"
	return result, nil
}

func (m *BackupManager) syncLocked(ctx context.Context) (BackupSyncResult, error) {
	state := m.state
	workDir := state.WorkDir
	birdDir := state.BirdDir
	wireGuardDir := state.WireGuardDir
	result := BackupSyncResult{}

	if err := os.MkdirAll(workDir, 0750); err != nil {
		return result, fmt.Errorf("create backup work dir: %w", err)
	}

	if err := m.ensureGitRepo(ctx, state); err != nil {
		return result, err
	}

	// Fetch first so the merge below sees current remote history.
	_, _ = m.runGit(ctx, workDir, "fetch", "origin", "main")
	local, _ := m.gitRevParse(ctx, workDir, "HEAD")
	remote, _ := m.gitRevParse(ctx, workDir, "origin/main")

	// Bidirectional sync, remote authoritative: merge remote history before
	// committing local samples so human edits always win conflicts.
	var restored backupRestoreStats
	if remote == "" {
		// Empty remote repository (or fresh clone): publish the local samples.
		if err := m.snapshotAndCommit(ctx, state, "自动备份: "); err != nil {
			return result, err
		}
		result.Committed = true
	} else if local != remote {
		result.RemoteMerged = true
		if _, err := m.runGit(ctx, workDir, "merge", "--no-edit", "-X", "theirs", "origin/main"); err != nil {
			// Unresolvable (e.g. unrelated histories): remote wins outright.
			_, _ = m.runGit(ctx, workDir, "merge", "--abort")
			if _, err := m.runGit(ctx, workDir, "reset", "--hard", "origin/main"); err != nil {
				return result, fmt.Errorf("reset to origin/main: %w", err)
			}
		}
		// The merged worktree is now the authoritative config. Apply it to /etc
		// BEFORE re-sampling: otherwise the next sample step would copy the old
		// local files over the merge result and silently discard human edits.
		applied, err := m.restoreDirsFromRepo(ctx, state)
		if err != nil {
			return result, err
		}
		restored = applied
	}

	if err := copyDir(birdDir, filepath.Join(workDir, "bird"), []string{"*.sock"}, nil, nil); err != nil {
		return result, fmt.Errorf("sample bird config: %w", err)
	}
	if err := copyDir(wireGuardDir, filepath.Join(workDir, "wireguard"), nil, nil, nil); err != nil {
		return result, fmt.Errorf("sample wireguard config: %w", err)
	}

	if _, err := m.runGit(ctx, workDir, "add", "-A"); err != nil {
		return result, fmt.Errorf("git add: %w", err)
	}
	changed, err := m.gitStatus(ctx, workDir)
	if err != nil {
		return result, fmt.Errorf("git status: %w", err)
	}
	if !changed {
		result.Message = "no local changes"
	} else {
		if _, err := m.runGit(ctx, workDir, "commit", "-m", "自动备份: "+m.deps.Now().UTC().Format("2006-01-02 15:04:05")); err != nil {
			return result, fmt.Errorf("git commit: %w", err)
		}
		result.Committed = true
	}

	local, _ = m.gitRevParse(ctx, workDir, "HEAD")
	remote, _ = m.gitRevParse(ctx, workDir, "origin/main")
	if remote != "" && local != remote {
		pushed := false
		for i := 0; i < 3; i++ {
			if _, err := m.runGit(ctx, workDir, "push", "origin", "main"); err == nil {
				pushed = true
				break
			}
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(5 * time.Second):
			}
			_, _ = m.runGit(ctx, workDir, "pull", "--rebase", "-X", "ours", "origin", "main")
		}
		if !pushed {
			return result, fmt.Errorf("git push origin main failed after 3 attempts")
		}
		result.Pushed = true
	}

	// Remote is authoritative: after a remote merge the /etc content came from
	// the repo; report what was restored. The node's own sample was taken
	// afterwards, so any genuinely local change is still committed and pushed.
	if restored.birdReloaded || restored.wgRestarted > 0 {
		result.Restored = true
		result.BirdReloaded = restored.birdReloaded
		result.WGRestarted = restored.wgRestarted
	}

	result.Synced = true
	return result, nil
}

func (m *BackupManager) snapshotAndCommit(ctx context.Context, state *BackupState, prefix string) error {
	if err := copyDir(state.BirdDir, filepath.Join(state.WorkDir, "bird"), []string{"*.sock"}, nil, nil); err != nil {
		return fmt.Errorf("sample bird config: %w", err)
	}
	if err := copyDir(state.WireGuardDir, filepath.Join(state.WorkDir, "wireguard"), nil, nil, nil); err != nil {
		return fmt.Errorf("sample wireguard config: %w", err)
	}
	if _, err := m.runGit(ctx, state.WorkDir, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	changed, err := m.gitStatus(ctx, state.WorkDir)
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if changed {
		if _, err := m.runGit(ctx, state.WorkDir, "commit", "-m", prefix+m.deps.Now().UTC().Format("2006-01-02 15:04:05")); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
	}
	return nil
}

func (m *BackupManager) restoreExisting(ctx context.Context, state *BackupState) error {
	if err := m.cloneRepo(ctx, state); err != nil {
		return err
	}
	_, err := m.restoreDirsFromRepo(ctx, state)
	return err
}

func (m *BackupManager) overwriteExisting(ctx context.Context, state *BackupState) error {
	if err := m.cloneRepo(ctx, state); err != nil {
		return err
	}
	if err := copyDir(state.BirdDir, filepath.Join(state.WorkDir, "bird"), []string{"*.sock"}, nil, nil); err != nil {
		return err
	}
	if err := copyDir(state.WireGuardDir, filepath.Join(state.WorkDir, "wireguard"), nil, nil, nil); err != nil {
		return err
	}
	if _, err := m.runGit(ctx, state.WorkDir, "add", "-A"); err != nil {
		return err
	}
	if changed, err := m.gitStatus(ctx, state.WorkDir); err != nil {
		return err
	} else if changed {
		if _, err := m.runGit(ctx, state.WorkDir, "commit", "-m", "覆盖远端: "+state.NodeName+" ("+m.deps.Now().UTC().Format("2006-01-02 15:04:05")+")"); err != nil {
			return err
		}
		if _, err := m.runGit(ctx, state.WorkDir, "push", "origin", "main"); err != nil {
			return err
		}
	}
	return nil
}

func (m *BackupManager) initialPush(ctx context.Context, state *BackupState) error {
	if err := m.initRepo(ctx, state); err != nil {
		return err
	}
	if err := copyDir(state.BirdDir, filepath.Join(state.WorkDir, "bird"), []string{"*.sock"}, nil, nil); err != nil {
		return err
	}
	if err := copyDir(state.WireGuardDir, filepath.Join(state.WorkDir, "wireguard"), nil, nil, nil); err != nil {
		return err
	}
	readme := fmt.Sprintf("# %s — DN42 配置备份\n\n此仓库由 agent-v2 bgp-backup 自动维护。\n\n- **bird/** — %s 配置备份\n- **wireguard/** — %s 配置备份\n\n## 权威规则\n\n- Web UI 中的修改具有**最高优先级**\n- 节点会定期采样本地配置并推送\n- 冲突时自动以远端（人工修改）为准\n", state.NodeName, state.BirdDir, state.WireGuardDir)
	if err := os.WriteFile(filepath.Join(state.WorkDir, "README.md"), []byte(readme), 0644); err != nil {
		return fmt.Errorf("write README: %w", err)
	}
	if _, err := m.runGit(ctx, state.WorkDir, "add", "-A"); err != nil {
		return err
	}
	if _, err := m.runGit(ctx, state.WorkDir, "commit", "-m", "初始化备份: "+state.NodeName+" ("+m.deps.Now().UTC().Format("2006-01-02 15:04:05")+")"); err != nil {
		return err
	}
	if _, err := m.runGit(ctx, state.WorkDir, "branch", "-M", "main"); err != nil {
		return err
	}
	if _, err := m.runGit(ctx, state.WorkDir, "push", "-u", "origin", "main"); err != nil {
		return err
	}
	return nil
}

func (m *BackupManager) ensureGitRepo(ctx context.Context, state *BackupState) error {
	if _, err := os.Stat(filepath.Join(state.WorkDir, ".git")); err == nil {
		return nil
	}
	return m.initRepo(ctx, state)
}

func (m *BackupManager) initRepo(ctx context.Context, state *BackupState) error {
	if err := os.RemoveAll(state.WorkDir); err != nil {
		return fmt.Errorf("reset backup work dir: %w", err)
	}
	if err := os.MkdirAll(state.WorkDir, 0750); err != nil {
		return fmt.Errorf("create backup work dir: %w", err)
	}
	if _, err := m.runGit(ctx, state.WorkDir, "init"); err != nil {
		return fmt.Errorf("git init: %w", err)
	}
	if _, err := m.runGit(ctx, state.WorkDir, "remote", "add", "origin", authRepoURL(state)); err != nil {
		return fmt.Errorf("git remote add: %w", err)
	}
	if err := m.configureGitIdentity(ctx, state); err != nil {
		return err
	}
	return nil
}

func (m *BackupManager) cloneRepo(ctx context.Context, state *BackupState) error {
	if err := os.RemoveAll(state.WorkDir); err != nil {
		return fmt.Errorf("reset backup work dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(state.WorkDir), 0750); err != nil {
		return fmt.Errorf("create backup work dir parent: %w", err)
	}
	if _, err := m.runGit(ctx, filepath.Dir(state.WorkDir), "clone", "--quiet", authRepoURL(state), state.WorkDir); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}
	return m.configureGitIdentity(ctx, state)
}

func (m *BackupManager) configureGitIdentity(ctx context.Context, state *BackupState) error {
	_, err := m.runGit(ctx, state.WorkDir, "config", "user.name", "bgp-backup-"+state.NodeName)
	if err != nil {
		return fmt.Errorf("git config user.name: %w", err)
	}
	_, err = m.runGit(ctx, state.WorkDir, "config", "user.email", "bgp-backup@"+state.NodeName)
	if err != nil {
		return fmt.Errorf("git config user.email: %w", err)
	}
	return nil
}

type backupRestoreStats struct {
	birdReloaded bool
	wgRestarted  int
}

func (m *BackupManager) restoreDirsFromRepo(ctx context.Context, state *BackupState) (backupRestoreStats, error) {
	var stats backupRestoreStats

	birdRepoDir := filepath.Join(state.WorkDir, "bird")
	if hasEntries(birdRepoDir) {
		var dirMode os.FileMode = 0755
		var fileMode os.FileMode = 0644
		if err := copyDir(birdRepoDir, state.BirdDir, nil, &dirMode, &fileMode); err != nil {
			return stats, fmt.Errorf("restore bird config: %w", err)
		}
		m.reloadBird(ctx)
		stats.birdReloaded = true
	}

	wgRepoDir := filepath.Join(state.WorkDir, "wireguard")
	if hasEntries(wgRepoDir) {
		var dirMode os.FileMode = 0700
		var fileMode os.FileMode = 0600
		before := wgUpInterfaces(ctx, m.runCmd)
		if err := copyDir(wgRepoDir, state.WireGuardDir, nil, &dirMode, &fileMode); err != nil {
			return stats, fmt.Errorf("restore wireguard config: %w", err)
		}
		after := listWGConfNames(state.WireGuardDir)
		stats.wgRestarted = m.bringUpRestoredWG(ctx, before, after)
	}
	return stats, nil
}

// bringUpRestoredWG starts every restored dn42-* interface that is not
// currently up. Interfaces that were already running are left alone: their
// in-kernel state is live and the next agent restart/recovery pass covers
// config drift, so a bounce would only drop sessions needlessly.
func (m *BackupManager) bringUpRestoredWG(ctx context.Context, before map[string]bool, restored map[string]bool) int {
	restarted := 0
	names := make([]string, 0, len(restored))
	for name := range restored {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, iface := range names {
		select {
		case <-ctx.Done():
			return restarted
		default:
		}
		if before[iface] {
			continue
		}
		if _, err := m.runCmd(ctx, "wg-quick", []string{"up", iface}, 15*time.Second); err != nil {
			log.Printf("WARN: wg-quick up %s failed: %v", iface, err)
			continue
		}
		restarted++
	}
	return restarted
}

func wgUpInterfaces(ctx context.Context, runCmd func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error)) map[string]bool {
	out, _ := runCmd(ctx, "wg", []string{"show", "interfaces"}, 10*time.Second)
	result := map[string]bool{}
	for _, iface := range strings.Fields(out) {
		result[strings.TrimSpace(iface)] = true
	}
	return result
}

func listWGConfNames(wireGuardDir string) map[string]bool {
	result := map[string]bool{}
	entries, err := os.ReadDir(wireGuardDir)
	if err != nil {
		return result
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "dn42-") && strings.HasSuffix(name, ".conf") {
			result[name[:len(name)-5]] = true
		}
	}
	return result
}

func (m *BackupManager) reloadBird(ctx context.Context) {
	if _, err := birdctl.Query(ctx, m.deps.BirdCtlPath, "configure"); err != nil {
		log.Printf("WARN: BIRD reload failed: %v", err)
	}
}

func (m *BackupManager) gitStatus(ctx context.Context, workDir string) (bool, error) {
	out, err := m.runGit(ctx, workDir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (m *BackupManager) gitRevParse(ctx context.Context, workDir, ref string) (string, error) {
	out, err := m.runGit(ctx, workDir, "rev-parse", ref)
	return strings.TrimSpace(out), err
}

func (m *BackupManager) runGit(ctx context.Context, workDir string, args ...string) (string, error) {
	return m.runCmd(ctx, "git", append([]string{"-C", workDir}, args...), 2*time.Minute)
}

func (m *BackupManager) runCmd(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
	return m.deps.RunCommand(ctx, name, args, timeout)
}

func (m *BackupManager) validateAPICredentials(ctx context.Context, gitInstance, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gitInstance, "/")+"/api/v1/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := m.deps.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("Git API authentication failed (HTTP %s): %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", err
	}
	if strings.TrimSpace(user.Login) == "" {
		return "", fmt.Errorf("Git API user has no login")
	}
	return strings.TrimSpace(user.Login), nil
}

func (m *BackupManager) repoExists(ctx context.Context, gitInstance, org, repo, token string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gitInstance, "/")+"/api/v1/repos/"+org+"/"+repo, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := m.deps.HTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 200:
		return true, nil
	case resp.StatusCode == 404:
		return false, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return false, fmt.Errorf("check repository failed (HTTP %s): %s", resp.Status, strings.TrimSpace(string(body)))
	}
}

func (m *BackupManager) createRepo(ctx context.Context, state *BackupState) error {
	payload := map[string]interface{}{
		"name":        state.RepoName,
		"description": "DN42 config backup for node " + state.NodeName,
		"private":     true,
		"auto_init":   false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(state.GitInstance, "/")+"/api/v1/orgs/"+state.GitOrg+"/repos", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+state.APIToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := m.deps.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	var created struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &created)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if strings.TrimSpace(created.Message) == "" {
			created.Message = "unknown error"
		}
		return fmt.Errorf("create repository failed (HTTP %s): %s", resp.Status, created.Message)
	}
	if strings.TrimSpace(created.Name) != "" {
		return nil
	}

	orgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(state.GitInstance, "/")+"/api/v1/orgs/"+state.GitOrg, nil)
	if err != nil {
		return err
	}
	orgReq.Header.Set("Authorization", "token "+state.APIToken)
	orgReq.Header.Set("Accept", "application/json")
	orgResp, err := m.deps.HTTPClient.Do(orgReq)
	if err != nil {
		return err
	}
	defer orgResp.Body.Close()
	if orgResp.StatusCode != 200 {
		return fmt.Errorf("organization %s does not exist", state.GitOrg)
	}
	if strings.TrimSpace(created.Message) == "" {
		created.Message = "unknown error"
	}
	return fmt.Errorf("create repository failed: %s", created.Message)
}

func (m *BackupManager) readState() (*BackupState, error) {
	data, err := os.ReadFile(m.cfg.StateFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read backup state file: %w", err)
	}
	var state BackupState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse backup state file: %w", err)
	}
	return &state, nil
}

func (m *BackupManager) writeState(state *BackupState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(m.cfg.StateFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create backup state dir: %w", err)
	}
	if err := os.WriteFile(m.cfg.StateFile, data, 0600); err != nil {
		return fmt.Errorf("write backup state file: %w", err)
	}
	return nil
}

func (m *BackupManager) readLegacyState() (*BackupState, error) {
	data, err := os.ReadFile(m.deps.LegacyConfigPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy backup config: %w", err)
	}
	return parseLegacyConfig(data, m.cfg)
}

func (m *BackupManager) existingLegacyPaths() []string {
	var paths []string
	for _, path := range m.deps.LegacyPaths {
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
}

func (m *BackupManager) uninstallLegacy(ctx context.Context, paths []string) error {
	_, _ = m.runCmd(ctx, "systemctl", []string{"stop", "bgp-backup.timer", "bgp-backup.service"}, 30*time.Second)
	_, _ = m.runCmd(ctx, "systemctl", []string{"disable", "bgp-backup.timer", "bgp-backup.service"}, 30*time.Second)

	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove legacy file %s: %w", path, err)
		}
	}
	_, _ = m.runCmd(ctx, "systemctl", []string{"daemon-reload"}, 30*time.Second)
	return nil
}

func validateBackupInstallRequest(req *BackupInstallRequest) error {
	req.NodeName = normalizeNodeName(req.NodeName)
	req.GitInstance = strings.TrimRight(strings.TrimSpace(req.GitInstance), "/")
	req.GitOrg = strings.TrimSpace(req.GitOrg)
	req.APIToken = strings.TrimSpace(req.APIToken)
	if req.NodeName == "" {
		return fmt.Errorf("node_name is required")
	}
	if req.GitInstance == "" {
		return fmt.Errorf("git_instance is required")
	}
	if req.GitOrg == "" {
		return fmt.Errorf("git_org is required")
	}
	if req.APIToken == "" {
		return fmt.Errorf("api_token is required")
	}
	u, err := url.Parse(req.GitInstance)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("git_instance must be an http(s) URL")
	}
	return nil
}

func normalizeNodeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('-')
		}
	}
	return builder.String()
}

func parseLegacyConfig(data []byte, cfg config.BackupConfig) (*BackupState, error) {
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(line[:eq]))
		value := strings.TrimSpace(line[eq+1:])
		if len(value) >= 2 && strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			if unquoted, err := strconv.Unquote(value); err == nil {
				value = unquoted
			}
		}
		values[key] = value
	}

	state := &BackupState{
		NodeName:     values["NODE_NAME"],
		WorkDir:      values["WORK_DIR"],
		BirdDir:      values["BIRD_DIR"],
		WireGuardDir: values["WG_DIR"],
		GitInstance:  values["GIT_INSTANCE"],
		GitOrg:       values["GIT_ORG"],
		RepoName:     values["REPO_NAME"],
		InstalledAt:  values["INSTALL_DATE"],
	}
	if state.WorkDir == "" {
		state.WorkDir = cfg.WorkDir
	}
	if state.BirdDir == "" {
		state.BirdDir = cfg.BirdDir
	}
	if state.WireGuardDir == "" {
		state.WireGuardDir = cfg.WireGuardDir
	}
	if state.RepoName == "" {
		state.RepoName = state.NodeName
	}

	if repoURL := values["REPO_URL"]; repoURL != "" {
		u, err := url.Parse(repoURL)
		if err == nil {
			if u.User != nil {
				state.GitUser = u.User.Username()
				if token, ok := u.User.Password(); ok {
					state.APIToken = token
				}
			}
			if state.GitInstance == "" {
				state.GitInstance = u.Scheme + "://" + u.Host
			}
			if state.GitOrg == "" {
				parts := strings.Split(strings.Trim(u.Path, "/"), "/")
				if len(parts) >= 2 {
					state.GitOrg = parts[0]
					state.RepoName = strings.TrimSuffix(parts[1], ".git")
				}
			}
		}
	}

	if state.NodeName == "" || state.GitInstance == "" || state.GitOrg == "" || state.RepoName == "" || state.GitUser == "" || state.APIToken == "" {
		return nil, fmt.Errorf("legacy bgp-backup config is incomplete")
	}
	return state, nil
}

func authRepoURL(state *BackupState) string {
	u, err := url.Parse(state.GitInstance)
	if err != nil {
		return ""
	}
	u.User = url.UserPassword(state.GitUser, state.APIToken)
	u.Path = "/" + state.GitOrg + "/" + state.RepoName + ".git"
	return u.String()
}

func publicRepoURL(state *BackupState) string {
	u, err := url.Parse(state.GitInstance)
	if err != nil {
		return state.GitInstance + "/" + state.GitOrg + "/" + state.RepoName
	}
	u.User = nil
	u.Path = "/" + state.GitOrg + "/" + state.RepoName
	return u.String()
}

func hasEntries(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return false
	}
	return true
}

func copyDir(src, dst string, excludePatterns []string, dirModeOverride, fileModeOverride *os.FileMode) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return os.RemoveAll(dst)
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	wanted := map[string]bool{".": true}

	err = filepath.WalkDir(srcAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if excludedByPatterns(entry.Name(), excludePatterns) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relSlash := filepath.ToSlash(rel)
		wanted[relSlash] = true
		target := filepath.Join(dst, rel)

		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(link, target)
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if entry.IsDir() {
			mode := os.FileMode(0755)
			if dirModeOverride != nil {
				mode = *dirModeOverride
			}
			if err := os.MkdirAll(target, mode); err != nil {
				return err
			}
			return os.Chmod(target, mode)
		}

		mode := info.Mode().Perm()
		if fileModeOverride != nil {
			mode = *fileModeOverride
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return err
		}
		return os.Chmod(target, mode)
	})
	if err != nil {
		return err
	}

	return removeExtraneous(dst, wanted)
}

func excludedByPatterns(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if ok, err := filepath.Match(pattern, name); err == nil && ok {
			return true
		}
	}
	return false
}

func removeExtraneous(dst string, wanted map[string]bool) error {
	return filepath.WalkDir(dst, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(dst, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if wanted[filepath.ToSlash(rel)] {
			return nil
		}
		if entry.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			return filepath.SkipDir
		}
		return os.Remove(path)
	})
}
