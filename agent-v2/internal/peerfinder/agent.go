package peerfinder

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	AgentVersion     = "1.0.6"
	nbPings          = 4
	maxTimestampSkew = 30 * time.Second
	connTimeout      = 15 * time.Second
	authDeadline     = 3 * time.Second
	defaultMaxWorker = 8
	maxBodySize      = 2000
	sigSize          = 32
	tsSize           = 8
	nonceSize        = 32
	headerSize       = sigSize + tsSize + nonceSize + 2
)

type CommandRunner func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error)

type Config struct {
	Host       string
	Port       int
	HMACKey    []byte
	MaxWorkers int
	Runner     CommandRunner
	Logger     *log.Logger
	Now        func() time.Time
}

type Server struct {
	host       string
	port       int
	hmacKey    []byte
	maxWorkers int
	runner     CommandRunner
	logger     *log.Logger
	now        func() time.Time
	nonces     *nonceCache
}

type request struct {
	Command string `json:"command"`
	IP      string `json:"ip"`
}

type PingResult struct {
	Reachable bool     `json:"reachable"`
	Sent      int      `json:"sent"`
	Recv      int      `json:"recv"`
	Latency   *float64 `json:"latency"`
	Jitter    *float64 `json:"jitter"`
	MinRTT    *float64 `json:"min_rtt"`
	MaxRTT    *float64 `json:"max_rtt"`
	Version   *string  `json:"version"`
}

func NewServer(cfg Config) (*Server, error) {
	host := cfg.Host
	if host == "" {
		host = "::"
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid peerfinder port %d", cfg.Port)
	}
	if len(cfg.HMACKey) != sha256.Size {
		return nil, fmt.Errorf("invalid peerfinder hmac key length %d", len(cfg.HMACKey))
	}
	if cfg.Runner == nil {
		return nil, fmt.Errorf("peerfinder command runner is required")
	}
	maxWorkers := cfg.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = defaultMaxWorker
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Server{
		host:       host,
		port:       cfg.Port,
		hmacKey:    append([]byte{}, cfg.HMACKey...),
		maxWorkers: maxWorkers,
		runner:     cfg.Runner,
		logger:     cfg.Logger,
		now:        now,
		nonces:     newNonceCache(maxTimestampSkew),
	}, nil
}

func (s *Server) Addr() string {
	return net.JoinHostPort(s.host, strconv.Itoa(s.port))
}

func (s *Server) Listen(ctx context.Context) (net.Listener, error) {
	if isUnspecifiedHost(s.host) {
		return s.listenDualStack(ctx)
	}
	lc := net.ListenConfig{Control: disableIPv6Only}
	return lc.Listen(ctx, "tcp", s.Addr())
}

func (s *Server) listenDualStack(ctx context.Context) (net.Listener, error) {
	port := strconv.Itoa(s.port)
	addr4 := net.JoinHostPort("0.0.0.0", port)
	addr6 := net.JoinHostPort("::", port)

	lc4 := net.ListenConfig{}
	ln4, err4 := lc4.Listen(ctx, "tcp4", addr4)

	lc6 := net.ListenConfig{Control: enableIPv6Only}
	ln6, err6 := lc6.Listen(ctx, "tcp6", addr6)

	if ln4 != nil && ln6 != nil {
		return newDualListener(ln4, ln6), nil
	}
	if ln4 != nil {
		if ln6 != nil {
			_ = ln6.Close()
		}
		return ln4, nil
	}
	if ln6 != nil {
		if ln4 != nil {
			_ = ln4.Close()
		}
		return ln6, nil
	}
	return nil, fmt.Errorf("peerfinder failed to listen on v4 (%v) and v6 (%v)", err4, err6)
}

type dualListener struct {
	v4     net.Listener
	v6     net.Listener
	ctx    context.Context
	cancel context.CancelFunc
	conns  chan net.Conn
	err    error
	once   sync.Once
	addr   net.Addr
}

func newDualListener(v4, v6 net.Listener) net.Listener {
	ctx, cancel := context.WithCancel(context.Background())
	d := &dualListener{
		v4:     v4,
		v6:     v6,
		ctx:    ctx,
		cancel: cancel,
		conns:  make(chan net.Conn, 2),
		addr:   v4.Addr(),
	}
	go d.acceptLoop(v4)
	go d.acceptLoop(v6)
	return d
}

