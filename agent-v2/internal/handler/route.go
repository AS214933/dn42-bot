package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/service"
)

type BirdcRunner func(ctx context.Context, ctlPath, table, target string) (string, error)

func NewBirdcRunner() BirdcRunner {
	return func(ctx context.Context, ctlPath, table, target string) (string, error) {
		return service.RunCommand(ctx, "birdc", []string{"-s", ctlPath, "show", "route", "table", table, "for", target, "all", "primary"}, 30*time.Second)
	}
}

func selectTable(target string, cfg *config.Config) string {
	if strings.Contains(target, ":") {
		return cfg.BirdTable6
	}
	return cfg.BirdTable4
}

func readTarget(r *http.Request) (string, int) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", http.StatusBadRequest
	}
	target := strings.TrimSpace(string(body))
	if target == "" {
		return "", http.StatusBadRequest
	}
	return target, 0
}

func RouteHandler(cfg *config.Config, runner BirdcRunner) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, status := readTarget(r)
		if status != 0 {
			http.Error(w, "bad request", status)
			return
		}

		table := selectTable(target, cfg)

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		output, err := runner(ctx, cfg.BirdCtlPath, table, target)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				http.Error(w, "request timeout", http.StatusRequestTimeout)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(output))
	})
}

func PathHandler(cfg *config.Config, runner BirdcRunner) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, status := readTarget(r)
		if status != 0 {
			http.Error(w, "bad request", status)
			return
		}

		table := selectTable(target, cfg)

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		output, err := runner(ctx, cfg.BirdCtlPath, table, target)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				http.Error(w, "request timeout", http.StatusRequestTimeout)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "BGP.as_path") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					w.Header().Set("Content-Type", "text/plain")
					w.Write([]byte(strings.TrimSpace(parts[1])))
					return
				}
			}
		}

		http.Error(w, "not found", http.StatusNotFound)
	})
}
