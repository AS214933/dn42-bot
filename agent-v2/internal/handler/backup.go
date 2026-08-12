package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/service"
)

type BackupService interface {
	Status() service.BackupStatus
	Install(ctx context.Context, req service.BackupInstallRequest) (service.BackupInstallResult, error)
	Sync(ctx context.Context) (service.BackupSyncResult, error)
}

func BackupStatusHandler(backup BackupService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(backup.Status())
	}
}

func BackupInstallHandler(backup BackupService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeBackupInstallRequest(w, r)
		if !ok {
			return
		}

		result, err := backup.Install(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}
}

func BackupSyncHandler(backup BackupService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := backup.Sync(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}
}

func decodeBackupInstallRequest(w http.ResponseWriter, r *http.Request) (service.BackupInstallRequest, bool) {
	var req service.BackupInstallRequest
	if r.Body == nil {
		return req, true
	}
	defer r.Body.Close()
	if r.ContentLength == 0 {
		return req, true
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return service.BackupInstallRequest{}, false
	}
	return req, true
}
