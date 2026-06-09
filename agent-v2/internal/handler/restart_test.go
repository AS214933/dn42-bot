package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

func TestRestartHandler_Forbidden(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "correct-secret"}
	h := NewRestartHandler(cfg, noopRun)

	req := httptest.NewRequest(http.MethodPost, "/restart", strings.NewReader("4242421234"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "wrong-secret")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestRestartHandler_MissingAuth(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "correct-secret"}
	h := NewRestartHandler(cfg, noopRun)

	req := httptest.NewRequest(http.MethodPost, "/restart", strings.NewReader("4242421234"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestRestartHandler_EmptyBody(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "s"}
	h := NewRestartHandler(cfg, noopRun)

	req := httptest.NewRequest(http.MethodPost, "/restart", strings.NewReader(""))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestRestartHandler_InvalidASN(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "s"}
	h := NewRestartHandler(cfg, noopRun)

	req := httptest.NewRequest(http.MethodPost, "/restart", strings.NewReader("abc"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestRestartHandler_Success(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "s", BirdCtlPath: "/run/bird.ctl"}

	var cmds []string
	mockRun := func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		cmds = append(cmds, name+" "+strings.Join(args, " "))
		return "", nil
	}

	h := NewRestartHandler(cfg, mockRun)
	req := httptest.NewRequest(http.MethodPost, "/restart", strings.NewReader("4242421234"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	assertContains(t, cmds, "wg-quick down dn42-4242421234")
	assertContains(t, cmds, "wg-quick up dn42-4242421234")
	assertContains(t, cmds, "birdc -s /run/bird.ctl restart DN42_4242421234_v4")
	assertContains(t, cmds, "birdc -s /run/bird.ctl restart DN42_4242421234_v6")
}

func TestRestartHandler_NotFound_BirdSyntaxAndWGError(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "s", BirdCtlPath: "/run/bird.ctl"}

	mockRun := func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		if name == "wg-quick" && len(args) > 0 && args[0] == "up" {
			return "RTNETLINK answers: No such device\nip link delete dev dn42-4242421234\n", nil
		}
		if name == "birdc" {
			return "syntax error", nil
		}
		return "", nil
	}

	h := NewRestartHandler(cfg, mockRun)
	req := httptest.NewRequest(http.MethodPost, "/restart", strings.NewReader("4242421234"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d, body: %q", rec.Code, rec.Body.String())
	}
}

func TestRestartHandler_BirdError(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "s", BirdCtlPath: "/run/bird.ctl"}

	mockRun := func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		if name == "birdc" {
			return "syntax error", nil
		}
		return "", nil
	}

	h := NewRestartHandler(cfg, mockRun)
	req := httptest.NewRequest(http.MethodPost, "/restart", strings.NewReader("4242421234"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bird error") {
		t.Errorf("expected body containing 'bird error', got %q", rec.Body.String())
	}
}

func TestRestartHandler_WGError(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "s", BirdCtlPath: "/run/bird.ctl"}

	mockRun := func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		if name == "wg-quick" && len(args) > 0 && args[0] == "up" {
			return "ip link delete dev dn42-4242421234\n", nil
		}
		if name == "birdc" {
			return "ok", nil
		}
		return "", nil
	}

	h := NewRestartHandler(cfg, mockRun)
	req := httptest.NewRequest(http.MethodPost, "/restart", strings.NewReader("4242421234"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "wg error") {
		t.Errorf("expected body containing 'wg error', got %q", rec.Body.String())
	}
}

func TestRestartHandler_WGDownErrorIgnored(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "s", BirdCtlPath: "/run/bird.ctl"}

	var cmds []string
	mockRun := func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		cmds = append(cmds, name+" "+strings.Join(args, " "))
		if name == "wg-quick" && len(args) > 0 && args[0] == "down" {
			return "", fmt.Errorf("interface not found")
		}
		return "", nil
	}

	h := NewRestartHandler(cfg, mockRun)
	req := httptest.NewRequest(http.MethodPost, "/restart", strings.NewReader("4242421234"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	assertContains(t, cmds, "wg-quick up dn42-4242421234")
}

func TestRestartHandler_WhitespaceASN(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Secret: "s", BirdCtlPath: "/run/bird.ctl"}

	var cmds []string
	mockRun := func(_ context.Context, name string, args []string, _ time.Duration) (string, error) {
		cmds = append(cmds, name+" "+strings.Join(args, " "))
		return "", nil
	}

	h := NewRestartHandler(cfg, mockRun)
	req := httptest.NewRequest(http.MethodPost, "/restart", strings.NewReader("  4242421234\n"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", "s")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	assertContains(t, cmds, "wg-quick down dn42-4242421234")
}
