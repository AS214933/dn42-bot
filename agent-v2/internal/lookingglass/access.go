package lookingglass

import (
	"fmt"
	"net/netip"
	"strings"
)

type sourceMatcher interface {
	Match(netip.Addr) bool
}

type prefixMatcher netip.Prefix

func (m prefixMatcher) Match(addr netip.Addr) bool {
	return netip.Prefix(m).Contains(addr.Unmap())
}

type presetMatcher string

func (m presetMatcher) Match(addr netip.Addr) bool {
	addr = addr.Unmap()
	switch string(m) {
	case "any":
		return addr.IsValid()
	case "public":
		return isPublic(addr)
	case "private":
		return addr.IsPrivate()
	case "dn42":
		return matchesAnyPrefix(addr, dn42Prefixes)
	default:
		return false
	}
}

type accessPolicy struct {
	allowed    []sourceMatcher
	disallowed []sourceMatcher
}

// ValidateSourceSelectors validates the source selectors accepted by Config.
func ValidateSourceSelectors(allowed, disallowed []string) error {
	_, err := newAccessPolicy(allowed, disallowed)
	return err
}

func newAccessPolicy(allowed, disallowed []string) (accessPolicy, error) {
	allowedMatchers, err := parseSourceMatchers("allowed", allowed)
	if err != nil {
		return accessPolicy{}, err
	}
	disallowedMatchers, err := parseSourceMatchers("disallowed", disallowed)
	if err != nil {
		return accessPolicy{}, err
	}
	return accessPolicy{allowed: allowedMatchers, disallowed: disallowedMatchers}, nil
}

func (p accessPolicy) Allows(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || matchesAny(addr, p.disallowed) {
		return false
	}
	return matchesAny(addr, p.allowed)
}

func matchesAny(addr netip.Addr, matchers []sourceMatcher) bool {
	for _, matcher := range matchers {
		if matcher.Match(addr) {
			return true
		}
	}
	return false
}

func parseSourceMatchers(field string, values []string) ([]sourceMatcher, error) {
	var matchers []sourceMatcher
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}

			switch preset := strings.ToLower(token); preset {
			case "any", "public", "private", "dn42":
				matchers = append(matchers, presetMatcher(preset))
				continue
			}

			if prefix, err := netip.ParsePrefix(token); err == nil {
				matchers = append(matchers, prefixMatcher(prefix.Masked()))
				continue
			}
			if addr, err := netip.ParseAddr(token); err == nil {
				addr = addr.Unmap()
				bits := 128
				if addr.Is4() {
					bits = 32
				}
				matchers = append(matchers, prefixMatcher(netip.PrefixFrom(addr, bits)))
				continue
			}

			return nil, fmt.Errorf("invalid %s source selector %q", field, token)
		}
	}
	return matchers, nil
}

// The dn42 selector includes native DN42 space plus the legacy 172.31/16
// affiliate range used by this project. It intentionally excludes other
// private and ULA address space.
var dn42Prefixes = mustPrefixes(
	"172.20.0.0/14",
	"172.31.0.0/16",
	"fd00::/8",
)

// These special-purpose ranges are not part of the public Internet even
// though some of them are classified as global unicast by netip.
var nonPublicPrefixes = mustPrefixes(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.88.99.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"64:ff9b:1::/48",
	"100::/64",
	"2001:2::/48",
	"2001:10::/28",
	"2001:20::/28",
	"2001:db8::/32",
	"3fff::/20",
	"5f00::/16",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
)

func isPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsGlobalUnicast() && !matchesAnyPrefix(addr, nonPublicPrefixes)
}

func matchesAnyPrefix(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func mustPrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}
