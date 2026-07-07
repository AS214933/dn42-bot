package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

var (
	BuildVersion = "dev"
	BuildCommit  = "unknown"
)

type UpdateStatus struct {
	CurrentVersion  string `json:"current_version"`
	BuildCommit     string `json:"build_commit"`
	Channel         string `json:"channel"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	Installed       bool   `json:"installed"`
	RestartRequired bool   `json:"restart_required"`
	Prerelease      bool   `json:"prerelease"`
	ReleaseURL      string `json:"release_url,omitempty"`
	AssetName       string `json:"asset_name,omitempty"`
}

type ReleaseUpdaterDeps struct {
	HTTPClient     *http.Client
	APIBaseURL     string
	RunCommand     func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error)
	CurrentVersion string
	BuildCommit    string
	GOOS           string
	GOARCH         string
}

type ReleaseUpdater struct {
	cfg            config.AutoUpdateConfig
	httpClient     *http.Client
	apiBaseURL     string
	runCmd         func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error)
	currentVersion string
	buildCommit    string
	goos           string
	goarch         string
	mu             sync.Mutex
}

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	HTMLURL    string        `json:"html_url"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func NewReleaseUpdater(cfg config.AutoUpdateConfig, deps ReleaseUpdaterDeps) *ReleaseUpdater {
	client := deps.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	apiBaseURL := strings.TrimRight(deps.APIBaseURL, "/")
	if apiBaseURL == "" {
		apiBaseURL = "https://api.github.com"
	}
	runCmd := deps.RunCommand
	if runCmd == nil {
		runCmd = RunCommand
	}
	currentVersion := deps.CurrentVersion
	if currentVersion == "" {
		currentVersion = BuildVersion
	}
	buildCommit := deps.BuildCommit
	if buildCommit == "" {
		buildCommit = BuildCommit
	}
	goos := deps.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := deps.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	return &ReleaseUpdater{
		cfg:            cfg,
		httpClient:     client,
		apiBaseURL:     apiBaseURL,
		runCmd:         runCmd,
		currentVersion: currentVersion,
		buildCommit:    buildCommit,
		goos:           goos,
		goarch:         goarch,
	}
}

func (u *ReleaseUpdater) Check(ctx context.Context, channelOverride string) (*UpdateStatus, error) {
	channel, err := updateChannel(channelOverride, u.cfg.Channel)
	if err != nil {
		return nil, err
	}

	release, asset, err := u.latestRelease(ctx, channel)
	if err != nil {
		return nil, err
	}

	return &UpdateStatus{
		CurrentVersion:  u.currentVersion,
		BuildCommit:     u.buildCommit,
		Channel:         channel,
		LatestVersion:   release.TagName,
		UpdateAvailable: updateAvailable(u.currentVersion, release.TagName),
		Prerelease:      release.Prerelease,
		ReleaseURL:      release.HTMLURL,
		AssetName:       asset.Name,
	}, nil
}

func (u *ReleaseUpdater) Install(ctx context.Context, channelOverride string, force bool) (*UpdateStatus, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	status, err := u.Check(ctx, channelOverride)
	if err != nil {
		return nil, err
	}
	if !status.UpdateAvailable && !force {
		return status, nil
	}
	if err := u.ensureServiceReady(); err != nil {
		return nil, err
	}

	release, asset, err := u.latestRelease(ctx, status.Channel)
	if err != nil {
		return nil, err
	}
	if err := u.installAsset(ctx, asset); err != nil {
		return nil, err
	}

	status.LatestVersion = release.TagName
	status.Installed = true
	status.RestartRequired = true
	status.UpdateAvailable = true
	status.Prerelease = release.Prerelease
	status.ReleaseURL = release.HTMLURL
	status.AssetName = asset.Name
	return status, nil
}

func (u *ReleaseUpdater) Restart(ctx context.Context) error {
	if err := u.ensureServiceReady(); err != nil {
		return err
	}

	if output, err := u.runCmd(ctx, "systemctl", []string{"daemon-reload"}, 30*time.Second); err != nil {
		return fmt.Errorf("systemctl daemon-reload failed: %w: %s", err, strings.TrimSpace(output))
	}
	if output, err := u.runCmd(ctx, "systemctl", []string{"restart", u.cfg.ServiceName}, 30*time.Second); err != nil {
		return fmt.Errorf("systemctl restart %s failed: %w: %s", u.cfg.ServiceName, err, strings.TrimSpace(output))
	}
	return nil
}

func (u *ReleaseUpdater) ensureServiceReady() error {
	if _, err := os.Stat(u.cfg.ServicePath); err != nil {
		return fmt.Errorf("agent service file %s is not usable: %w", u.cfg.ServicePath, err)
	}
	return nil
}

func (u *ReleaseUpdater) Run(ctx context.Context) {
	if !u.cfg.Enabled {
		return
	}

	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			status, err := u.Install(ctx, "", false)
			if err != nil {
				log.Printf("auto update check failed: %v", err)
			} else if status.Installed {
				log.Printf("installed agent update %s from %s; restarting %s", status.LatestVersion, status.Channel, u.cfg.ServiceName)
				if err := u.Restart(ctx); err != nil {
					log.Printf("auto update restart failed: %v", err)
				}
			}
			timer.Reset(u.cfg.CheckInterval)
		}
	}
}

