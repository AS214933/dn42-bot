package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
)

func TestCountConfFilesFiltersPrefixAndNumericName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := []string{
		"dn42-4242421234.conf",
		"dn42-4242421816.conf",
		"dn42-not-a-number.conf",
		"wg0.conf",
		"README.txt",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	got, err := countConfFiles(dir, "dn42-")
	if err != nil {
		t.Fatalf("countConfFiles returned error: %v", err)
	}
	if got != 2 {
		t.Fatalf("countConfFiles = %d, want 2", got)
	}
}

func TestCountPeersIgnoresNonPeerConfigFiles(t *testing.T) {
	t.Parallel()

	wgDir := t.TempDir()
	birdDir := t.TempDir()
	for _, path := range []string{
		filepath.Join(wgDir, "dn42-4242421234.conf"),
		filepath.Join(wgDir, "wg0.conf"),
		filepath.Join(birdDir, "4242421234.conf"),
		filepath.Join(birdDir, "bird.conf"),
	} {
		if err := os.WriteFile(path, []byte{}, 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	wgCount, birdCount, err := countPeers(wgDir, birdDir)
	if err != nil {
		t.Fatalf("countPeers returned error: %v", err)
	}
	if wgCount != 1 || birdCount != 1 {
		t.Fatalf("countPeers = (%d, %d), want (1, 1)", wgCount, birdCount)
	}
}

func TestRegisterLookingGlassRoutesDisabled(t *testing.T) {
	t.Parallel()
	router := chi.NewRouter()
	cfg := &config.Config{}
	if err := registerLookingGlassRoutes(router, cfg, nil); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/traceroute?q=1.1.1.1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRegisterLookingGlassRoutesAreExact(t *testing.T) {
	t.Parallel()
	router := chi.NewRouter()
	cfg := &config.Config{
		BirdCtlPath: "/nonexistent/bird.ctl",
		LookingGlass: config.LookingGlassConfig{
			Enabled:                 true,
			AllowedCIDRs:            []string{"any"},
			BirdMaxConcurrent:       16,
			TracerouteMaxConcurrent: 10,
			RequestTimeout:          time.Second,
			MaxQueryLength:          4096,
			MaxOutputBytes:          65536,
		},
	}
	if err := registerLookingGlassRoutes(router, cfg, nil); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/bird/extra?q=show+protocols", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for non-exact path", rec.Code)
	}
}

func TestRegisterLookingGlassRoutesOutsideAuth(t *testing.T) {
	t.Parallel()
	router := chi.NewRouter()
	cfg := &config.Config{
		BirdCtlPath: "/nonexistent/bird.ctl",
		LookingGlass: config.LookingGlassConfig{
			Enabled:                 true,
			AllowedCIDRs:            []string{"any"},
			BirdMaxConcurrent:       16,
			TracerouteMaxConcurrent: 10,
			RequestTimeout:          time.Second,
			MaxQueryLength:          4096,
			MaxOutputBytes:          65536,
		},
	}
	if err := registerLookingGlassRoutes(router, cfg, nil); err != nil {
		t.Fatal(err)
	}
	queries := map[string]string{
		"/bird":        "show+protocols",
		"/bird6":       "show+protocols",
		"/traceroute":  "1.1.1.1",
		"/traceroute6": "1.1.1.1",
	}
	for path, query := range queries {
		req := httptest.NewRequest(http.MethodGet, path+"?q="+query, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound || rec.Code == http.StatusForbidden {
			t.Errorf("%s status = %d, route should be registered without token auth", path, rec.Code)
		}
	}
}

func TestRegisterLookingGlassRoutesAllowedEmptyDeniesAll(t *testing.T) {
	t.Parallel()
	router := chi.NewRouter()
	cfg := &config.Config{
		BirdCtlPath: "/nonexistent/bird.ctl",
		LookingGlass: config.LookingGlassConfig{
			Enabled:                 true,
			BirdMaxConcurrent:       16,
			TracerouteMaxConcurrent: 10,
			RequestTimeout:          time.Second,
			MaxQueryLength:          4096,
			MaxOutputBytes:          65536,
		},
	}
	if err := registerLookingGlassRoutes(router, cfg, nil); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/traceroute?q=1.1.1.1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestNewHTTPServerTimeouts(t *testing.T) {
	t.Parallel()
	srv := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 5*time.Second || srv.ReadTimeout != 15*time.Second || srv.WriteTimeout != 310*time.Second || srv.IdleTimeout != 60*time.Second {
		t.Fatalf("unexpected HTTP timeouts: %+v", srv)
	}
	if srv.WriteTimeout <= 5*time.Minute {
		t.Fatalf("WriteTimeout = %v, must exceed the remote update request timeout", srv.WriteTimeout)
	}
	if srv.MaxHeaderBytes != 64*1024 {
		t.Fatalf("MaxHeaderBytes = %d", srv.MaxHeaderBytes)
	}
}
