package handler

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

func prePeerTestConfig() *config.Config {
	return &config.Config{
		MaxPeers:           10,
		MinPeerRequirement: 2,
		Open:               true,
		NetSupport: config.NetSupport{
			IPv4:    true,
			IPv6:    true,
			IPv4NAT: false,
			CN:      true,
		},
		MyDN42LinkLocalAddress: net.ParseIP("fe80::1"),
		ExtraMsg:               "Hello from test",
	}
}

func TestPrePeer_Success(t *testing.T) {
	t.Parallel()
	cfg := prePeerTestConfig()
	h := &PrePeerHandler{
		Cfg: cfg,
		GetPeerNum: func() (int, int, error) {
			return 5, 5, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/pre_peer", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp PrePeerResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	assertInt(t, "existed", resp.Existed, 5)
	assertInt(t, "max", resp.Max, 10)
	assertInt(t, "requirement", resp.Requirement, 2)
	if !resp.Open {
		t.Error("expected open=true")
	}
	if resp.LLA != "fe80::1" {
		t.Errorf("expected lla=fe80::1, got %s", resp.LLA)
	}
	if resp.Msg != "Hello from test" {
		t.Errorf("expected msg='Hello from test', got %s", resp.Msg)
	}
	if !resp.NetSupport.IPv4 || !resp.NetSupport.IPv6 || resp.NetSupport.IPv4NAT || !resp.NetSupport.CN {
		t.Errorf("unexpected net_support: %+v", resp.NetSupport)
	}
}

func TestPrePeer_GetPeerNumError(t *testing.T) {
	t.Parallel()
	cfg := prePeerTestConfig()
	h := &PrePeerHandler{
		Cfg: cfg,
		GetPeerNum: func() (int, int, error) {
			return 0, 0, errors.New("wireguard and bird config count mismatch: wg=3 bird=2")
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/pre_peer", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestPrePeer_ZeroPeers(t *testing.T) {
	t.Parallel()
	cfg := prePeerTestConfig()
	cfg.MaxPeers = 0
	cfg.Open = false
	cfg.ExtraMsg = ""
	h := &PrePeerHandler{
		Cfg: cfg,
		GetPeerNum: func() (int, int, error) {
			return 0, 0, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/pre_peer", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp PrePeerResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	assertInt(t, "existed", resp.Existed, 0)
	assertInt(t, "max", resp.Max, 0)
	if resp.Open {
		t.Error("expected open=false")
	}
	if resp.Msg != "" {
		t.Errorf("expected empty msg, got %s", resp.Msg)
	}
}

func TestPrePeer_JSONKeys(t *testing.T) {
	t.Parallel()
	cfg := prePeerTestConfig()
	h := &PrePeerHandler{
		Cfg: cfg,
		GetPeerNum: func() (int, int, error) {
			return 3, 3, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/pre_peer", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	expectedKeys := []string{"existed", "max", "requirement", "open", "net_support", "lla", "msg"}
	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing expected JSON key: %s", key)
		}
	}
}

func assertInt(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %d, want %d", name, got, want)
	}
}