func (u *ReleaseUpdater) latestRelease(ctx context.Context, channel string) (*githubRelease, *githubAsset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.apiBaseURL+"/repos/"+u.cfg.Repository+"/releases?per_page=30", nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "dn42-agent-v2-updater")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, nil, fmt.Errorf("GitHub releases API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, nil, err
	}

	wantAsset := u.assetName()
	for i := range releases {
		release := &releases[i]
		if release.Draft {
			continue
		}
		if channel == "stable" && release.Prerelease {
			continue
		}
		asset := findReleaseAsset(release, wantAsset)
		if asset == nil {
			continue
		}
		return release, asset, nil
	}

	return nil, nil, fmt.Errorf("no %s release with asset %s found", channel, wantAsset)
}

func (u *ReleaseUpdater) installAsset(ctx context.Context, asset *githubAsset) error {
	tmpDir, err := os.MkdirTemp(u.cfg.DataDir, ".agent-update-*")
	if err != nil {
		return fmt.Errorf("create update temp dir under %s: %w", u.cfg.DataDir, err)
	}
	defer os.RemoveAll(tmpDir)

	tmpPath := filepath.Join(tmpDir, filepath.Base(u.cfg.AgentPath)+".new")
	if err := downloadFile(ctx, u.httpClient, asset.BrowserDownloadURL, tmpPath); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod downloaded agent: %w", err)
	}
	if err := os.Rename(tmpPath, u.cfg.AgentPath); err != nil {
		return fmt.Errorf("replace agent binary %s: %w", u.cfg.AgentPath, err)
	}
	return nil
}

func (u *ReleaseUpdater) assetName() string {
	return "agent-v2-" + u.goos + "-" + u.goarch
}

func findReleaseAsset(release *githubRelease, name string) *githubAsset {
	for i := range release.Assets {
		if release.Assets[i].Name == name {
			return &release.Assets[i]
		}
	}
	return nil
}

func downloadFile(ctx context.Context, client *http.Client, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "dn42-agent-v2-updater")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s returned %s", url, resp.Status)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return fmt.Errorf("create downloaded agent: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write downloaded agent: %w", err)
	}
	return nil
}

func updateChannel(override, configured string) (string, error) {
	channel := configured
	if strings.TrimSpace(override) != "" {
		channel = override
	}
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "", "candidate", "candidates", "prerelease", "pre", "preview", "alpha", "beta", "rc":
		return "candidate", nil
	case "stable", "release", "releases":
		return "stable", nil
	default:
		return "", fmt.Errorf("invalid update channel %q: expected stable or candidate", channel)
	}
}

func updateAvailable(current, latest string) bool {
	current = strings.TrimSpace(current)
	latest = strings.TrimSpace(latest)
	if latest == "" {
		return false
	}
	if current == "" || current == "dev" || current == "unknown" {
		return true
	}
	if current == latest {
		return false
	}

	c, okC := parseSemver(current)
	l, okL := parseSemver(latest)
	if !okC || !okL {
		return current != latest
	}
	return c.compare(l) < 0
}

type semver struct {
	major int
	minor int
	patch int
	pre   string
}

func parseSemver(tag string) (semver, bool) {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "v")
	version, pre, _ := strings.Cut(tag, "-")
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semver{}, false
	}
	return semver{major: major, minor: minor, patch: patch, pre: pre}, true
}

func (v semver) compare(other semver) int {
	if v.major != other.major {
		return compareInt(v.major, other.major)
	}
	if v.minor != other.minor {
		return compareInt(v.minor, other.minor)
	}
	if v.patch != other.patch {
		return compareInt(v.patch, other.patch)
	}
	if v.pre == other.pre {
		return 0
	}
	if v.pre == "" {
		return 1
	}
	if other.pre == "" {
		return -1
	}
	return comparePrerelease(v.pre, other.pre)
}

func compareInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func comparePrerelease(a, b string) int {
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	for i := 0; i < len(ap) || i < len(bp); i++ {
		if i >= len(ap) {
			return -1
		}
		if i >= len(bp) {
			return 1
		}
		ai, aerr := strconv.Atoi(ap[i])
		bi, berr := strconv.Atoi(bp[i])
		switch {
		case aerr == nil && berr == nil:
			if ai != bi {
				return compareInt(ai, bi)
			}
		case aerr == nil:
			return -1
		case berr == nil:
			return 1
		case ap[i] != bp[i]:
			if ap[i] < bp[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
