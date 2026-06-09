package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
)

func errorlistTestConfig() *config.Config {
	return &config.Config{
		BirdCtlPath: "/var/run/bird/bird.ctl",
	}
}

func TestErrorlist_NoPeers(t *testing.T) {
	t.Parallel()
	h := &ErrorListHandler{
		Cfg: errorlistTestConfig(),
		ListWGASNs: func() ([]int, error) {
			return nil, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return nil, nil
		},
		Now: func() time.Time { return time.Unix(1700000000, 0) },
	}

	req := httptest.NewRequest(http.MethodPost, "/errorlist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp []ErrorEntry
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty array, got %d entries", len(resp))
	}
}

func TestErrorlist_AllHealthy(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	h := &ErrorListHandler{
		Cfg: errorlistTestConfig(),
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		GetHandshake: func(ctx context.Context, iface string) (int64, error) {
			return now.Unix() - 60, nil
		},
		GetProtocolStatus: func(ctx context.Context, session, ctlPath string) (*model.BirdSessionStatus, error) {
			return &model.BirdSessionStatus{State: "Established"}, nil
		},
		ReadBirdConfig: func(asn int) (string, error) {
			return "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
				"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
				"    description \"test@example.com\";\n" +
				"}\n", nil
		},
		Now: func() time.Time { return now },
	}

	req := httptest.NewRequest(http.MethodPost, "/errorlist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp []ErrorEntry
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected no errors for healthy peer, got %d entries", len(resp))
		for _, e := range resp {
			t.Logf("  ASN %d: %v", e.ASN, e.Issues)
		}
	}
}

func TestErrorlist_WGConfigMissing(t *testing.T) {
	t.Parallel()
	h := &ErrorListHandler{
		Cfg: errorlistTestConfig(),
		ListWGASNs: func() ([]int, error) {
			return nil, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		ReadBirdConfig: func(asn int) (string, error) {
			return "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
				"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
				"    description \"test@example.com\";\n" +
				"}\n", nil
		},
		GetProtocolStatus: func(ctx context.Context, session, ctlPath string) (*model.BirdSessionStatus, error) {
			return &model.BirdSessionStatus{State: "Established"}, nil
		},
		Now: func() time.Time { return time.Unix(1700000000, 0) },
	}

	req := httptest.NewRequest(http.MethodPost, "/errorlist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp []ErrorEntry
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 error entry, got %d", len(resp))
	}
	assertInt(t, "asn", resp[0].ASN, 4242421234)
	if len(resp[0].Issues) != 1 || resp[0].Issues[0] != "BIRD config exists but WireGuard config missing" {
		t.Errorf("unexpected issues: %v", resp[0].Issues)
	}
}

func TestErrorlist_BIRDConfigMissing(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	h := &ErrorListHandler{
		Cfg: errorlistTestConfig(),
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return nil, nil
		},
		GetHandshake: func(ctx context.Context, iface string) (int64, error) {
			return now.Unix() - 60, nil
		},
		Now: func() time.Time { return now },
	}

	req := httptest.NewRequest(http.MethodPost, "/errorlist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp []ErrorEntry
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 error entry, got %d", len(resp))
	}
	assertInt(t, "asn", resp[0].ASN, 4242421234)
	if len(resp[0].Issues) != 1 || resp[0].Issues[0] != "WireGuard config exists but BIRD config missing" {
		t.Errorf("unexpected issues: %v", resp[0].Issues)
	}
}

func TestErrorlist_WGHandshakeStale(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	h := &ErrorListHandler{
		Cfg: errorlistTestConfig(),
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		GetHandshake: func(ctx context.Context, iface string) (int64, error) {
			return now.Unix() - 1000, nil
		},
		GetProtocolStatus: func(ctx context.Context, session, ctlPath string) (*model.BirdSessionStatus, error) {
			return &model.BirdSessionStatus{State: "Established"}, nil
		},
		ReadBirdConfig: func(asn int) (string, error) {
			return "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
				"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
				"    description \"test@example.com\";\n" +
				"}\n", nil
		},
		Now: func() time.Time { return now },
	}

	req := httptest.NewRequest(http.MethodPost, "/errorlist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp []ErrorEntry
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 error entry, got %d", len(resp))
	}
	found := false
	for _, issue := range resp[0].Issues {
		if issue == "WireGuard handshake stale" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'WireGuard handshake stale' in issues, got %v", resp[0].Issues)
	}
}

