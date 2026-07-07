package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/service"
)

type fakeUpdateService struct {
	checkChannel   string
	installChannel string
	force          bool
	restarted      bool
	status         *service.UpdateStatus
	err            error
}

func (s *fakeUpdateService) Check(ctx context.Context, channelOverride string) (*service.UpdateStatus, error) {
	s.checkChannel = channelOverride
	if s.err != nil {
		return nil, s.err
	}
	return s.status, nil
}

func (s *fakeUpdateService) Install(ctx context.Context, channelOverride string, force bool) (*service.UpdateStatus, error) {
	s.installChannel = channelOverride
	s.force = force
	if s.err != nil {
		return nil, s.err
	}
	return s.status, nil
}

func (s *fakeUpdateService) Restart(ctx context.Context) error {
	s.restarted = true
	return nil
}

func TestUpdateCheckHandler(t *testing.T) {
	t.Parallel()
	updater := &fakeUpdateService{
		status: &service.UpdateStatus{
			CurrentVersion:  "v2.0.0-alpha.3",
			LatestVersion:   "v2.0.0-alpha.4",
			UpdateAvailable: true,
			Channel:         "candidate",
		},
	}
	handler := UpdateCheckHandler(updater)

	req := httptest.NewRequest(http.MethodPost, "/update/check", strings.NewReader(`{"channel":"candidate"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if updater.checkChannel != "candidate" {
		t.Fatalf("check channel = %q, want candidate", updater.checkChannel)
	}
	var status service.UpdateStatus
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !status.UpdateAvailable || status.LatestVersion != "v2.0.0-alpha.4" {
		t.Fatalf("status = %+v", status)
	}
}

func TestUpdateApplyHandlerNoUpdate(t *testing.T) {
	t.Parallel()
	updater := &fakeUpdateService{
		status: &service.UpdateStatus{
			CurrentVersion:  "v2.0.0-alpha.4",
			LatestVersion:   "v2.0.0-alpha.4",
			UpdateAvailable: false,
			Channel:         "candidate",
		},
	}
	handler := UpdateApplyHandler(updater)

	req := httptest.NewRequest(http.MethodPost, "/update/apply", strings.NewReader(`{"channel":"stable","force":true}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if updater.installChannel != "stable" || !updater.force {
		t.Fatalf("install channel=%q force=%v", updater.installChannel, updater.force)
	}
	if updater.restarted {
		t.Fatal("Restart() should not be called when no restart is required")
	}
}

func TestUpdateApplyHandlerRestartRequired(t *testing.T) {
	t.Parallel()
	updater := &fakeUpdateService{
		status: &service.UpdateStatus{
			CurrentVersion:  "v2.0.0-alpha.3",
			LatestVersion:   "v2.0.0-alpha.4",
			UpdateAvailable: true,
			Installed:       true,
			RestartRequired: true,
			Channel:         "candidate",
		},
	}
	handler := UpdateApplyHandler(updater)

	req := httptest.NewRequest(http.MethodPost, "/update/apply", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
}
