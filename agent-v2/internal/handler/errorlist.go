package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
)

const wgHandshakeStaleSeconds = 900

var birdV4SessionRegex = regexp.MustCompile(`protocol bgp DN42_(\d+)_v4 `)
var birdV6SessionRegex = regexp.MustCompile(`protocol bgp DN42_(\d+)_v6 `)

type ErrorEntry struct {
	ASN    int      `json:"asn"`
	Issues []string `json:"issues"`
}

type ErrorListHandler struct {
	Cfg              *config.Config
	ListWGASNs       func() ([]int, error)
	ListBirdASNs     func() ([]int, error)
	GetHandshake     func(ctx context.Context, iface string) (int64, error)
	GetProtocolStatus func(ctx context.Context, session, ctlPath string) (*model.BirdSessionStatus, error)
	ReadBirdConfig   func(asn int) (string, error)
	Now              func() time.Time
}

func (h *ErrorListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	wgSet := make(map[int]bool, len(wgASNs))
	for _, asn := range wgASNs {
		wgSet[asn] = true
	}
	birdSet := make(map[int]bool, len(birdASNs))
	for _, asn := range birdASNs {
		birdSet[asn] = true
	}

	allASNSet := make(map[int]bool)
	for _, asn := range wgASNs {
		allASNSet[asn] = true
	}
	for _, asn := range birdASNs {
		allASNSet[asn] = true
	}
	allASNs := make([]int, 0, len(allASNSet))
	for asn := range allASNSet {
		allASNs = append(allASNs, asn)
	}
	sort.Ints(allASNs)

	now := h.Now().Unix()
	var errs []ErrorEntry

	for _, asn := range allASNs {
		var issues []string

		if wgSet[asn] && !birdSet[asn] {
			issues = append(issues, "WireGuard config exists but BIRD config missing")
		} else if !wgSet[asn] && birdSet[asn] {
			issues = append(issues, "BIRD config exists but WireGuard config missing")
		}

		if wgSet[asn] {
			iface := fmt.Sprintf("dn42-%d", asn)
			handshakeTS, handshakeErr := h.GetHandshake(r.Context(), iface)
			if handshakeErr != nil {
				issues = append(issues, "WireGuard status check failed")
			} else if handshakeTS == 0 {
				issues = append(issues, "WireGuard never handshaked")
			} else if now-handshakeTS > wgHandshakeStaleSeconds {
				issues = append(issues, "WireGuard handshake stale")
			}
		}

		if birdSet[asn] {
			sessions, readErr := h.findBirdSessions(asn)
			if readErr != nil {
				issues = append(issues, "BIRD config unreadable")
			}
			for _, suffix := range sessions {
				session := fmt.Sprintf("DN42_%d_%s", asn, suffix)
				status, statusErr := h.GetProtocolStatus(r.Context(), session, h.Cfg.BirdCtlPath)
				if statusErr != nil {
					issues = append(issues, fmt.Sprintf("BIRD %s check failed", session))
					continue
				}
				if status.State != "Established" {
					issues = append(issues, fmt.Sprintf("BIRD %s state: %s", session, status.State))
				}
			}
		}

		if len(issues) > 0 {
			errs = append(errs, ErrorEntry{ASN: asn, Issues: issues})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(errs)
}

func (h *ErrorListHandler) findBirdSessions(asn int) ([]string, error) {
	if h.ReadBirdConfig == nil {
		return nil, fmt.Errorf("ReadBirdConfig not configured")
	}
	content, err := h.ReadBirdConfig(asn)
	if err != nil {
		return nil, err
	}
	var sessions []string
	if birdV4SessionRegex.MatchString(content) {
		sessions = append(sessions, "v4")
	}
	if birdV6SessionRegex.MatchString(content) {
		sessions = append(sessions, "v6")
	}
	return sessions, nil
}
