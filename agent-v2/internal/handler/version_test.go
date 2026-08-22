package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionReturns200PlainText(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/version", nil)
	rec := httptest.NewRecorder()

	VersionHandler()(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("expected Content-Type text/plain, got %s", ct)
	}
	if body := rec.Body.String(); body != "32\n" {
		t.Errorf("expected body %q, got %q", "32\n", body)
	}
}

func TestVersionDoesNotRequireAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/version", nil)
	rec := httptest.NewRecorder()

	VersionHandler()(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 without auth, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "32\n" {
		t.Errorf("expected body %q, got %q", "32\n", body)
	}
}
