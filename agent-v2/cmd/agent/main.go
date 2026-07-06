package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/handler"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/middleware"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/service"
)

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(middleware.SentryMiddleware(cfg.SentryDSN))

	runCmd := service.RunCommand

	// /version — no auth
	r.Post("/version", handler.VersionHandler())

	// Auth-protected group
	r.Group(func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(cfg.Secret))

		r.Post("/config/get", handler.ConfigGetHandler(cfg))

		r.Handle("/pre_peer", &handler.PrePeerHandler{
			Cfg: cfg,
			GetPeerNum: func() (int, int, error) {
				wgCount, birdCount, err := countPeers("/etc/wireguard", "/etc/bird/dn42_peers")
				return wgCount, birdCount, err
			},
		})

		r.Handle("/info", &handler.InfoHandler{
			Cfg:         cfg,
			WGConfDir:   "/etc/wireguard",
			BirdConfDir: "/etc/bird/dn42_peers",
			RunCmd:      runCmd,
		})

		r.Handle("/peer", &handler.PeerHandler{
			Cfg:         cfg,
			WGConfDir:   "/etc/wireguard",
			BirdConfDir: "/etc/bird/dn42_peers",
			RunCmd:      runCmd,
		})

		r.Handle("/remove", handler.NewRemoveHandler(cfg, runCmd, os.Remove))

		r.Handle("/restart", handler.NewRestartHandler(cfg, runCmd))

		r.Handle("/errorlist", &handler.ErrorListHandler{
			Cfg: cfg,
			ListWGASNs: func() ([]int, error) {
				return listASNs("/etc/wireguard", "dn42-", ".conf")
			},
			ListBirdASNs: func() ([]int, error) {
				return listASNs("/etc/bird/dn42_peers", "", ".conf")
			},
			GetHandshake: func(ctx context.Context, iface string) (int64, error) {
				out, err := runCmd(ctx, "wg", []string{"show", iface, "latest-handshakes"}, 10*time.Second)
				if err != nil {
					return 0, err
				}
				return service.ParseHandshake(out)
			},
			GetProtocolStatus: service.GetProtocolStatus,
			ReadBirdConfig: func(asn int) (string, error) {
				data, err := os.ReadFile(filepath.Join("/etc/bird/dn42_peers", fmt.Sprintf("%d.conf", asn)))
				return string(data), err
			},
			Now: time.Now,
		})

		r.Handle("/listpeers", &handler.ListPeersHandler{
			ListWGASNs: func() ([]int, error) {
				return listASNs("/etc/wireguard", "dn42-", ".conf")
			},
			ListBirdASNs: func() ([]int, error) {
				return listASNs("/etc/bird/dn42_peers", "", ".conf")
			},
		})

		r.Post("/ping", handler.PingHandler(runCmd))
		r.Post("/trace", handler.TraceHandler(nil))
		r.Post("/tcping", handler.TCPingHandler(nil))

		r.Handle("/route", handler.RouteHandler(cfg, handler.NewBirdcRunner()))
		r.Handle("/path", handler.PathHandler(cfg, handler.NewBirdcRunner()))

		r.Handle("/igp_topology", handler.TopologyHandler(cfg, handler.DefaultBirdCommand()))
	})

	ctx := context.Background()
	service.EnsureWGInterfacesUp(ctx, cfg, service.DefaultRecoveryDeps())

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-done
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
	log.Println("server stopped")
}

func countPeers(wgDir, birdDir string) (int, int, error) {
	wgCount, err := countConfFiles(wgDir, "dn42-")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read WG config dir: %w", err)
	}
	birdCount, err := countConfFiles(birdDir, "")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read BIRD config dir: %w", err)
	}
	return wgCount, birdCount, nil
}

func countConfFiles(dir, prefix string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".conf") {
			continue
		}
		numStr := name[len(prefix) : len(name)-5]
		if _, err := strconv.Atoi(numStr); err == nil {
			count++
		}
	}
	return count, nil
}

func listASNs(dir, prefix, suffix string) ([]int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var asns []int
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		numStr := name[len(prefix) : len(name)-len(suffix)]
		if asn, err := strconv.Atoi(numStr); err == nil {
			asns = append(asns, asn)
		}
	}
	return asns, nil
}
