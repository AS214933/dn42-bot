package lookingglass

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

const (
	defaultBirdMaxConcurrent       = 16
	defaultTracerouteMaxConcurrent = 10
	defaultRequestTimeout          = 15 * time.Second
	defaultMaxQueryLength          = 4096
	defaultMaxOutputBytes          = 64 * 1024
)

// TraceRunner executes one traceroute for the requested address family.
// family is 0 when the runner should automatically select an address family.
type TraceRunner func(ctx context.Context, target string, family int) (string, error)

// Config controls the embedded bird-lg-go proxy compatibility handler.
// AllowedSources and DisallowedSources accept CIDRs, individual IP addresses,
// and the selectors any, public, private, and dn42.
type Config struct {
	BirdSocket              string
	AllowedSources          []string
	DisallowedSources       []string
	BirdMaxConcurrent       int
	TracerouteMaxConcurrent int
	RequestTimeout          time.Duration
	MaxQueryLength          int
	MaxOutputBytes          int
}

type Handler struct {
	policy         accessPolicy
	bird           birdClient
	trace          TraceRunner
	birdSemaphore  chan struct{}
	traceSemaphore chan struct{}
	requestTimeout time.Duration
	maxQueryLength int
	maxOutputBytes int
}

func NewHandler(cfg Config, traceRunner TraceRunner) (*Handler, error) {
	if strings.TrimSpace(cfg.BirdSocket) == "" {
		return nil, errors.New("BIRD socket path is required")
	}
	policy, err := newAccessPolicy(cfg.AllowedSources, cfg.DisallowedSources)
	if err != nil {
		return nil, err
	}

	if cfg.BirdMaxConcurrent <= 0 {
		cfg.BirdMaxConcurrent = defaultBirdMaxConcurrent
	}
	if cfg.TracerouteMaxConcurrent <= 0 {
		cfg.TracerouteMaxConcurrent = defaultTracerouteMaxConcurrent
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.MaxQueryLength <= 0 {
		cfg.MaxQueryLength = defaultMaxQueryLength
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}

	return &Handler{
		policy:         policy,
		bird:           birdClient{socketPath: cfg.BirdSocket, maxOutputBytes: cfg.MaxOutputBytes},
		trace:          traceRunner,
		birdSemaphore:  make(chan struct{}, cfg.BirdMaxConcurrent),
		traceSemaphore: make(chan struct{}, cfg.TracerouteMaxConcurrent),
		requestTimeout: cfg.RequestTimeout,
		maxQueryLength: cfg.MaxQueryLength,
		maxOutputBytes: cfg.MaxOutputBytes,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.sourceAllowed(r.RemoteAddr) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	switch r.URL.Path {
	case "/bird", "/bird6":
		h.serveBird(w, r)
	case "/traceroute":
		h.serveTraceroute(w, r, 0)
	case "/traceroute6":
		h.serveTraceroute(w, r, 0)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) sourceAllowed(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && h.policy.Allows(addr)
}

func (h *Handler) serveBird(w http.ResponseWriter, r *http.Request) {
	query, ok := h.readQuery(w, r)
	if !ok {
		return
	}
	if !isAllowedBirdCommand(query) {
		http.Error(w, "Forbidden: only 'show protocols' and 'show route' commands are allowed", http.StatusForbidden)
		return
	}
	if !acquire(h.birdSemaphore) {
		http.Error(w, "Too many concurrent BIRD requests. Please try again later.", http.StatusServiceUnavailable)
		return
	}
	defer release(h.birdSemaphore)

	ctx, cancel := context.WithTimeout(r.Context(), h.requestTimeout)
	defer cancel()
	output, err := h.bird.Query(ctx, query)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			http.Error(w, "Request Timeout", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, "BIRD query failed", http.StatusInternalServerError)
		return
	}
	writePlainText(w, output)
}

func (h *Handler) serveTraceroute(w http.ResponseWriter, r *http.Request, family int) {
	target, ok := h.readQuery(w, r)
	if !ok {
		return
	}
	target = strings.TrimSpace(target)
	if target == "" || strings.HasPrefix(target, "-") {
		http.Error(w, "Invalid target.", http.StatusBadRequest)
		return
	}
	if h.trace == nil {
		http.Error(w, "traceroute not supported on this node.", http.StatusNotImplemented)
		return
	}
	if !acquire(h.traceSemaphore) {
		http.Error(w, "Too many concurrent traceroute requests. Please try again later.", http.StatusServiceUnavailable)
		return
	}
	defer release(h.traceSemaphore)

	ctx, cancel := context.WithTimeout(r.Context(), h.requestTimeout)
	defer cancel()
	output, err := h.trace(ctx, target, family)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "Request Timeout", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, "Error executing traceroute", http.StatusInternalServerError)
		return
	}
	if len(output) > h.maxOutputBytes {
		output = output[:h.maxOutputBytes]
	}
	writePlainText(w, output)
}

func (h *Handler) readQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Invalid Request", http.StatusBadRequest)
		return "", false
	}
	if len(query) > h.maxQueryLength {
		http.Error(w, "Query too long", http.StatusRequestURITooLong)
		return "", false
	}
	if containsControlCharacter(query) {
		http.Error(w, "Invalid Request", http.StatusBadRequest)
		return "", false
	}
	return query, true
}

func isAllowedBirdCommand(query string) bool {
	for _, command := range []string{"show protocols", "show route"} {
		if query == command || strings.HasPrefix(query, command+" ") {
			return true
		}
	}
	return false
}

func containsControlCharacter(value string) bool {
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return true
		}
	}
	return false
}

func acquire(semaphore chan struct{}) bool {
	select {
	case semaphore <- struct{}{}:
		return true
	default:
		return false
	}
}

func release(semaphore chan struct{}) {
	<-semaphore
}

func writePlainText(w http.ResponseWriter, output string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprint(w, output)
}
