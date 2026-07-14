package handler

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/middleware"
)

const testSecret = "test-secret"

func configTestCfg() *config.Config {
	return &config.Config{
		Secret:             testSecret,
		DefaultMTU:         1420,
		Open:               true,
		MaxPeers:           10,
		MinPeerRequirement: 1,
		ExtraMsg:           "hello",
		MyDN42IPv4Address:  net.ParseIP("172.20.0.1"),
		VnstatAutoAdd:      true,
		VnstatAutoRemove:   false,
		BirdCtlPath:        "/var/run/bird/bird.ctl",
		BirdTable4:         "master4",
		BirdTable6:         "master6",
		NetSupport: config.NetSupport{
			IPv4:    true,
			IPv6:    true,
			IPv4NAT: false,
			CN:      true,
		},
		LookingGlass: config.LookingGlassConfig{
			Enabled:                 true,
			AllowedCIDRs:            []string{"dn42"},
			DisallowedCIDRs:         []string{"172.20.0.1"},
			TracerouteEnabled:       true,
			BirdMaxConcurrent:       16,
			TracerouteMaxConcurrent: 10,
			RequestTimeout:          15 * time.Second,
			MaxQueryLength:          4096,
			MaxOutputBytes:          65536,
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
		"BIRD_TABLE_6", "NET_SUPPORT", "LOOKING_GLASS",
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

func TestConfigGetLookingGlass(t *testing.T) {
	t.Parallel()
	cfg := configTestCfg()
	h := middleware.AuthMiddleware(cfg.Secret)(ConfigGetHandler(cfg))
	body, err := json.Marshal(map[string]interface{}{"keys": []string{"LOOKING_GLASS"}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/config/get", bytes.NewReader(body))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response map[string]struct {
		Enabled         bool     `json:"enabled"`
		AllowedCIDRs    []string `json:"allowed_cidrs"`
		DisallowedCIDRs []string `json:"disallowed_cidrs"`
		RequestTimeout  string   `json:"request_timeout"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	lg, ok := response["LOOKING_GLASS"]
	if !ok || !lg.Enabled || len(lg.AllowedCIDRs) != 1 || lg.AllowedCIDRs[0] != "dn42" || len(lg.DisallowedCIDRs) != 1 || lg.RequestTimeout != "15s" {
		t.Fatalf("LOOKING_GLASS = %+v", lg)
	}
}
