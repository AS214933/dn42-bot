package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

func TestRemoveHandler_Forbidden(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "correct-secret"}
	h := NewRemoveHandler(cfg, noopRun, noopRemove)

	req := httptest.NewRequest(http.MethodPost, "/remove", strings.NewReader("4242421234"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "wrong-secret")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestRemoveHandler_MissingAuth(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "correct-secret"}
	h := NewRemoveHandler(cfg, noopRun, noopRemove)

	req := httptest.NewRequest(http.MethodPost, "/remove", strings.NewReader("4242421234"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestRemoveHandler_EmptyBody(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "s"}
	h := NewRemoveHandler(cfg, noopRun, noopRemove)

	req := httptest.NewRequest(http.MethodPost, "/remove", strings.NewReader(""))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestRemoveHandler_InvalidASN(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "s"}
	h := NewRemoveHandler(cfg, noopRun, noopRemove)

	req := httptest.NewRequest(http.MethodPost, "/remove", strings.NewReader("not-a-number"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestRemoveHandler_Success(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Secret:       "s",
		BirdCtlPath:  "/run/bird.ctl",
		VnstatAutoRemove: false,
	}

	var cmds []string
	mockRun := func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		cmds = append(cmds, name+" "+strings.Join(args, " "))
		return "", nil
	}
	var removed []string
	mockRemove := func(name string) error {
		removed = append(removed, name)
		return nil
	}

	h := NewRemoveHandler(cfg, mockRun, mockRemove)
	req := httptest.NewRequest(http.MethodPost, "/remove", strings.NewReader("4242421234"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	assertContains(t, cmds, "wg-quick down dn42-4242421234")
	assertContains(t, cmds, "birdc -s /run/bird.ctl c")
	assertContains(t, removed, "/etc/wireguard/dn42-4242421234.conf")
	assertContains(t, removed, "/etc/bird/dn42_peers/4242421234.conf")

	for _, cmd := range cmds {
		if strings.Contains(cmd, "vnstat") {
			t.Error("vnstat should not be called when VnstatAutoRemove is false")
		}
	}
}

func TestRemoveHandler_VnstatAutoRemove(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Secret:           "s",
		BirdCtlPath:      "/run/bird.ctl",
		VnstatAutoRemove: true,
	}

	var cmds []string
	mockRun := func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		cmds = append(cmds, name+" "+strings.Join(args, " "))
		return "", nil
	}

	h := NewRemoveHandler(cfg, mockRun, noopRemove)
	req := httptest.NewRequest(http.MethodPost, "/remove", strings.NewReader("4242421234"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	assertContains(t, cmds, "vnstat --remove -i dn42-4242421234 --force")
}

func TestRemoveHandler_WhitespaceASN(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "s", BirdCtlPath: "/run/bird.ctl"}
	var cmds []string
	mockRun := func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		cmds = append(cmds, name+" "+strings.Join(args, " "))
		return "", nil
	}

	h := NewRemoveHandler(cfg, mockRun, noopRemove)
	req := httptest.NewRequest(http.MethodPost, "/remove", strings.NewReader("  4242421234  \n"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	assertContains(t, cmds, "wg-quick down dn42-4242421234")
}

func noopRun(_ context.Context, _ string, _ []string, _ time.Duration) (string, error) {
	return "", nil
}

func noopRemove(_ string) error { return nil }

func assertContains(t *testing.T, haystack []string, needle string) {
	t.Helper()
	for _, s := range haystack {
		if s == needle {
			return
		}
	}
	t.Errorf("expected %q in command list, got %v", needle, haystack)
}
