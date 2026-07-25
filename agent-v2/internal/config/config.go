package config

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/lookingglass"
	"gopkg.in/yaml.v3"
)

type NetSupport struct {
	IPv4    bool `yaml:"ipv4"`
	IPv6    bool `yaml:"ipv6"`
	IPv4NAT bool `yaml:"ipv4_nat"`
	CN      bool `yaml:"cn"`
}

type AutoUpdateConfig struct {
	Enabled       bool          `yaml:"enabled"`
	Channel       string        `yaml:"channel"`
	CheckInterval time.Duration `yaml:"check_interval"`
	Repository    string        `yaml:"repository"`
	DataDir       string        `yaml:"data_dir"`
	AgentPath     string        `yaml:"agent_path"`
	ServiceName   string        `yaml:"service_name"`
	ServicePath   string        `yaml:"service_path"`
}

type LookingGlassConfig struct {
	Enabled                 bool          `yaml:"enabled" json:"enabled"`
	AllowedCIDRs            []string      `yaml:"allowed_cidrs" json:"allowed_cidrs"`
	DisallowedCIDRs         []string      `yaml:"disallowed_cidrs" json:"disallowed_cidrs"`
	TracerouteEnabled       bool          `yaml:"traceroute_enabled" json:"traceroute_enabled"`
	BirdMaxConcurrent       int           `yaml:"bird_max_concurrent" json:"bird_max_concurrent"`
	TracerouteMaxConcurrent int           `yaml:"traceroute_max_concurrent" json:"traceroute_max_concurrent"`
	RequestTimeout          time.Duration `yaml:"request_timeout" json:"-"`
	MaxQueryLength          int           `yaml:"max_query_length" json:"max_query_length"`
	MaxOutputBytes          int           `yaml:"max_output_bytes" json:"max_output_bytes"`
}

type PeerFinderConfig struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	Host          string `yaml:"host" json:"host"`
	Port          int    `yaml:"port" json:"port"`
	HMACKey       []byte `yaml:"-" json:"-"`
	SecretKeyFile string `yaml:"secret_key_file" json:"-"`
}

type Config struct {
	Host                   string             `yaml:"host"`
	Port                   int                `yaml:"port"`
	Secret                 string             `yaml:"secret"`
	Open                   bool               `yaml:"open"`
	MaxPeers               int                `yaml:"max_peers"`
	MinPeerRequirement     int                `yaml:"min_peer_requirement"`
	NetSupport             NetSupport         `yaml:"net_support"`
	ExtraMsg               string             `yaml:"extra_msg"`
	MyDN42LinkLocalAddress net.IP             `yaml:"my_dn42_link_local_address"`
	MyDN42ULAAddress       net.IP             `yaml:"my_dn42_ula_address"`
	MyDN42IPv4Address      net.IP             `yaml:"my_dn42_ipv4_address"`
	MyWGPublicKey          string             `yaml:"my_wg_public_key"`
	SentryDSN              string             `yaml:"sentry_dsn"`
	BirdCtlPath            string             `yaml:"bird_ctl_path"`
	BirdTable4             string             `yaml:"bird_table_4"`
	BirdTable6             string             `yaml:"bird_table_6"`
	VnstatAutoAdd          bool               `yaml:"vnstat_auto_add"`
	VnstatAutoRemove       bool               `yaml:"vnstat_auto_remove"`
	DefaultMTU             int                `yaml:"default_mtu"`
	ServerURL              string             `yaml:"server_url"`
	DNSServers             []string           `yaml:"dns_servers"`
	AutoUpdate             AutoUpdateConfig   `yaml:"auto_update"`
	LookingGlass           LookingGlassConfig `yaml:"looking_glass"`
	PeerFinder             PeerFinderConfig   `yaml:"peerfinder"`
}

