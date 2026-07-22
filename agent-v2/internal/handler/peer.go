package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/birdctl"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/service"
)

type CmdRunner func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error)

type PeerHandler struct {
	Cfg         *config.Config
	WGConfDir   string
	BirdConfDir string
	RunCmd      CmdRunner
	// BirdQuery runs one BIRD control command. Defaults to birdctl.Query.
	BirdQuery   func(ctx context.Context, command string) (string, error)
}

func (h *PeerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var peer model.PeerInfo
	if err := json.NewDecoder(r.Body).Decode(&peer); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	wgCount, birdCount, err := h.getPeerNum()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if wgCount != birdCount {
		http.Error(w, fmt.Sprintf("wireguard and bird config count mismatch: wg=%d bird=%d", wgCount, birdCount), http.StatusInternalServerError)
		return
	}

	wgExists := fileExists(filepath.Join(h.WGConfDir, fmt.Sprintf("dn42-%d.conf", peer.ASN)))
	birdExists := fileExists(filepath.Join(h.BirdConfDir, fmt.Sprintf("%d.conf", peer.ASN)))

	if !wgExists && !birdExists {
		if (h.Cfg.MaxPeers != 0 && wgCount >= h.Cfg.MaxPeers) || !h.Cfg.Open {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
	}

	mtu := peer.MTU
	if mtu == 0 {
		mtu = h.Cfg.DefaultMTU
	}
	if mtu == 0 {
		mtu = 1420
	}
	peer.MTU = mtu

	wgConfig := service.GenerateConfig(peer, h.Cfg)
	wgPath := filepath.Join(h.WGConfDir, fmt.Sprintf("dn42-%d.conf", peer.ASN))
	if err := os.WriteFile(wgPath, []byte(wgConfig), 0644); err != nil {
		http.Error(w, "Failed to write WG config", http.StatusInternalServerError)
		return
	}

	birdConfig := h.generateBirdConfig(peer)
	birdPath := filepath.Join(h.BirdConfDir, fmt.Sprintf("%d.conf", peer.ASN))
	if err := os.WriteFile(birdPath, []byte(birdConfig), 0644); err != nil {
		http.Error(w, "Failed to write BIRD config", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	iface := fmt.Sprintf("dn42-%d", peer.ASN)

	output, _ := h.RunCmd(ctx, "wg-quick", []string{"up", iface}, 10*time.Second)
	_ = output

	birdQuery := h.BirdQuery
	if birdQuery == nil {
		birdQuery = func(ctx context.Context, command string) (string, error) {
			qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return birdctl.Query(qctx, h.Cfg.BirdCtlPath, command)
		}
	}
	birdQuery(ctx, "configure")

	if h.Cfg.VnstatAutoAdd {
		h.RunCmd(ctx, "vnstat", []string{"--add", "-i", iface}, 10*time.Second)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *PeerHandler) getPeerNum() (int, int, error) {
	wgEntries, err := os.ReadDir(h.WGConfDir)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read WG config dir: %w", err)
	}
	wgCount := 0
	for _, e := range wgEntries {
		name := e.Name()
		if strings.HasPrefix(name, "dn42-") && strings.HasSuffix(name, ".conf") {
			numStr := name[5 : len(name)-5]
			if _, err := strconv.Atoi(numStr); err == nil {
				wgCount++
			}
		}
	}

	birdEntries, err := os.ReadDir(h.BirdConfDir)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read BIRD config dir: %w", err)
	}
	birdCount := 0
	for _, e := range birdEntries {
		name := e.Name()
		if strings.HasSuffix(name, ".conf") {
			numStr := name[:len(name)-5]
			if _, err := strconv.Atoi(numStr); err == nil {
				birdCount++
			}
		}
	}

	return wgCount, birdCount, nil
}

func (h *PeerHandler) generateBirdConfig(peer model.PeerInfo) string {
	var sb strings.Builder

	switch peer.Channel {
	case "IPv6 only":
		sb.WriteString(service.GenerateProtocol(6, true, peer))
	case "IPv4 only":
		sb.WriteString(service.GenerateProtocol(4, true, peer))
	case "IPv6 & IPv4":
		switch peer.MPBGP {
		case "IPv6":
			sb.WriteString(service.GenerateProtocol(6, false, peer))
		case "IPv4":
			sb.WriteString(service.GenerateProtocol(4, false, peer))
		case "Not supported":
			sb.WriteString(service.GenerateProtocol(6, true, peer))
			sb.WriteString("\n")
			sb.WriteString(service.GenerateProtocol(4, true, peer))
		}
	}

	return sb.String()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
