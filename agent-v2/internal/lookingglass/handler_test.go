package lookingglass

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestHandler(t *testing.T, mutate func(*Config), runner TraceRunner) *Handler {
	t.Helper()
	cfg := Config{
		BirdSocket:     "/tmp/not-used.ctl",
		AllowedSources: []string{"any"},
		RequestTimeout: time.Second,
		MaxQueryLength: 128,
		MaxOutputBytes: 1024,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	handler, err := NewHandler(cfg, runner)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func request(handler http.Handler, method, path, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestNewHandlerValidatesConfig(t *testing.T) {
	t.Parallel()
	if _, err := NewHandler(Config{AllowedSources: []string{"any"}}, nil); err == nil {
		t.Fatal("expected missing BIRD socket error")
	}
	if _, err := NewHandler(Config{BirdSocket: "/tmp/bird.ctl", AllowedSources: []string{"invalid"}}, nil); err == nil {
		t.Fatal("expected invalid ACL error")
	}
}

func TestHandlerRejectsNonGET(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, nil, nil)
	rec := request(handler, http.MethodPost, "/bird?q=show+protocols", "8.8.8.8:1234")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status = %d, Allow = %q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestHandlerACL(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, func(cfg *Config) {
		cfg.AllowedSources = []string{"any"}
		cfg.DisallowedSources = []string{"private"}
	}, nil)
	if rec := request(handler, http.MethodGet, "/bird?q=show+protocols", "10.0.0.1:1234"); rec.Code != http.StatusForbidden {
		t.Fatalf("private source status = %d, want 403", rec.Code)
	}
	if rec := request(handler, http.MethodGet, "/traceroute?q=1.1.1.1", "not-an-address"); rec.Code != http.StatusForbidden {
		t.Fatalf("malformed source status = %d, want 403", rec.Code)
	}
}

func TestHandlerAllowedEmptyIsDenyAll(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, func(cfg *Config) { cfg.AllowedSources = nil }, nil)
	rec := request(handler, http.MethodGet, "/bird?q=show+protocols", "8.8.8.8:1234")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestHandlerRejectsInvalidQueries(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, func(cfg *Config) { cfg.MaxQueryLength = 8 }, nil)
	tests := []struct {
		name string
		path string
		code int
	}{
		{name: "empty", path: "/bird", code: http.StatusBadRequest},
		{name: "too long", path: "/bird?q=show+protocols", code: http.StatusRequestURITooLong},
		{name: "newline", path: "/bird?q=" + url.QueryEscape("show route\nconfigure"), code: http.StatusRequestURITooLong},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := request(handler, http.MethodGet, test.path, "8.8.8.8:1234")
			if rec.Code != test.code {
				t.Fatalf("status = %d, want %d", rec.Code, test.code)
			}
		})
	}

	controlHandler := newTestHandler(t, nil, nil)
	rec := request(controlHandler, http.MethodGet, "/bird?q="+url.QueryEscape("show route\r\nconfigure"), "8.8.8.8:1234")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("control character status = %d, want 400", rec.Code)
	}
}

func TestHandlerOnlyAllowsReadOnlyBirdCommands(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, nil, nil)
	for _, query := range []string{"configure", "show status", "show protocolsx", "Show Protocols"} {
		rec := request(handler, http.MethodGet, "/bird?q="+url.QueryEscape(query), "8.8.8.8:1234")
		if rec.Code != http.StatusForbidden {
			t.Errorf("query %q status = %d, want 403", query, rec.Code)
		}
	}
	for _, query := range []string{"show protocols", "show protocols all", "show route", "show route for 1.1.1.1 all"} {
		if !isAllowedBirdCommand(query) {
			t.Errorf("expected %q to be allowed", query)
		}
	}
}

