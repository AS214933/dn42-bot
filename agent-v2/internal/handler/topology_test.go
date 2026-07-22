package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/middleware"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
)

func TestParseBabelNeighbors(t *testing.T) {
	output := "BABEL 100 ready.\n" +
		"fe80::abcd    wg0     96    10    123\n" +
		"fd42::1       wg1     128   5     456\n"

	neighbors := ParseBabelNeighbors(output)
	if len(neighbors) != 2 {
		t.Fatalf("expected 2 neighbors, got %d", len(neighbors))
	}

	if neighbors[0].Interface != "wg0" {
		t.Errorf("expected interface wg0, got %s", neighbors[0].Interface)
	}
	if neighbors[0].Cost != 96 {
		t.Errorf("expected cost 96, got %d", neighbors[0].Cost)
	}
	if neighbors[0].Address != "fe80::abcd" {
		t.Errorf("expected address fe80::abcd, got %s", neighbors[0].Address)
	}

	if neighbors[1].Interface != "wg1" {
		t.Errorf("expected interface wg1, got %s", neighbors[1].Interface)
	}
	if neighbors[1].Cost != 128 {
		t.Errorf("expected cost 128, got %d", neighbors[1].Cost)
	}
	if neighbors[1].Address != "fd42::1" {
		t.Errorf("expected address fd42::1, got %s", neighbors[1].Address)
	}
}

func TestParseBabelNeighborsEdgeCases(t *testing.T) {
	output := "BABEL 100 ready.\n" +
		"not-an-ip    wg0     96\n" +
		"fe80::1      wg0\n" +
		"some-header:\n" +
		"\n" +
		"fe80::2      wg1     bad\n"

	neighbors := ParseBabelNeighbors(output)
	if len(neighbors) != 1 {
		t.Fatalf("expected 1 neighbor, got %d", len(neighbors))
	}
	if neighbors[0].Address != "fe80::2" {
		t.Errorf("expected address fe80::2, got %s", neighbors[0].Address)
	}
	if neighbors[0].Cost != 65535 {
		t.Errorf("expected default cost 65535, got %d", neighbors[0].Cost)
	}
}

func TestParseBabelInterfaces(t *testing.T) {
	output := "BABEL 100 ready.\n" +
		"Interface    State Auth RXcost Nbrs Timer NextHop(v4) NextHop(v6)\n" +
		"wg0          Up    no   96     1    5.000 172.20.0.1  fd42:42::1\n" +
		"wg1          Up    no   96     0    5.000 172.20.0.2  fd42:42::2\n"

	interfaces := ParseBabelInterfaces(output)
	if len(interfaces) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(interfaces))
	}

	if interfaces[0].Interface != "wg0" {
		t.Errorf("expected interface wg0, got %s", interfaces[0].Interface)
	}
	if interfaces[0].NextHopV4 == nil || *interfaces[0].NextHopV4 != "172.20.0.1" {
		v := "<nil>"
		if interfaces[0].NextHopV4 != nil {
			v = *interfaces[0].NextHopV4
		}
		t.Errorf("expected next_hop_v4 172.20.0.1, got %s", v)
	}
	if interfaces[0].NextHopV6 == nil || *interfaces[0].NextHopV6 != "fd42:42::1" {
		v := "<nil>"
		if interfaces[0].NextHopV6 != nil {
			v = *interfaces[0].NextHopV6
		}
		t.Errorf("expected next_hop_v6 fd42:42::1, got %s", v)
	}

	if interfaces[1].Interface != "wg1" {
		t.Errorf("expected interface wg1, got %s", interfaces[1].Interface)
	}
}

func TestParseBabelInterfacesEdgeCases(t *testing.T) {
	output := "BABEL 100 ready.\n" +
		"Interface    State Auth RXcost Nbrs Timer NextHop(v4) NextHop(v6)\n" +
		"wg0          Up\n" +
		"some:\n" +
		"\n" +
		"wg1          Up    no   96     0    5.000 bad-ip     also-bad\n"

	interfaces := ParseBabelInterfaces(output)
	if len(interfaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(interfaces))
	}
	if interfaces[0].Interface != "wg1" {
		t.Errorf("expected interface wg1, got %s", interfaces[0].Interface)
	}
	if interfaces[0].NextHopV4 != nil {
		t.Errorf("expected next_hop_v4 nil, got %s", *interfaces[0].NextHopV4)
	}
	if interfaces[0].NextHopV6 != nil {
		t.Errorf("expected next_hop_v6 nil, got %s", *interfaces[0].NextHopV6)
	}
}

func TestTopologyWithValidAuth(t *testing.T) {
	cfg := configTestCfg()
	mockBird := func(_ context.Context, command string) (string, error) {
		if strings.Contains(command, "interfaces") {
			return "BABEL 100 ready.\n" +
				"Interface    State Auth RXcost Nbrs Timer NextHop(v4) NextHop(v6)\n" +
				"wg0          Up    no   96     1    5.000 172.20.0.1  fd42:42::1\n", nil
		}
		return "BABEL 100 ready.\n" +
			"fe80::abcd    wg0     96    10    123\n", nil
	}

	handler := middleware.AuthMiddleware(cfg.Secret)(TopologyHandler(cfg, mockBird))

	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPost, "/igp_topology", bytes.NewReader(body))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp model.IGPTopologyResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Protocol != "babel" {
		t.Errorf("expected protocol babel, got %s", resp.Protocol)
	}
	if len(resp.Interfaces) != 1 {
		t.Errorf("expected 1 interface, got %d", len(resp.Interfaces))
	}
	if len(resp.Neighbors) != 1 {
		t.Errorf("expected 1 neighbor, got %d", len(resp.Neighbors))
	}
	if len(resp.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(resp.Errors), resp.Errors)
	}
}

func TestTopologyWithInvalidAuth(t *testing.T) {
	cfg := configTestCfg()
	mockBird := func(_ context.Context, _ string) (string, error) {
		t.Fatal("bird command should not be called when auth fails")
		return "", nil
	}

	handler := middleware.AuthMiddleware(cfg.Secret)(TopologyHandler(cfg, mockBird))

	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPost, "/igp_topology", bytes.NewReader(body))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "wrong-secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.Code)
	}
}

func TestTopologyHandlesBirdcFailure(t *testing.T) {
	cfg := configTestCfg()
	mockBird := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("birdc not available")
	}

	handler := middleware.AuthMiddleware(cfg.Secret)(TopologyHandler(cfg, mockBird))

	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPost, "/igp_topology", bytes.NewReader(body))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp model.IGPTopologyResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Protocol != "babel" {
		t.Errorf("expected protocol babel, got %s", resp.Protocol)
	}
	if len(resp.Interfaces) != 0 {
		t.Errorf("expected 0 interfaces, got %d", len(resp.Interfaces))
	}
	if len(resp.Neighbors) != 0 {
		t.Errorf("expected 0 neighbors, got %d", len(resp.Neighbors))
	}
	if len(resp.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d: %v", len(resp.Errors), resp.Errors)
	}
}