func TestErrorlist_WGNeverHandshaked(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	h := &ErrorListHandler{
		Cfg: errorlistTestConfig(),
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		GetHandshake: func(ctx context.Context, iface string) (int64, error) {
			return 0, nil
		},
		GetProtocolStatus: func(ctx context.Context, session, ctlPath string) (*model.BirdSessionStatus, error) {
			return &model.BirdSessionStatus{State: "Established"}, nil
		},
		ReadBirdConfig: func(asn int) (string, error) {
			return "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
				"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
				"    description \"test@example.com\";\n" +
				"}\n", nil
		},
		Now: func() time.Time { return now },
	}

	req := httptest.NewRequest(http.MethodPost, "/errorlist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp []ErrorEntry
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 error entry, got %d", len(resp))
	}
	found := false
	for _, issue := range resp[0].Issues {
		if issue == "WireGuard never handshaked" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'WireGuard never handshaked' in issues, got %v", resp[0].Issues)
	}
}

func TestErrorlist_BIRDNotEstablished(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	h := &ErrorListHandler{
		Cfg: errorlistTestConfig(),
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		GetHandshake: func(ctx context.Context, iface string) (int64, error) {
			return now.Unix() - 60, nil
		},
		GetProtocolStatus: func(ctx context.Context, session, ctlPath string) (*model.BirdSessionStatus, error) {
			return &model.BirdSessionStatus{State: "start"}, nil
		},
		ReadBirdConfig: func(asn int) (string, error) {
			return "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
				"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
				"    description \"test@example.com\";\n" +
				"}\n", nil
		},
		Now: func() time.Time { return now },
	}

	req := httptest.NewRequest(http.MethodPost, "/errorlist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp []ErrorEntry
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 error entry, got %d", len(resp))
	}
	found := false
	for _, issue := range resp[0].Issues {
		if issue == "BIRD DN42_4242421234_v6 state: start" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected BIRD state issue in issues, got %v", resp[0].Issues)
	}
}

func TestErrorlist_BIRDSessionsNotFound(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	h := &ErrorListHandler{
		Cfg: errorlistTestConfig(),
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		GetHandshake: func(ctx context.Context, iface string) (int64, error) {
			return now.Unix() - 60, nil
		},
		GetProtocolStatus: func(ctx context.Context, session, ctlPath string) (*model.BirdSessionStatus, error) {
			return nil, errors.New("session not found")
		},
		ReadBirdConfig: func(asn int) (string, error) {
			return "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
				"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
				"    description \"test@example.com\";\n" +
				"}\n" +
				"\n" +
				"protocol bgp DN42_4242421234_v4 from dn42_peers {\n" +
				"    neighbor 172.22.167.101 % 'dn42-4242421234' external;\n" +
				"    description \"test@example.com\";\n" +
				"}\n", nil
		},
		Now: func() time.Time { return now },
	}

	req := httptest.NewRequest(http.MethodPost, "/errorlist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp []ErrorEntry
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 error entry, got %d", len(resp))
	}
	v4Found := false
	v6Found := false
	for _, issue := range resp[0].Issues {
		if issue == "BIRD DN42_4242421234_v4 check failed" {
			v4Found = true
		}
		if issue == "BIRD DN42_4242421234_v6 check failed" {
			v6Found = true
		}
	}
	if !v4Found {
		t.Errorf("expected v4 check failure in issues, got %v", resp[0].Issues)
	}
	if !v6Found {
		t.Errorf("expected v6 check failure in issues, got %v", resp[0].Issues)
	}
}

