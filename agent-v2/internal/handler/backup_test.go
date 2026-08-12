package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/service"
)

type fakeBackupService struct {
	status  service.BackupStatus
	install service.BackupInstallResult
	sync    service.BackupSyncResult
	err     error
}

func (f *fakeBackupService) Status() service.BackupStatus {
	return f.status
}

func (f *fakeBackupService) Install(_ context.Context, _ service.BackupInstallRequest) (service.BackupInstallResult, error) {
	return f.install, f.err
}

func (f *fakeBackupService) Sync(_ context.Context) (service.BackupSyncResult, error) {
	return f.sync, f.err
}

func TestBackupStatusHandler(t *testing.T) {
	t.Parallel()
	svc := &fakeBackupService{status: service.BackupStatus{
		Enabled:     true,
		Installed:   true,
		NodeName:    "cn01",
		GitInstance: "https://git.example.com",
		GitOrg:      "dn42-backup",
		RepoName:    "cn01",
		RepoURL:     "https://git.example.com/dn42-backup/cn01",
	}}
	req := httptest.NewRequest(http.MethodPost, "/backup/status", nil)
	rec := httptest.NewRecorder()
	BackupStatusHandler(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got service.BackupStatus
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.NodeName != "cn01" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestBackupInstallHandler(t *testing.T) {
	t.Parallel()
	svc := &fakeBackupService{install: service.BackupInstallResult{
		Installed:   true,
		NodeName:    "cn01",
		RepoName:    "cn01",
		ActionTaken: "create",
	}}
	body := strings.NewReader(`{"node_name":"CN01","git_instance":"https://git.example.com","git_org":"dn42-backup","api_token":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/backup/install", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	BackupInstallHandler(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got service.BackupInstallResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Installed || got.ActionTaken != "create" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestBackupInstallHandlerInvalidJSON(t *testing.T) {
	t.Parallel()
	svc := &fakeBackupService{}
	req := httptest.NewRequest(http.MethodPost, "/backup/install", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()
	BackupInstallHandler(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestBackupSyncHandler(t *testing.T) {
	t.Parallel()
	svc := &fakeBackupService{sync: service.BackupSyncResult{Synced: true, Pushed: true}}
	req := httptest.NewRequest(http.MethodPost, "/backup/sync", nil)
	rec := httptest.NewRecorder()
	BackupSyncHandler(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got service.BackupSyncResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Synced || !got.Pushed {
		t.Fatalf("unexpected response: %+v", got)
	}
}
