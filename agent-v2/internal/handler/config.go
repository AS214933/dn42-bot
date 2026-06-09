package handler

import (
	"encoding/json"
	"net/http"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
)

var displayFields = map[string]bool{
	"DEFAULT_MTU":          true,
	"OPEN":                 true,
	"MAX_PEERS":            true,
	"MIN_PEER_REQUIREMENT": true,
	"EXTRA_MSG":            true,
	"MY_DN42_IPv4_ADDRESS": true,
	"VNSTAT_AUTO_ADD":      true,
	"VNSTAT_AUTO_REMOVE":   true,
	"BIRD_CTL_PATH":        true,
	"BIRD_TABLE_4":         true,
	"BIRD_TABLE_6":         true,
	"NET_SUPPORT":          true,
}

func ConfigGetHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Keys []string `json:"keys"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body) // ignore decode errors → empty keys
		}

		requestedKeys := body.Keys
		if len(requestedKeys) == 0 {
			requestedKeys = allDisplayFieldKeys()
		}

		result := buildConfigResponse(cfg, requestedKeys)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}
}

func allDisplayFieldKeys() []string {
	keys := make([]string, 0, len(displayFields))
	for k := range displayFields {
		keys = append(keys, k)
	}
	return keys
}

func buildConfigResponse(cfg *config.Config, keys []string) map[string]interface{} {
	result := make(map[string]interface{})
	for _, key := range keys {
		if !displayFields[key] {
			continue
		}
		switch key {
		case "DEFAULT_MTU":
			result[key] = cfg.DefaultMTU
		case "OPEN":
			result[key] = cfg.Open
		case "MAX_PEERS":
			result[key] = cfg.MaxPeers
		case "MIN_PEER_REQUIREMENT":
			result[key] = cfg.MinPeerRequirement
		case "EXTRA_MSG":
			result[key] = cfg.ExtraMsg
		case "MY_DN42_IPv4_ADDRESS":
			result[key] = cfg.MyDN42IPv4Address.String()
		case "VNSTAT_AUTO_ADD":
			result[key] = cfg.VnstatAutoAdd
		case "VNSTAT_AUTO_REMOVE":
			result[key] = cfg.VnstatAutoRemove
		case "BIRD_CTL_PATH":
			result[key] = cfg.BirdCtlPath
		case "BIRD_TABLE_4":
			result[key] = cfg.BirdTable4
		case "BIRD_TABLE_6":
			result[key] = cfg.BirdTable6
		case "NET_SUPPORT":
			result[key] = model.NetSupport{
				IPv4:    cfg.NetSupport.IPv4,
				IPv6:    cfg.NetSupport.IPv6,
				IPv4NAT: cfg.NetSupport.IPv4NAT,
				CN:      cfg.NetSupport.CN,
			}
		}
	}
	return result
}