type rawConfig struct {
	Host                   string                `yaml:"host"`
	Port                   *int                  `yaml:"port"`
	Secret                 string                `yaml:"secret"`
	Open                   bool                  `yaml:"open"`
	MaxPeers               *int                  `yaml:"max_peers"`
	MinPeerRequirement     *int                  `yaml:"min_peer_requirement"`
	NetSupport             NetSupport            `yaml:"net_support"`
	ExtraMsg               *string               `yaml:"extra_msg"`
	MyDN42LinkLocalAddress string                `yaml:"my_dn42_link_local_address"`
	MyDN42ULAAddress       string                `yaml:"my_dn42_ula_address"`
	MyDN42IPv4Address      string                `yaml:"my_dn42_ipv4_address"`
	MyWGPublicKey          string                `yaml:"my_wg_public_key"`
	SentryDSN              *string               `yaml:"sentry_dsn"`
	BirdCtlPath            *string               `yaml:"bird_ctl_path"`
	BirdTable4             string                `yaml:"bird_table_4"`
	BirdTable6             string                `yaml:"bird_table_6"`
	VnstatAutoAdd          bool                  `yaml:"vnstat_auto_add"`
	VnstatAutoRemove       *bool                 `yaml:"vnstat_auto_remove"`
	DefaultMTU             *int                  `yaml:"default_mtu"`
	ServerURL              *string               `yaml:"server_url"`
	DNSServers             []string              `yaml:"dns_servers"`
	AutoUpdate             rawAutoUpdateConfig   `yaml:"auto_update"`
	LookingGlass           rawLookingGlassConfig `yaml:"looking_glass"`
	PeerFinder             rawPeerFinderConfig   `yaml:"peerfinder"`
}

type rawAutoUpdateConfig struct {
	Enabled       bool    `yaml:"enabled"`
	Channel       *string `yaml:"channel"`
	CheckInterval *string `yaml:"check_interval"`
	Repository    *string `yaml:"repository"`
	DataDir       *string `yaml:"data_dir"`
	AgentPath     *string `yaml:"agent_path"`
	ServiceName   *string `yaml:"service_name"`
	ServicePath   *string `yaml:"service_path"`
}

type rawLookingGlassConfig struct {
	Enabled                 bool     `yaml:"enabled"`
	AllowedCIDRs            []string `yaml:"allowed_cidrs"`
	DisallowedCIDRs         []string `yaml:"disallowed_cidrs"`
	TracerouteEnabled       *bool    `yaml:"traceroute_enabled"`
	BirdMaxConcurrent       *int     `yaml:"bird_max_concurrent"`
	TracerouteMaxConcurrent *int     `yaml:"traceroute_max_concurrent"`
	RequestTimeout          *string  `yaml:"request_timeout"`
	MaxQueryLength          *int     `yaml:"max_query_length"`
	MaxOutputBytes          *int     `yaml:"max_output_bytes"`
}

