package handler

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/middleware"
)

const testSecret = "test-secret"

func configTestCfg() *config.Config {
	return &config.Config{
		Secret:              testSecret,
		DefaultMTU:          1420,
		Open:                true,
		MaxPeers:            10,
		MinPeerRequirement:  1,
		ExtraMsg:            "hello",
		MyDN42IPv4Address:   net.ParseIP("172.20.0.1"),
		VnstatAutoAdd:       true,
		VnstatAutoRemove:    false,
		BirdCtlPath:         "/var/run/bird/bird.ctl",
		BirdTable4:          "master4",
		BirdTable6:          "master6",
		NetSupport: config.NetSupport{
			IPv4:    true,
			IPv6:    true,
			IPv4NAT: false,
			CN:      true,
		},
	}
}

func TestConfigGetWithValidAuth(t *testing.T) {
	cfg := configTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(ConfigGetHandler(cfg))

	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPost, "/config/get", bytes.NewReader(body))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
}

func TestConfigGetWithInvalidAuth(t *testing.T) {
	cfg := configTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(ConfigGetHandler(cfg))

	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPost, "/config/get", bytes.NewReader(body))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "wrong-secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.Code)
	}
}

func TestConfigGetWithSpecificKeys(t *testing.T) {
	cfg := configTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(ConfigGetHandler(cfg))

	body, _ := json.Marshal(map[string]interface{}{
		"keys": []string{"OPEN", "MAX_PEERS"},
	})
	req := httptest.NewRequest(http.MethodPost, "/config/get", bytes.NewReader(body))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp) != 2 {
		t.Errorf("expected 2 fields, got %d: %v", len(resp), resp)
	}
	if v, ok := resp["OPEN"]; !ok || v != true {
		t.Errorf("expected OPEN=true, got %v", v)
	}
	if v, ok := resp["MAX_PEERS"]; !ok || v.(float64) != 10 {
		t.Errorf("expected MAX_PEERS=10, got %v", v)
	}
}

func TestConfigGetWithEmptyBody(t *testing.T) {
	cfg := configTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(ConfigGetHandler(cfg))

	req := httptest.NewRequest(http.MethodPost, "/config/get", nil)
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	expectedFields := []string{
		"DEFAULT_MTU", "OPEN", "MAX_PEERS", "MIN_PEER_REQUIREMENT",
		"EXTRA_MSG", "MY_DN42_IPv4_ADDRESS", "VNSTAT_AUTO_ADD",
		"VNSTAT_AUTO_REMOVE", "BIRD_CTL_PATH", "BIRD_TABLE_4",
		"BIRD_TABLE_6", "NET_SUPPORT",
	}
	for _, field := range expectedFields {
		if _, ok := resp[field]; !ok {
			t.Errorf("expected field %s in response", field)
		}
	}
	if len(resp) != len(expectedFields) {
		t.Errorf("expected %d fields, got %d: %v", len(expectedFields), len(resp), resp)
	}
}
