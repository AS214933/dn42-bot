package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/birdctl"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

type RestartHandler struct {
	cfg       *config.Config
	runCmd    RunFunc
	birdQuery func(ctx context.Context, command string) (string, error)
}

func NewRestartHandler(cfg *config.Config, runCmd RunFunc) *RestartHandler {
	return &RestartHandler{cfg: cfg, runCmd: runCmd}
}

func (h *RestartHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-DN42-Bot-Api-Secret-Token") != h.cfg.Secret {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	body, err := readBody(r)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	asn, err := strconv.Atoi(strings.TrimSpace(body))
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	iface := fmt.Sprintf("dn42-%d", asn)

	h.runCmd(ctx, "wg-quick", []string{"down", iface}, 10*time.Second)

	outWG, _ := h.runCmd(ctx, "wg-quick", []string{"up", iface}, 10*time.Second)

	session := fmt.Sprintf("DN42_%d", asn)
	birdQuery := h.birdQuery
	if birdQuery == nil {
		birdQuery = func(ctx context.Context, command string) (string, error) {
			qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return birdctl.Query(qctx, h.cfg.BirdCtlPath, command)
		}
	}
	outV4, _ := birdQuery(ctx, "restart "+session+"_v4")
	outV6, _ := birdQuery(ctx, "restart "+session+"_v6")

	wgError := strings.Contains(strings.ToLower(outWG), "ip link delete dev")
	v4SyntaxErr := strings.Contains(outV4, "syntax error")
	v6SyntaxErr := strings.Contains(outV6, "syntax error")

	if v4SyntaxErr && v6SyntaxErr {
		if wgError {
			http.Error(w, "", http.StatusNotFound)
		} else {
			http.Error(w, "bird error", http.StatusInternalServerError)
		}
		return
	}

	if wgError {
		http.Error(w, "wg error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
