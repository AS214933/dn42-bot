package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListPeers_NoPeers(t *testing.T) {
	t.Parallel()
	h := &ListPeersHandler{
		ListWGASNs: func() ([]int, error) {
			return nil, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return nil, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/listpeers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp ListPeersResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.ASNs) != 0 {
		t.Errorf("expected empty asns array, got %v", resp.ASNs)
	}
}

func TestListPeers_OnlyWG(t *testing.T) {
	t.Parallel()
	h := &ListPeersHandler{
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234, 4242421816}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return nil, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/listpeers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp ListPeersResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.ASNs) != 2 {
		t.Fatalf("expected 2 asns, got %d", len(resp.ASNs))
	}
	assertInt(t, "asns[0]", resp.ASNs[0], 4242421234)
	assertInt(t, "asns[1]", resp.ASNs[1], 4242421816)
}

func TestListPeers_OnlyBird(t *testing.T) {
	t.Parallel()
	h := &ListPeersHandler{
		ListWGASNs: func() ([]int, error) {
			return nil, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return []int{4242429999}, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/listpeers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp ListPeersResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.ASNs) != 1 {
		t.Fatalf("expected 1 asn, got %d", len(resp.ASNs))
	}
	assertInt(t, "asns[0]", resp.ASNs[0], 4242429999)
}

func TestListPeers_Deduplicated(t *testing.T) {
	t.Parallel()
	h := &ListPeersHandler{
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234, 4242421816}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return []int{4242421234, 4242429999}, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/listpeers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp ListPeersResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.ASNs) != 3 {
		t.Fatalf("expected 3 unique asns, got %d: %v", len(resp.ASNs), resp.ASNs)
	}
	assertInt(t, "asns[0]", resp.ASNs[0], 4242421234)
	assertInt(t, "asns[1]", resp.ASNs[1], 4242421816)
	assertInt(t, "asns[2]", resp.ASNs[2], 4242429999)
}

func TestListPeers_Sorted(t *testing.T) {
	t.Parallel()
	h := &ListPeersHandler{
		ListWGASNs: func() ([]int, error) {
			return []int{4242429999, 4242421111, 4242425555}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return nil, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/listpeers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp ListPeersResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.ASNs) != 3 {
		t.Fatalf("expected 3 asns, got %d", len(resp.ASNs))
	}
	for i := 1; i < len(resp.ASNs); i++ {
		if resp.ASNs[i-1] >= resp.ASNs[i] {
			t.Errorf("response not sorted: asns[%d]=%d >= asns[%d]=%d", i-1, resp.ASNs[i-1], i, resp.ASNs[i])
		}
	}
}

func TestListPeers_WGError(t *testing.T) {
	t.Parallel()
	h := &ListPeersHandler{
		ListWGASNs: func() ([]int, error) {
			return nil, errors.New("wireguard dir not found")
		},
		ListBirdASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/listpeers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestListPeers_BirdError(t *testing.T) {
	t.Parallel()
	h := &ListPeersHandler{
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return nil, errors.New("bird dir not found")
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/listpeers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestListPeers_JSONKeys(t *testing.T) {
	t.Parallel()
	h := &ListPeersHandler{
		ListWGASNs: func() ([]int, error) {
			return []int{4242421234}, nil
		},
		ListBirdASNs: func() ([]int, error) {
			return nil, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/listpeers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := raw["asns"]; !ok {
		t.Error("missing expected JSON key: asns")
	}
}
