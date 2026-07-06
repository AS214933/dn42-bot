package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
)

type PrePeerResponse struct {
	Existed     int              `json:"existed"`
	Max         int              `json:"max"`
	Requirement int              `json:"requirement"`
	Open        bool             `json:"open"`
	NetSupport  model.NetSupport `json:"net_support"`
	LLA         string           `json:"lla"`
	Msg         string           `json:"msg"`
}

type PrePeerHandler struct {
	Cfg        *config.Config
	GetPeerNum func() (int, int, error)
}

func (h *PrePeerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wgCount, birdCount, err := h.GetPeerNum()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if wgCount != birdCount {
		http.Error(w, fmt.Sprintf("wireguard and bird config count mismatch: wg=%d bird=%d", wgCount, birdCount), http.StatusInternalServerError)
		return
	}

	resp := PrePeerResponse{
		Existed:     wgCount,
		Max:         h.Cfg.MaxPeers,
		Requirement: h.Cfg.MinPeerRequirement,
		Open:        h.Cfg.Open,
		NetSupport: model.NetSupport{
			IPv4:    h.Cfg.NetSupport.IPv4,
			IPv6:    h.Cfg.NetSupport.IPv6,
			IPv4NAT: h.Cfg.NetSupport.IPv4NAT,
			CN:      h.Cfg.NetSupport.CN,
		},
		LLA: h.Cfg.MyDN42LinkLocalAddress.String(),
		Msg: h.Cfg.ExtraMsg,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