func TestHandlerBirdCompatibilityPaths(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/bird", "/bird6"} {
		t.Run(path, func(t *testing.T) {
			server := startMockBirdServer(t, "0016 Access restricted\n", "1000 route output\n")
			handler := newTestHandler(t, func(cfg *Config) { cfg.BirdSocket = server.socket }, nil)
			rec := request(handler, http.MethodGet, path+"?q="+url.QueryEscape("show route"), "8.8.8.8:1234")
			if rec.Code != http.StatusOK || rec.Body.String() != "route output\n" {
				t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandlerReturnsBirdErrorBody(t *testing.T) {
	t.Parallel()
	server := startMockBirdServer(t, "0016 Access restricted\n", "9001 protocol not found\n")
	handler := newTestHandler(t, func(cfg *Config) { cfg.BirdSocket = server.socket }, nil)
	rec := request(handler, http.MethodGet, "/bird?q="+url.QueryEscape("show protocols missing"), "8.8.8.8:1234")
	if rec.Code != http.StatusOK || rec.Body.String() != "protocol not found\n" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
}

func TestHandlerTracerouteCompatibilityPaths(t *testing.T) {
	t.Parallel()
	type call struct {
		target string
		family int
	}
	calls := make(chan call, 2)
	runner := func(_ context.Context, target string, family int) (string, error) {
		calls <- call{target: target, family: family}
		return "trace output", nil
	}
	handler := newTestHandler(t, nil, runner)

	for _, test := range []struct {
		path   string
		family int
	}{
		{path: "/traceroute", family: 0},
		{path: "/traceroute6", family: 0},
	} {
		rec := request(handler, http.MethodGet, test.path+"?q=example.com", "[2001:4860:4860::8888]:1234")
		if rec.Code != http.StatusOK || rec.Body.String() != "trace output" {
			t.Fatalf("%s: status = %d, body = %q", test.path, rec.Code, rec.Body.String())
		}
		got := <-calls
		if got.target != "example.com" || got.family != test.family {
			t.Fatalf("%s: runner call = %+v", test.path, got)
		}
	}
}

func TestHandlerTracerouteValidationAndOutputLimit(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	runner := func(_ context.Context, _ string, _ int) (string, error) {
		calls.Add(1)
		return strings.Repeat("x", 100), nil
	}
	handler := newTestHandler(t, func(cfg *Config) { cfg.MaxOutputBytes = 10 }, runner)
	if rec := request(handler, http.MethodGet, "/traceroute?q=--help", "8.8.8.8:1234"); rec.Code != http.StatusBadRequest {
		t.Fatalf("dash target status = %d, want 400", rec.Code)
	}
	rec := request(handler, http.MethodGet, "/traceroute?q=1.1.1.1", "8.8.8.8:1234")
	if rec.Code != http.StatusOK || len(rec.Body.String()) != 10 {
		t.Fatalf("status = %d, output length = %d", rec.Code, len(rec.Body.String()))
	}
	if calls.Load() != 1 {
		t.Fatalf("runner calls = %d, want 1", calls.Load())
	}
}

func TestHandlerTracerouteConcurrencyLimit(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	releaseRunner := make(chan struct{})
	runner := func(ctx context.Context, _ string, _ int) (string, error) {
		close(started)
		select {
		case <-releaseRunner:
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	handler := newTestHandler(t, func(cfg *Config) { cfg.TracerouteMaxConcurrent = 1 }, runner)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		firstDone <- request(handler, http.MethodGet, "/traceroute?q=1.1.1.1", "8.8.8.8:1234")
	}()
	<-started

	second := request(handler, http.MethodGet, "/traceroute?q=1.0.0.1", "8.8.8.8:1234")
	if second.Code != http.StatusServiceUnavailable {
		t.Fatalf("second status = %d, want 503", second.Code)
	}
	close(releaseRunner)
	if first := <-firstDone; first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", first.Code)
	}
}

func TestHandlerTracerouteTimeout(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, _ string, _ int) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	handler := newTestHandler(t, func(cfg *Config) { cfg.RequestTimeout = 10 * time.Millisecond }, runner)
	rec := request(handler, http.MethodGet, "/traceroute?q=1.1.1.1", "8.8.8.8:1234")
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rec.Code)
	}
}

func TestHandlerTracerouteError(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, nil, func(context.Context, string, int) (string, error) {
		return "", errors.New("failed")
	})
	rec := request(handler, http.MethodGet, "/traceroute?q=1.1.1.1", "8.8.8.8:1234")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandlerUnknownPath(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, nil, nil)
	rec := request(handler, http.MethodGet, "/unknown", "8.8.8.8:1234")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
