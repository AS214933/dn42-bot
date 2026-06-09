package handler

import (
	"encoding/json"
	"net/http"
	"sort"
)

type ListPeersHandler struct {
	ListWGASNs   func() ([]int, error)
	ListBirdASNs func() ([]int, error)
}

type ListPeersResponse struct {
	ASNs []int `json:"asns"`
}

func (h *ListPeersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wgASNs, err := h.ListWGASNs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	birdASNs, err := h.ListBirdASNs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	seen := make(map[int]bool)
	for _, asn := range wgASNs {
		seen[asn] = true
	}
	for _, asn := range birdASNs {
		seen[asn] = true
	}

	asns := make([]int, 0, len(seen))
	for asn := range seen {
		asns = append(asns, asn)
	}
	sort.Ints(asns)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ListPeersResponse{ASNs: asns})
}
