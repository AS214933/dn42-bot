package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/middleware"
)

func routeTestCfg() *config.Config {
	return &config.Config{
		Secret:              testSecret,
		DefaultMTU:          1420,
		Open:                true,
		MaxPeers:            10,
		MinPeerRequirement:  1,
		ExtraMsg:            "hello",
		BirdCtlPath:         "/var/run/bird/bird.ctl",
		BirdTable4:          "master4",
		BirdTable6:          "master6",
		NetSupport: config.NetSupport{
			IPv4:    true,
			IPv6:    true,
			IPv4NAT: false,
			CN:      true,
		},
	}
}

func mockRunner(output string, err error) BirdcRunner {
	return func(_ context.Context, _, _, _ string) (string, error) {
		return output, err
	}
}

func recordingRunner(output string) (*BirdcRunner, *string, *string) {
	var capturedTable, capturedTarget string
	runner := BirdcRunner(func(_ context.Context, _, table, target string) (string, error) {
		capturedTable = table
		capturedTarget = target
		return output, nil
	})
	return &runner, &capturedTable, &capturedTarget
}

func TestRouteIPv4(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	runner, table, target := recordingRunner("route output")
	handler := middleware.AuthMiddleware(cfg.Secret)(RouteHandler(cfg, *runner))

	req := httptest.NewRequest(http.MethodPost, "/route", strings.NewReader("172.20.0.1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "route output" {
		t.Errorf("expected body %q, got %q", "route output", rec.Body.String())
	}
	if *table != "master4" {
		t.Errorf("expected table master4, got %s", *table)
	}
	if *target != "172.20.0.1" {
		t.Errorf("expected target 172.20.0.1, got %s", *target)
	}
}

func TestRouteIPv6(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	runner, table, _ := recordingRunner("route v6 output")
	handler := middleware.AuthMiddleware(cfg.Secret)(RouteHandler(cfg, *runner))

	req := httptest.NewRequest(http.MethodPost, "/route", strings.NewReader("fd42:d42:d42::1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if *table != "master6" {
		t.Errorf("expected table master6, got %s", *table)
	}
}

func TestRouteRequiresAuth(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(RouteHandler(cfg, mockRunner("output", nil)))

	req := httptest.NewRequest(http.MethodPost, "/route", strings.NewReader("172.20.0.1"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestRoutePlainTextContent(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(RouteHandler(cfg, mockRunner("some route", nil)))

	req := httptest.NewRequest(http.MethodPost, "/route", strings.NewReader("172.20.0.1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("expected Content-Type text/plain, got %s", ct)
	}
}

func TestRouteEmptyBody(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(RouteHandler(cfg, mockRunner("", nil)))

	req := httptest.NewRequest(http.MethodPost, "/route", strings.NewReader(""))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty target, got %d", rec.Code)
	}
}

func TestRouteEmptyBodyNil(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(RouteHandler(cfg, mockRunner("", nil)))

	req := httptest.NewRequest(http.MethodPost, "/route", nil)
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for nil body, got %d", rec.Code)
	}
}

func TestPathIPv4(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	runner, table, target := recordingRunner("172.20.0.0/24 via 172.20.0.1 ... BGP.as_path: 4242421234 4242425678")
	handler := middleware.AuthMiddleware(cfg.Secret)(PathHandler(cfg, *runner))

	req := httptest.NewRequest(http.MethodPost, "/path", strings.NewReader("172.20.0.1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if *table != "master4" {
		t.Errorf("expected table master4, got %s", *table)
	}
	if *target != "172.20.0.1" {
		t.Errorf("expected target 172.20.0.1, got %s", *target)
	}
}

func TestPathIPv6(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	runner, table, _ := recordingRunner("BGP.as_path: 4242421234")
	handler := middleware.AuthMiddleware(cfg.Secret)(PathHandler(cfg, *runner))

	req := httptest.NewRequest(http.MethodPost, "/path", strings.NewReader("fd42:d42:d42::1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if *table != "master6" {
		t.Errorf("expected table master6, got %s", *table)
	}
}

func TestPathExtractsASPath(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	birdcOutput := "172.20.0.0/24 unicast [DN42_4242421234_v4 12:34:56] * (100)\n" +
		"\tType: BGP unicast univ\n" +
		"\tBGP.origin: IGP\n" +
		"\tBGP.as_path: 4242421234 4242425678\n" +
		"\tBGP.next_hop: 172.20.0.1\n"
	handler := middleware.AuthMiddleware(cfg.Secret)(PathHandler(cfg, mockRunner(birdcOutput, nil)))

	req := httptest.NewRequest(http.MethodPost, "/path", strings.NewReader("172.20.0.1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	expected := "4242421234 4242425678"
	if rec.Body.String() != expected {
		t.Errorf("expected body %q, got %q", expected, rec.Body.String())
	}
}

func TestPathASPathPlainText(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	birdcOutput := "\tBGP.as_path: 64512 64513\n"
	handler := middleware.AuthMiddleware(cfg.Secret)(PathHandler(cfg, mockRunner(birdcOutput, nil)))

	req := httptest.NewRequest(http.MethodPost, "/path", strings.NewReader("10.0.0.1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("expected Content-Type text/plain, got %s", ct)
	}
	if rec.Body.String() != "64512 64513" {
		t.Errorf("expected body %q, got %q", "64512 64513", rec.Body.String())
	}
}

func TestPathNotFound(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	birdcOutput := "172.20.0.0/24 unicast [DN42_4242421234_v4 12:34:56] * (100)\n" +
		"\tType: BGP unicast univ\n" +
		"\tBGP.origin: IGP\n"
	handler := middleware.AuthMiddleware(cfg.Secret)(PathHandler(cfg, mockRunner(birdcOutput, nil)))

	req := httptest.NewRequest(http.MethodPost, "/path", strings.NewReader("172.20.0.1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestPathRequiresAuth(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(PathHandler(cfg, mockRunner("output", nil)))

	req := httptest.NewRequest(http.MethodPost, "/path", strings.NewReader("172.20.0.1"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestPathEmptyBody(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(PathHandler(cfg, mockRunner("", nil)))

	req := httptest.NewRequest(http.MethodPost, "/path", strings.NewReader(""))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty target, got %d", rec.Code)
	}
}

func TestRouteBirdcNonZeroWithOutput(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	output := "BIRD 2.0.12 ready.\nNetwork not in table\n"
	handler := middleware.AuthMiddleware(cfg.Secret)(RouteHandler(cfg, mockRunner(output, errors.New("exit status 1"))))

	req := httptest.NewRequest(http.MethodPost, "/route", strings.NewReader("127.0.0.1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for birdc non-zero with output, got %d body %q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != output {
		t.Errorf("expected body %q, got %q", output, rec.Body.String())
	}
}

func TestRouteBirdcFailureEmptyOutput(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(RouteHandler(cfg, mockRunner("", errors.New("exit status 1"))))

	req := httptest.NewRequest(http.MethodPost, "/route", strings.NewReader("127.0.0.1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for empty birdc failure, got %d", rec.Code)
	}
}

func TestPathBirdcNonZeroWithoutASPath(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	output := "BIRD 2.0.12 ready.\nNetwork not in table\n"
	handler := middleware.AuthMiddleware(cfg.Secret)(PathHandler(cfg, mockRunner(output, errors.New("exit status 1"))))

	req := httptest.NewRequest(http.MethodPost, "/path", strings.NewReader("127.0.0.1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when birdc non-zero has no AS path, got %d body %q", rec.Code, rec.Body.String())
	}
}

func TestPathBirdcFailureEmptyOutput(t *testing.T) {
	t.Parallel()
	cfg := routeTestCfg()
	handler := middleware.AuthMiddleware(cfg.Secret)(PathHandler(cfg, mockRunner("", errors.New("exit status 1"))))

	req := httptest.NewRequest(http.MethodPost, "/path", strings.NewReader("127.0.0.1"))
	req.Header.Set("X-DN42-Bot-Api-Secret-Token", testSecret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for empty birdc failure, got %d", rec.Code)
	}
}