func (d *dualListener) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if d.ctx.Err() == nil {
				d.err = err
				_ = d.v4.Close()
				_ = d.v6.Close()
			}
			d.cancel()
			return
		}
		select {
		case d.conns <- conn:
		case <-d.ctx.Done():
			_ = conn.Close()
			return
		}
	}
}

func (d *dualListener) Accept() (net.Conn, error) {
	select {
	case conn := <-d.conns:
		return conn, nil
	case <-d.ctx.Done():
		if d.err != nil {
			return nil, d.err
		}
		return nil, net.ErrClosed
	}
}

func (d *dualListener) Close() error {
	d.once.Do(func() {
		d.cancel()
		_ = d.v4.Close()
		_ = d.v6.Close()
		for {
			select {
			case conn := <-d.conns:
				_ = conn.Close()
			default:
				return
			}
		}
	})
	return nil
}

func (d *dualListener) Addr() net.Addr {
	return d.addr
}

func isUnspecifiedHost(host string) bool {
	host = strings.TrimSpace(host)
	return host == "" || host == "::" || host == "0.0.0.0"
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := s.Listen(ctx)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	if dl, ok := ln.(*dualListener); ok {
		s.logf("DN42 Peer Finder Agent %s listening on %s", AgentVersion, dl.v4.Addr())
		s.logf("DN42 Peer Finder Agent %s listening on %s", AgentVersion, dl.v6.Addr())
	} else {
		s.logf("DN42 Peer Finder Agent %s listening on %s", AgentVersion, ln.Addr())
	}
	slots := make(chan struct{}, s.maxWorkers)
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return nil
		}

		conn, err := ln.Accept()
		if err != nil {
			<-slots
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.logf("peerfinder accept error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		go func() {
			defer func() {
				_ = conn.Close()
				<-slots
			}()
			s.handleConnection(conn, conn.RemoteAddr())
		}()
	}
}

func disableIPv6Only(network, _ string, c syscall.RawConn) error {
	if network != "tcp6" {
		return nil
	}
	return c.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, 0)
	})
}

func enableIPv6Only(network, _ string, c syscall.RawConn) error {
	if network != "tcp6" {
		return nil
	}
	return c.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, 1)
	})
}

func (s *Server) handleConnection(conn net.Conn, addr net.Addr) {
	tsBuf, nonceBuf, body, ok := s.readAuthenticatedRequest(conn, addr)
	if !ok {
		return
	}

	now := s.now()
	_ = conn.SetDeadline(now.Add(connTimeout))

	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		s.logf("peerfinder invalid json from %s: %v", addr, err)
		return
	}

	var result any
	switch req.Command {
	case "version":
		result = map[string]string{"version": AgentVersion}
	case "ping":
		if req.IP == "" {
			return
		}
		if _, err := netip.ParseAddr(req.IP); err != nil {
			s.logf("peerfinder invalid ping ip from %s: %q", addr, req.IP)
			return
		}
		pingResult := s.runPing(req.IP)
		version := AgentVersion
		pingResult.Version = &version
		result = pingResult
	default:
		s.logf("peerfinder unknown command %q from %s", req.Command, addr)
		result = map[string]string{"error": "unknown command"}
	}

	respPayload, err := json.Marshal(result)
	if err != nil || len(respPayload) > 65535 {
		return
	}
	respHeader := make([]byte, headerSize)
	copy(respHeader[:sigSize], s.sign(tsBuf, nonceBuf, respPayload))
	copy(respHeader[sigSize:sigSize+tsSize], tsBuf)
	copy(respHeader[sigSize+tsSize:sigSize+tsSize+nonceSize], nonceBuf)
	binary.BigEndian.PutUint16(respHeader[headerSize-2:], uint16(len(respPayload)))
	if err := writeAll(conn, append(respHeader, respPayload...), s.now().Add(connTimeout)); err != nil {
		s.logf("peerfinder response write failed to %s: %v", addr, err)
	}
}

