package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/birdctl"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
)

type ParsedBirdConfig struct {
	Version     int
	Neighbor    string
	Description string
	Only        bool
}

func GenerateProtocol(version int, only bool, peer model.PeerInfo) string {
	var sb strings.Builder
	neighborAddr := peer.IPv6
	if version == 4 {
		neighborAddr = peer.IPv4
	}

	sb.WriteString(fmt.Sprintf("protocol bgp DN42_%d_v%d from dn42_peers {\n", peer.ASN, version))
	sb.WriteString(fmt.Sprintf("    neighbor %s %% 'dn42-%d' external;\n", neighborAddr, peer.ASN))
	sb.WriteString(fmt.Sprintf("    description \"%s\";\n", peer.Contact))

	if only {
		disabledFamily := "ipv6"
		if version == 6 {
			disabledFamily = "ipv4"
		}
		sb.WriteString(fmt.Sprintf("    %s {\n", disabledFamily))
		sb.WriteString("        import none;\n")
		sb.WriteString("        export none;\n")
		sb.WriteString("    };\n")
	}

	sb.WriteString("}\n")
	return sb.String()
}

var birdV6Regex = regexp.MustCompile(
	`(?m)protocol bgp DN42_(\d+)_v6 from dn42_peers \{\n` +
		`(?:(?: +.*?\n)*?.*\n)+?` +
		`^}`)

var birdV4Regex = regexp.MustCompile(
	`(?m)protocol bgp DN42_(\d+)_v4 from dn42_peers \{\n` +
		`(?:(?: +.*?\n)*?.*\n)+?` +
		`^}`)

var birdV6OnlyRegex = regexp.MustCompile(
	` {4}ipv4 \{\n` +
		`(?: {4,}.*?\n)*?` +
		`(?:(?: {8}import none;\n(?: {4,}.*?\n)*? {8}export none;)|(?: {8}export none;\n(?: {4,}.*?\n)*? {8}import none;))\n` +
		`(?: {4,}.*?\n)*?` +
		` {4}\};`)

var birdV4OnlyRegex = regexp.MustCompile(
	` {4}ipv6 \{\n` +
		`(?: {4,}.*?\n)*?` +
		`(?:(?: {8}import none;\n(?: {4,}.*?\n)*? {8}export none;)|(?: {8}export none;\n(?: {4,}.*?\n)*? {8}import none;))\n` +
		`(?: {4,}.*?\n)*?` +
		` {4}\};`)

var birdDescRegex = regexp.MustCompile(`(?m)^ {4}description "(.*)";$`)

func ParseProtocol(asn int, content string) (*ParsedBirdConfig, error) {
	result := &ParsedBirdConfig{}

	v6Match := birdV6Regex.FindString(content)
	v4Match := birdV4Regex.FindString(content)

	if v6Match != "" {
		result.Version = 6
		if birdV6OnlyRegex.MatchString(v6Match) {
			result.Only = true
		}
		if m := birdDescRegex.FindStringSubmatch(v6Match); m != nil {
			result.Description = m[1]
		}
		result.Neighbor = extractNeighbor(v6Match)
	} else if v4Match != "" {
		result.Version = 4
		if birdV4OnlyRegex.MatchString(v4Match) {
			result.Only = true
		}
		if m := birdDescRegex.FindStringSubmatch(v4Match); m != nil {
			result.Description = m[1]
		}
		result.Neighbor = extractNeighbor(v4Match)
	} else {
		return nil, fmt.Errorf("no BIRD protocol found for ASN %d", asn)
	}

	return result, nil
}

var neighborRegex = regexp.MustCompile(`neighbor ([^\s]+) %`)

func extractNeighbor(block string) string {
	m := neighborRegex.FindStringSubmatch(block)
	if m == nil {
		return ""
	}
	return m[1]
}

func ParseBirdStatus(session, output string) (*model.BirdSessionStatus, error) {
	var fields []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 && parts[0] == session {
			fields = parts
			break
		}
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("session %q not found in birdc output", session)
	}

	status := &model.BirdSessionStatus{
		Routes: make(map[string]string),
	}
	if len(fields) >= 6 {
		status.State = fields[5]
	}
	if len(fields) >= 7 {
		status.Info = strings.Join(fields[6:], " ")
	}
	return status, nil
}

func GetProtocolStatus(ctx context.Context, session, ctlPath string) (*model.BirdSessionStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := birdctl.Query(ctx, ctlPath, "show protocols "+session)
	if err != nil {
		return nil, fmt.Errorf("bird show protocols failed: %w", err)
	}
	return ParseBirdStatus(session, output)
}

func ParseBirdAll(session, output string) (map[string]map[string]string, error) {
	channels := make(map[string]map[string]string)
	chunks := strings.Split(output, "Channel ")

	for _, chunk := range chunks {
		lines := strings.Split(strings.TrimSpace(chunk), "\n")
		if len(lines) == 0 {
			continue
		}
		header := strings.TrimSpace(lines[0])
		if !strings.HasPrefix(header, "ipv") {
			continue
		}
		channelName := header
		props := make(map[string]string)
		for _, line := range lines[1:] {
			trimmed := strings.TrimSpace(line)
			idx := strings.Index(trimmed, ":")
			if idx < 0 {
				continue
			}
			key := strings.TrimSpace(trimmed[:idx])
			val := strings.TrimSpace(trimmed[idx+1:])
			props[key] = val
		}
		channels[channelName] = props
	}

	return channels, nil
}

func GetProtocolAll(ctx context.Context, session, ctlPath string) (map[string]map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := birdctl.Query(ctx, ctlPath, "show protocols all "+session)
	if err != nil {
		return nil, fmt.Errorf("bird show protocols all failed: %w", err)
	}
	return ParseBirdAll(session, output)
}

func ReloadConfig(ctx context.Context, ctlPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := birdctl.Query(ctx, ctlPath, "configure")
	return err
}

func RestartProtocol(ctx context.Context, session, ctlPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := birdctl.Query(ctx, ctlPath, "restart "+session)
	return err
}
