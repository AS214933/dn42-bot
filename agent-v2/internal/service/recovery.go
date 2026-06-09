package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

var dnsErrorKeywords = []string{
	"name or service not known",
	"temporary failure in name resolution",
	"no address associated with hostname",
	"nodename nor servname provided",
	"non-recoverable failure in name resolution",
	"could not resolve",
}

type dnsFailure struct {
	ASN      string `json:"asn"`
	Endpoint string `json:"endpoint"`
}

type RecoveryDeps struct {
	RunCommand func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error)
	ReadDir    func(dirname string) ([]os.DirEntry, error)
	ReadFile   func(filename string) ([]byte, error)
	WriteFile  func(filename string, data []byte, perm os.FileMode) error
	HTTPPost   func(ctx context.Context, url string, body []byte, headers map[string]string) error
	WGDir      string
}

func DefaultRecoveryDeps() RecoveryDeps {
	return RecoveryDeps{
		RunCommand: RunCommand,
		ReadDir:    os.ReadDir,
		ReadFile:   os.ReadFile,
		WriteFile:  os.WriteFile,
		HTTPPost:   defaultHTTPPost,
		WGDir:      "/etc/wireguard",
	}
}

func EnsureWGInterfacesUp(ctx context.Context, cfg *config.Config, deps RecoveryDeps) {
	entries, err := deps.ReadDir(deps.WGDir)
	if err != nil {
		return
	}

	configs := make(map[string]string)
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "dn42-") && strings.HasSuffix(name, ".conf") {
			ifaceName := name[:len(name)-5]
			configs[ifaceName] = deps.WGDir + "/" + name
		}
	}
	if len(configs) == 0 {
		return
	}

	out, _ := deps.RunCommand(ctx, "wg", []string{"show", "interfaces"}, 10*time.Second)
	existing := make(map[string]bool)
	for _, iface := range strings.Fields(out) {
		existing[iface] = true
	}

	var toStart []string
	for name := range configs {
		if !existing[name] {
			toStart = append(toStart, name)
		}
	}
	if len(toStart) == 0 {
		return
	}

	var (
		dnsFailures []dnsFailure
		mu          sync.Mutex
		wg          sync.WaitGroup
		sem         = make(chan struct{}, 3)
	)

	for _, ifaceName := range toStart {
		wg.Add(1)
		go func(iface string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			select {
			case <-ctx.Done():
				return
			default:
			}

			configPath := configs[iface]
			output, err := deps.RunCommand(ctx, "wg-quick", []string{"up", iface}, 10*time.Second)
			if err == nil {
				_ = output
				return
			}

			if isDNSError(output) && hasHostnameEndpointWith(configPath, deps.ReadFile) {
				asn := extractASNFromIfaceName(iface)
				endpoint := extractEndpointFromConfigWith(configPath, deps.ReadFile)
				if removeEndpointFromConfigWith(configPath, deps.ReadFile, deps.WriteFile) {
					deps.RunCommand(ctx, "wg-quick", []string{"up", iface}, 10*time.Second)
					mu.Lock()
					dnsFailures = append(dnsFailures, dnsFailure{ASN: asn, Endpoint: endpoint})
					mu.Unlock()
				}
			}
		}(ifaceName)
	}

	wg.Wait()

	if len(dnsFailures) > 0 && cfg.ServerURL != "" {
		notifyServerDNSFailures(ctx, cfg, deps, dnsFailures)
	}
}

func isDNSError(output string) bool {
	if output == "" {
		return false
	}
	lower := strings.ToLower(output)
	for _, kw := range dnsErrorKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func hasHostnameEndpoint(configPath string) bool {
	return hasHostnameEndpointWith(configPath, os.ReadFile)
}

func hasHostnameEndpointWith(configPath string, readFile func(string) ([]byte, error)) bool {
	data, err := readFile(configPath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Endpoint") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) != 2 {
				continue
			}
			value := strings.TrimSpace(parts[1])
			host := value
			if idx := strings.LastIndex(value, ":"); idx != -1 {
				host = value[:idx]
			}
			host = strings.Trim(host, "[]")
			if net.ParseIP(host) == nil {
				return true
			}
			return false
		}
	}
	return false
}

var endpointLineRegex = regexp.MustCompile(`(?m)^Endpoint\s*=.*\n?`)

func removeEndpointFromConfig(configPath string) bool {
	return removeEndpointFromConfigWith(configPath, os.ReadFile, os.WriteFile)
}

func removeEndpointFromConfigWith(configPath string, readFile func(string) ([]byte, error), writeFile func(string, []byte, os.FileMode) error) bool {
	data, err := readFile(configPath)
	if err != nil {
		return false
	}
	newContent := endpointLineRegex.ReplaceAll(data, []byte{})
	return writeFile(configPath, newContent, 0644) == nil
}

func extractASNFromIfaceName(iface string) string {
	if strings.HasPrefix(iface, "dn42-") {
		return iface[5:]
	}
	return iface
}

func extractEndpointFromConfig(configPath string) string {
	return extractEndpointFromConfigWith(configPath, os.ReadFile)
}

func extractEndpointFromConfigWith(configPath string, readFile func(string) ([]byte, error)) string {
	data, err := readFile(configPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Endpoint") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func buildNotificationPayload(failures []dnsFailure) []byte {
	payload := map[string]interface{}{
		"type":     "dns_failure",
		"failures": failures,
	}
	data, _ := json.Marshal(payload)
	return data
}

func notifyServerDNSFailures(ctx context.Context, cfg *config.Config, deps RecoveryDeps, failures []dnsFailure) {
	url := strings.TrimRight(cfg.ServerURL, "/") + "/internal/broadcast"
	body := buildNotificationPayload(failures)
	headers := map[string]string{
		"Content-Type":                "application/json",
		"X-DN42-Bot-Api-Secret-Token": cfg.Secret,
	}
	deps.HTTPPost(ctx, url, body, headers)
}

func defaultHTTPPost(ctx context.Context, url string, body []byte, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	resp.Body.Close()
	return nil
}
