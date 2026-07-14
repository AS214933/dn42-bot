package lookingglass

import (
	"net/netip"
	"testing"
)

func TestAccessPolicyAllowedEmptyDeniesAll(t *testing.T) {
	t.Parallel()
	policy, err := newAccessPolicy(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Allows(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("an empty allowed list must deny access")
	}
}

func TestAccessPolicyDisallowedTakesPriority(t *testing.T) {
	t.Parallel()
	policy, err := newAccessPolicy([]string{"any"}, []string{"192.0.2.10"})
	if err != nil {
		t.Fatal(err)
	}
	if policy.Allows(netip.MustParseAddr("192.0.2.10")) {
		t.Fatal("disallowed address was accepted")
	}
	if !policy.Allows(netip.MustParseAddr("192.0.2.11")) {
		t.Fatal("unblocked address was rejected")
	}
}

func TestAccessPolicyCIDRAndSingleIP(t *testing.T) {
	t.Parallel()
	policy, err := newAccessPolicy([]string{"192.0.2.0/24", "2001:db8::1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"192.0.2.20", "2001:db8::1"} {
		if !policy.Allows(netip.MustParseAddr(value)) {
			t.Errorf("expected %s to be allowed", value)
		}
	}
	for _, value := range []string{"198.51.100.1", "2001:db8::2"} {
		if policy.Allows(netip.MustParseAddr(value)) {
			t.Errorf("expected %s to be denied", value)
		}
	}
}

func TestAccessPolicyPresets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		selector string
		allowed  []string
		denied   []string
	}{
		{
			name:     "any",
			selector: "any",
			allowed:  []string{"8.8.8.8", "127.0.0.1", "fd00::1"},
		},
		{
			name:     "public",
			selector: "public",
			allowed:  []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"},
			denied: []string{
				"10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.1.1",
				"172.20.0.1", "192.0.2.1", "198.18.0.1", "203.0.113.1",
				"fd00::1", "fe80::1", "2001:db8::1", "3fff::1",
			},
		},
		{
			name:     "private",
			selector: "private",
			allowed:  []string{"10.0.0.1", "172.16.0.1", "192.168.0.1", "fd00::1"},
			denied:   []string{"8.8.8.8", "100.64.0.1", "fe80::1"},
		},
		{
			name:     "dn42",
			selector: "dn42",
			allowed:  []string{"172.20.0.1", "172.23.255.255", "172.31.0.1", "fd42::1"},
			denied:   []string{"172.19.255.255", "172.24.0.1", "10.127.0.1", "fc42::1", "8.8.8.8"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			policy, err := newAccessPolicy([]string{test.selector}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range test.allowed {
				if !policy.Allows(netip.MustParseAddr(value)) {
					t.Errorf("expected %s to match %s", value, test.selector)
				}
			}
			for _, value := range test.denied {
				if policy.Allows(netip.MustParseAddr(value)) {
					t.Errorf("expected %s not to match %s", value, test.selector)
				}
			}
		})
	}
}

func TestAccessPolicyBuiltInChoices(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		allowed    []string
		disallowed []string
		accept     []string
		reject     []string
	}{
		{
			name:       "block public",
			allowed:    []string{"any"},
			disallowed: []string{"public"},
			accept:     []string{"10.0.0.1", "172.20.0.1", "fd42::1"},
			reject:     []string{"8.8.8.8", "2606:4700:4700::1111"},
		},
		{
			name:    "dn42 only",
			allowed: []string{"dn42"},
			accept:  []string{"172.20.0.1", "172.31.0.1", "fd42::1"},
			reject:  []string{"10.0.0.1", "8.8.8.8", "fc42::1"},
		},
		{
			name:    "public and dn42",
			allowed: []string{"public", "dn42"},
			accept:  []string{"8.8.8.8", "172.20.0.1", "fd42::1"},
			reject:  []string{"10.0.0.1", "192.168.0.1", "fc42::1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			policy, err := newAccessPolicy(test.allowed, test.disallowed)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range test.accept {
				if !policy.Allows(netip.MustParseAddr(value)) {
					t.Errorf("expected %s to be accepted", value)
				}
			}
			for _, value := range test.reject {
				if policy.Allows(netip.MustParseAddr(value)) {
					t.Errorf("expected %s to be rejected", value)
				}
			}
		})
	}
}

func TestAccessPolicyAcceptsCommaSeparatedSelectors(t *testing.T) {
	t.Parallel()
	policy, err := newAccessPolicy([]string{"192.0.2.1, 2001:db8::1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Allows(netip.MustParseAddr("192.0.2.1")) || !policy.Allows(netip.MustParseAddr("2001:db8::1")) {
		t.Fatal("comma-separated selectors were not parsed")
	}
}

func TestAccessPolicyRejectsInvalidSelector(t *testing.T) {
	t.Parallel()
	if _, err := newAccessPolicy([]string{"not-a-selector"}, nil); err == nil {
		t.Fatal("expected an invalid selector error")
	}
	if _, err := newAccessPolicy([]string{"any"}, []string{"bad-prefix"}); err == nil {
		t.Fatal("expected an invalid disallowed selector error")
	}
}