type rawPeerFinderConfig struct {
	Enabled       bool    `yaml:"enabled"`
	Host          *string `yaml:"host"`
	Port          *int    `yaml:"port"`
	SecretKey     *string `yaml:"secret_key"`
	SecretKeyFile *string `yaml:"secret_key_file"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if raw.Host == "" {
		raw.Host = "0.0.0.0"
	}

	port := 54321
	if raw.Port != nil {
		port = *raw.Port
	}

	maxPeers := 0
	if raw.MaxPeers != nil {
		if *raw.MaxPeers > 0 {
			maxPeers = *raw.MaxPeers
		}
	}

	minPeerReq := 0
	if raw.MinPeerRequirement != nil {
		if *raw.MinPeerRequirement > 0 {
			minPeerReq = *raw.MinPeerRequirement
		}
	}

	extraMsg := ""
	if raw.ExtraMsg != nil {
		extraMsg = *raw.ExtraMsg
	}

	sentryDSN := ""
	if raw.SentryDSN != nil {
		sentryDSN = *raw.SentryDSN
	}

	birdCtlPath := "/var/run/bird/bird.ctl"
	if raw.BirdCtlPath != nil && *raw.BirdCtlPath != "" {
		birdCtlPath = *raw.BirdCtlPath
	}

	defaultMTU := 1420
	if raw.DefaultMTU != nil {
		defaultMTU = *raw.DefaultMTU
	}

	serverURL := ""
	if raw.ServerURL != nil {
		serverURL = *raw.ServerURL
	}

	dnsServers, err := normalizeDNSServers(raw.DNSServers)
	if err != nil {
		return nil, err
	}

	autoUpdate, err := normalizeAutoUpdate(raw.AutoUpdate)
	if err != nil {
		return nil, err
	}

	lookingGlass, err := normalizeLookingGlass(raw.LookingGlass)
	if err != nil {
		return nil, err
	}

	peerFinder, err := normalizePeerFinder(raw.PeerFinder, port, filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	linkLocalAddr := net.ParseIP(raw.MyDN42LinkLocalAddress)
	if linkLocalAddr == nil {
		return nil, fmt.Errorf("invalid my_dn42_link_local_address: %q", raw.MyDN42LinkLocalAddress)
	}

	ulaAddr := net.ParseIP(raw.MyDN42ULAAddress)
	if ulaAddr == nil {
		return nil, fmt.Errorf("invalid my_dn42_ula_address: %q", raw.MyDN42ULAAddress)
	}

	ipv4Addr := net.ParseIP(raw.MyDN42IPv4Address)
	if ipv4Addr == nil {
		return nil, fmt.Errorf("invalid my_dn42_ipv4_address: %q", raw.MyDN42IPv4Address)
	}

	vnstatAutoRemove := false
	if raw.VnstatAutoRemove != nil {
		vnstatAutoRemove = *raw.VnstatAutoRemove
	}
	if !raw.VnstatAutoAdd {
		vnstatAutoRemove = false
	}

	cfg := &Config{
		Host:                   raw.Host,
		Port:                   port,
		Secret:                 raw.Secret,
		Open:                   raw.Open,
		MaxPeers:               maxPeers,
		MinPeerRequirement:     minPeerReq,
		NetSupport:             raw.NetSupport,
		ExtraMsg:               extraMsg,
		MyDN42LinkLocalAddress: linkLocalAddr,
		MyDN42ULAAddress:       ulaAddr,
		MyDN42IPv4Address:      ipv4Addr,
		MyWGPublicKey:          raw.MyWGPublicKey,
		SentryDSN:              sentryDSN,
		BirdCtlPath:            birdCtlPath,
		BirdTable4:             raw.BirdTable4,
		BirdTable6:             raw.BirdTable6,
		VnstatAutoAdd:          raw.VnstatAutoAdd,
		VnstatAutoRemove:       vnstatAutoRemove,
		DefaultMTU:             defaultMTU,
		ServerURL:              serverURL,
		DNSServers:             dnsServers,
		AutoUpdate:             autoUpdate,
		LookingGlass:           lookingGlass,
		PeerFinder:             peerFinder,
	}

	return cfg, nil
}

func normalizePeerFinder(raw rawPeerFinderConfig, apiPort int, configDir string) (PeerFinderConfig, error) {
	cfg := PeerFinderConfig{
		Enabled: raw.Enabled,
		Host:    "::",
		Port:    9000,
	}
	if raw.Host != nil && strings.TrimSpace(*raw.Host) != "" {
		cfg.Host = strings.TrimSpace(*raw.Host)
	}
	if raw.Port != nil {
		if *raw.Port <= 0 || *raw.Port > 65535 {
			return PeerFinderConfig{}, fmt.Errorf("invalid peerfinder.port %d: expected 1-65535", *raw.Port)
		}
		cfg.Port = *raw.Port
	}
	if !cfg.Enabled {
		return cfg, nil
	}
	if cfg.Port == apiPort {
		return PeerFinderConfig{}, fmt.Errorf("invalid peerfinder.port %d: must not reuse the agent API port", cfg.Port)
	}

	key, keyFile, err := loadPeerFinderKey(raw, configDir)
	if err != nil {
		return PeerFinderConfig{}, err
	}
	cfg.HMACKey = key
	cfg.SecretKeyFile = keyFile
	return cfg, nil
}

func loadPeerFinderKey(raw rawPeerFinderConfig, configDir string) ([]byte, string, error) {
	keySource := ""
	keyFile := ""
	if raw.SecretKeyFile != nil && strings.TrimSpace(*raw.SecretKeyFile) != "" {
		keyFile = strings.TrimSpace(*raw.SecretKeyFile)
		readPath := keyFile
		if !filepath.IsAbs(readPath) {
			readPath = filepath.Join(configDir, readPath)
		}
		data, err := os.ReadFile(readPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read peerfinder.secret_key_file: %w", err)
		}
		keySource = string(data)
	} else if raw.SecretKey != nil {
		keySource = *raw.SecretKey
	}

	keySource = strings.TrimSpace(keySource)
	if keySource == "" {
		return nil, "", fmt.Errorf("peerfinder.secret_key or peerfinder.secret_key_file is required when peerfinder is enabled")
	}
	key, err := hex.DecodeString(keySource)
	if err != nil || len(key) != 32 {
		return nil, "", fmt.Errorf("invalid peerfinder secret key: expected 32-byte hex string")
	}
	return key, keyFile, nil
}

func normalizeLookingGlass(raw rawLookingGlassConfig) (LookingGlassConfig, error) {
	cfg := LookingGlassConfig{
		Enabled:                 raw.Enabled,
		AllowedCIDRs:            append([]string{}, raw.AllowedCIDRs...),
		DisallowedCIDRs:         append([]string{}, raw.DisallowedCIDRs...),
		TracerouteEnabled:       true,
		BirdMaxConcurrent:       16,
		TracerouteMaxConcurrent: 10,
		RequestTimeout:          15 * time.Second,
		MaxQueryLength:          4096,
		MaxOutputBytes:          64 * 1024,
	}
	if raw.TracerouteEnabled != nil {
		cfg.TracerouteEnabled = *raw.TracerouteEnabled
	}

	if err := lookingglass.ValidateSourceSelectors(cfg.AllowedCIDRs, cfg.DisallowedCIDRs); err != nil {
		return LookingGlassConfig{}, fmt.Errorf("invalid looking_glass source policy: %w", err)
	}
	if raw.BirdMaxConcurrent != nil {
		if *raw.BirdMaxConcurrent <= 0 || *raw.BirdMaxConcurrent > 256 {
			return LookingGlassConfig{}, fmt.Errorf("invalid looking_glass.bird_max_concurrent %d: expected 1-256", *raw.BirdMaxConcurrent)
		}
		cfg.BirdMaxConcurrent = *raw.BirdMaxConcurrent
	}
	if raw.TracerouteMaxConcurrent != nil {
		if *raw.TracerouteMaxConcurrent <= 0 || *raw.TracerouteMaxConcurrent > 64 {
			return LookingGlassConfig{}, fmt.Errorf("invalid looking_glass.traceroute_max_concurrent %d: expected 1-64", *raw.TracerouteMaxConcurrent)
		}
		cfg.TracerouteMaxConcurrent = *raw.TracerouteMaxConcurrent
	}
	if raw.RequestTimeout != nil {
		timeout, err := time.ParseDuration(strings.TrimSpace(*raw.RequestTimeout))
		if err != nil || timeout <= 0 || timeout > 2*time.Minute {
			return LookingGlassConfig{}, fmt.Errorf("invalid looking_glass.request_timeout %q: expected a duration up to 2m", *raw.RequestTimeout)
		}
		cfg.RequestTimeout = timeout
	}
	if raw.MaxQueryLength != nil {
		if *raw.MaxQueryLength <= 0 || *raw.MaxQueryLength > 4096 {
			return LookingGlassConfig{}, fmt.Errorf("invalid looking_glass.max_query_length %d: expected 1-4096", *raw.MaxQueryLength)
		}
		cfg.MaxQueryLength = *raw.MaxQueryLength
	}
	if raw.MaxOutputBytes != nil {
		if *raw.MaxOutputBytes <= 0 || *raw.MaxOutputBytes > 64*1024 {
			return LookingGlassConfig{}, fmt.Errorf("invalid looking_glass.max_output_bytes %d: expected 1-65536", *raw.MaxOutputBytes)
		}
		cfg.MaxOutputBytes = *raw.MaxOutputBytes
	}

	return cfg, nil
}

func normalizeAutoUpdate(raw rawAutoUpdateConfig) (AutoUpdateConfig, error) {
	cfg := AutoUpdateConfig{
		Enabled:       raw.Enabled,
		Channel:       "candidate",
		CheckInterval: 24 * time.Hour,
		Repository:    "AS214933/dn42-bot",
		DataDir:       "/etc/dn42-agent",
		AgentPath:     "/etc/dn42-agent/agent",
		ServiceName:   "dn42-agent.service",
		ServicePath:   "/etc/systemd/system/dn42-agent.service",
	}

	if raw.Channel != nil {
		channel, err := normalizeUpdateChannel(*raw.Channel)
		if err != nil {
			return AutoUpdateConfig{}, err
		}
		cfg.Channel = channel
	}
	if raw.CheckInterval != nil && strings.TrimSpace(*raw.CheckInterval) != "" {
		interval, err := time.ParseDuration(strings.TrimSpace(*raw.CheckInterval))
		if err != nil || interval <= 0 {
			return AutoUpdateConfig{}, fmt.Errorf("invalid auto_update.check_interval %q", *raw.CheckInterval)
		}
		cfg.CheckInterval = interval
	}
	if raw.Repository != nil && strings.TrimSpace(*raw.Repository) != "" {
		cfg.Repository = strings.TrimSpace(*raw.Repository)
	}
	if raw.DataDir != nil && strings.TrimSpace(*raw.DataDir) != "" {
		cfg.DataDir = strings.TrimSpace(*raw.DataDir)
	}
	if raw.AgentPath != nil && strings.TrimSpace(*raw.AgentPath) != "" {
		cfg.AgentPath = strings.TrimSpace(*raw.AgentPath)
	}
	if raw.ServiceName != nil && strings.TrimSpace(*raw.ServiceName) != "" {
		cfg.ServiceName = strings.TrimSpace(*raw.ServiceName)
	}
	if raw.ServicePath != nil && strings.TrimSpace(*raw.ServicePath) != "" {
		cfg.ServicePath = strings.TrimSpace(*raw.ServicePath)
	}

	if !strings.Contains(cfg.Repository, "/") {
		return AutoUpdateConfig{}, fmt.Errorf("invalid auto_update.repository %q: expected owner/repo", cfg.Repository)
	}
	for field, path := range map[string]string{
		"data_dir":     cfg.DataDir,
		"agent_path":   cfg.AgentPath,
		"service_path": cfg.ServicePath,
	} {
		if !filepath.IsAbs(path) {
			return AutoUpdateConfig{}, fmt.Errorf("invalid auto_update.%s %q: expected absolute path", field, path)
		}
	}
	if cfg.ServiceName == "" {
		return AutoUpdateConfig{}, fmt.Errorf("invalid auto_update.service_name: empty")
	}

	return cfg, nil
}

func normalizeUpdateChannel(channel string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "", "candidate", "candidates", "prerelease", "pre", "preview", "alpha", "beta", "rc":
		return "candidate", nil
	case "stable", "release", "releases":
		return "stable", nil
	default:
		return "", fmt.Errorf("invalid auto_update.channel %q: expected stable or candidate", channel)
	}
}

func normalizeDNSServers(servers []string) ([]string, error) {
	result := make([]string, 0, len(servers))
	for _, server := range servers {
		normalized, err := normalizeDNSServer(server)
		if err != nil {
			return nil, err
		}
		if normalized != "" {
			result = append(result, normalized)
		}
	}
	return result, nil
}

func normalizeDNSServer(server string) (string, error) {
	trimmed := strings.TrimSpace(server)
	if trimmed == "" {
		return "", nil
	}

	if addrPort, err := netip.ParseAddrPort(trimmed); err == nil {
		if addrPort.Port() == 0 {
			return "", fmt.Errorf("invalid dns server %q: invalid port", server)
		}
		return addrPort.String(), nil
	}
	if addr, err := netip.ParseAddr(trimmed); err == nil {
		return net.JoinHostPort(addr.String(), "53"), nil
	}

	host, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid dns server %q: expected IP, IP:port, or [IPv6]:port", server)
	}
	if _, err := netip.ParseAddr(host); err != nil {
		return "", fmt.Errorf("invalid dns server %q: DNS server must be an IP address", server)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum <= 0 || portNum > 65535 {
		return "", fmt.Errorf("invalid dns server %q: invalid port", server)
	}
	return net.JoinHostPort(host, port), nil
}
