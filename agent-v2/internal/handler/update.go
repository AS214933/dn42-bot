package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/service"
)

type UpdateService interface {
	Check(ctx context.Context, channelOverride string) (*service.UpdateStatus, error)
	Install(ctx context.Context, channelOverride string, force bool) (*service.UpdateStatus, error)
	Restart(ctx context.Context) error
}

type updateRequest struct {
	Channel string `json:"channel"`
	Force   bool   `json:"force"`
}

func UpdateCheckHandler(updater UpdateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeUpdateRequest(w, r)
		if !ok {
			return
		}

		status, err := updater.Check(r.Context(), req.Channel)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	}
}

func UpdateApplyHandler(updater UpdateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeUpdateRequest(w, r)
		if !ok {
			return
		}

		status, err := updater.Install(r.Context(), req.Channel, req.Force)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		code := http.StatusOK
		if status.RestartRequired {
			code = http.StatusAccepted
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(status)

		if status.RestartRequired {
			go func() {
				time.Sleep(500 * time.Millisecond)
				if err := updater.Restart(context.Background()); err != nil {
					log.Printf("agent update restart failed: %v", err)
				}
			}()
		}
	}
}

func decodeUpdateRequest(w http.ResponseWriter, r *http.Request) (updateRequest, bool) {
	var req updateRequest
	if r.Body == nil {
		return req, true
	}
	defer r.Body.Close()
	if r.ContentLength == 0 {
		return req, true
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return updateRequest{}, false
	}
	return req, true
}
