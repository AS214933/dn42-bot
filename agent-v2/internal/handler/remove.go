package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

type RunFunc func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error)

type RemoveHandler struct {
	cfg    *config.Config
	runCmd RunFunc
	remove func(name string) error
}

func NewRemoveHandler(cfg *config.Config, runCmd RunFunc, remove func(name string) error) *RemoveHandler {
	return &RemoveHandler{cfg: cfg, runCmd: runCmd, remove: remove}
}

func (h *RemoveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	h.remove(fmt.Sprintf("/etc/wireguard/%s.conf", iface))
	h.remove(fmt.Sprintf("/etc/bird/dn42_peers/%d.conf", asn))

	h.runCmd(ctx, "birdc", []string{"-s", h.cfg.BirdCtlPath, "c"}, 10*time.Second)

	if h.cfg.VnstatAutoRemove {
		h.runCmd(ctx, "vnstat", []string{"--remove", "-i", iface, "--force"}, 10*time.Second)
	}

	w.WriteHeader(http.StatusOK)
}

func readBody(r *http.Request) (string, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil {
		return "", err
	}
	if len(body) == 0 {
		return "", fmt.Errorf("empty body")
	}
	if len(body) > 1024 {
		return "", fmt.Errorf("body too large")
	}
	return string(body), nil
}