func TestErrorlist_MultipleASNs(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	h := &ErrorListHandler{
		Cfg: errorlistTestConfig(),
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234, 4242421816}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return []int{4242421234, 4242421816}, nil
		},
		GetHandshake: func(ctx context.Context, iface string) (int64, error) {
			if iface == "dn42-4242421234" {
				return now.Unix() - 60, nil
			}
			return now.Unix() - 2000, nil
		},
		GetProtocolStatus: func(ctx context.Context, session, ctlPath string) (*model.BirdSessionStatus, error) {
			if session == "DN42_4242421234_v6" {
				return &model.BirdSessionStatus{State: "Established"}, nil
			}
			return &model.BirdSessionStatus{State: "start"}, nil
		},
		ReadBirdConfig: func(asn int) (string, error) {
			if asn == 4242421234 {
				return "protocol bgp DN42_4242421234_v6 from dn42_peers {\n" +
					"    neighbor fd42:d42:d42::1234 % 'dn42-4242421234' external;\n" +
					"    description \"test@example.com\";\n" +
					"}\n", nil
			}
			return "protocol bgp DN42_4242421816_v6 from dn42_peers {\n" +
				"    neighbor fd42:d42:d42::1816 % 'dn42-4242421816' external;\n" +
				"    description \"peer2@example.com\";\n" +
				"}\n", nil
		},
		Now: func() time.Time { return now },
	}

	req := httptest.NewRequest(http.MethodPost, "/errorlist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp []ErrorEntry
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp) != 1 {
		t.Fatalf("expected 1 error entry (only ASN 4242421816), got %d", len(resp))
	}
	assertInt(t, "asn", resp[0].ASN, 4242421816)
	if len(resp[0].Issues) != 2 {
		t.Errorf("expected 2 issues for ASN 4242421816, got %d: %v", len(resp[0].Issues), resp[0].Issues)
	}
}

func TestErrorlist_ResponseSorted(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	h := &ErrorListHandler{
		Cfg: errorlistTestConfig(),
		ListWGASNs: func() ([]int, error) {
			return []int{4242429999, 4242421111}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return []int{4242429999, 4242421111}, nil
		},
		GetHandshake: func(ctx context.Context, iface string) (int64, error) {
			return 0, nil
		},
		GetProtocolStatus: func(ctx context.Context, session, ctlPath string) (*model.BirdSessionStatus, error) {
			return &model.BirdSessionStatus{State: "Established"}, nil
		},
		ReadBirdConfig: func(asn int) (string, error) {
			return "protocol bgp DN42_" + fmt.Sprintf("%d", asn) + "_v6 from dn42_peers {\n" +
				"    neighbor fd42:d42:d42::1 % 'dn42-" + fmt.Sprintf("%d", asn) + "' external;\n" +
				"    description \"test@example.com\";\n" +
				"}\n", nil
		},
		Now: func() time.Time { return now },
	}

	req := httptest.NewRequest(http.MethodPost, "/errorlist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp []ErrorEntry
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 error entries, got %d", len(resp))
	}
	if resp[0].ASN >= resp[1].ASN {
		t.Errorf("expected sorted response, got ASN %d before %d", resp[0].ASN, resp[1].ASN)
	}
}

func TestErrorlist_JSONKeys(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	h := &ErrorListHandler{
		Cfg: errorlistTestConfig(),
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return nil, nil
		},
		GetHandshake: func(ctx context.Context, iface string) (int64, error) {
			return now.Unix() - 60, nil
		},
		Now: func() time.Time { return now },
	}

	req := httptest.NewRequest(http.MethodPost, "/errorlist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var raw []map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(raw))
	}
	if _, ok := raw[0]["asn"]; !ok {
		t.Error("missing expected JSON key: asn")
	}
	if _, ok := raw[0]["issues"]; !ok {
		t.Error("missing expected JSON key: issues")
	}
}