func (s *Server) readAuthenticatedRequest(conn net.Conn, addr net.Addr) ([]byte, []byte, []byte, bool) {
	deadline := s.now().Add(authDeadline)
	header := make([]byte, headerSize)
	if err := readExact(conn, header, deadline); err != nil {
		return nil, nil, nil, false
	}

	sig := header[:sigSize]
	tsBuf := header[sigSize : sigSize+tsSize]
	nonceBuf := header[sigSize+tsSize : sigSize+tsSize+nonceSize]
	bodyLen := binary.BigEndian.Uint16(header[headerSize-2:])
	if bodyLen > maxBodySize {
		return nil, nil, nil, false
	}

	body := make([]byte, bodyLen)
	if err := readExact(conn, body, deadline); err != nil {
		return nil, nil, nil, false
	}
	if !hmac.Equal(s.sign(tsBuf, nonceBuf, body), sig) {
		return nil, nil, nil, false
	}

	reqTS := int64(binary.BigEndian.Uint64(tsBuf))
	now := s.now()
	if delta := now.Sub(time.Unix(reqTS, 0)); delta > maxTimestampSkew || delta < -maxTimestampSkew {
		s.logf("peerfinder rejected stale request from %s", addr)
		return nil, nil, nil, false
	}
	if !s.nonces.checkAndAdd(nonceBuf, reqTS, now.Unix()) {
		s.logf("peerfinder rejected replayed nonce from %s", addr)
		return nil, nil, nil, false
	}

	return append([]byte{}, tsBuf...), append([]byte{}, nonceBuf...), body, true
}

func (s *Server) runPing(ip string) PingResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := s.runner(ctx, "ping", []string{"-n", "-q", "-c", strconv.Itoa(nbPings), "-w", "6", ip}, 10*time.Second)
	if err != nil && output == "" {
		s.logf("peerfinder ping to %s failed: %v", ip, err)
		return defaultPingResult()
	}
	return ParsePingOutput(output)
}

func (s *Server) sign(tsBuf, nonceBuf, body []byte) []byte {
	mac := hmac.New(sha256.New, s.hmacKey)
	_, _ = mac.Write(tsBuf)
	_, _ = mac.Write(nonceBuf)
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}

func (s *Server) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

var (
	packetStatsRE = regexp.MustCompile(`(\d+) packets transmitted, (\d+) (?:packets )?received`)
	rttStatsRE    = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)/([0-9]+(?:\.[0-9]+)?)/([0-9]+(?:\.[0-9]+)?)/([0-9]+(?:\.[0-9]+)?)`)
)

func ParsePingOutput(output string) PingResult {
	result := defaultPingResult()
	if match := packetStatsRE.FindStringSubmatch(output); len(match) == 3 {
		result.Sent, _ = strconv.Atoi(match[1])
		result.Recv, _ = strconv.Atoi(match[2])
		result.Reachable = result.Recv > 0
	}
	if match := rttStatsRE.FindStringSubmatch(output); len(match) == 5 {
		result.MinRTT = parseFloatPtr(match[1])
		result.Latency = parseFloatPtr(match[2])
		result.MaxRTT = parseFloatPtr(match[3])
		result.Jitter = parseFloatPtr(match[4])
	}
	return result
}

func defaultPingResult() PingResult {
	return PingResult{}
}

func parseFloatPtr(value string) *float64 {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func readExact(conn net.Conn, buf []byte, deadline time.Time) error {
	_ = conn.SetReadDeadline(deadline)
	_, err := io.ReadFull(conn, buf)
	return err
}

func writeAll(conn net.Conn, data []byte, deadline time.Time) error {
	_ = conn.SetWriteDeadline(deadline)
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
		data = data[n:]
	}
	return nil
}

type nonceCache struct {
	windowSeconds int64
	mu            sync.Mutex
	seen          map[string]int64
}

func newNonceCache(window time.Duration) *nonceCache {
	return &nonceCache{
		windowSeconds: int64(window / time.Second),
		seen:          make(map[string]int64),
	}
}

func (c *nonceCache) checkAndAdd(nonce []byte, reqTS, now int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for nonce, expiry := range c.seen {
		if expiry < now {
			delete(c.seen, nonce)
		}
	}
	key := string(nonce)
	if _, ok := c.seen[key]; ok {
		return false
	}
	c.seen[key] = reqTS + c.windowSeconds
	return true
}
