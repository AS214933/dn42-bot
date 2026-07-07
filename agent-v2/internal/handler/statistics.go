package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	ntrace "github.com/nxtrace/NTrace-core/trace"
)

type CommandRunner func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error)

var ntraceTraceroute = ntrace.TracerouteWithContext

func PingHandler(runner CommandRunner, resolvers ...IPResolver) http.HandlerFunc {
	resolver := optionalResolver(resolvers)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		target := readBodyTarget(w, r)
		if target == "" {
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()

		commandTarget, err := resolveCommandTarget(ctx, resolver, target)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		output, err := runner(ctx, "ping", []string{"-c", "5", "-w", "6", commandTarget}, 8*time.Second)
		if err != nil && ctx.Err() == context.DeadlineExceeded {
			http.Error(w, "Request Timeout", http.StatusRequestTimeout)
			return
		}
		if err != nil && output == "" {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(output))
	}
}

func TraceHandler(runner CommandRunner, resolvers ...IPResolver) http.HandlerFunc {
	resolver := optionalResolver(resolvers)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		target := readBodyTarget(w, r)
		if target == "" {
			return
		}

		output, err := runTrace(r.Context(), runner, target, resolver)
		if err != nil && output == "" {
			if err == errTraceTimeout {
				http.Error(w, "Request Timeout", http.StatusRequestTimeout)
				return
			}
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		result := postProcessTrace(output)

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(result))
	}
}

var errTraceTimeout = fmt.Errorf("trace timeout")

func runTrace(ctx context.Context, runner CommandRunner, target string, resolver IPResolver) (string, error) {
	if runner == nil {
		return runNativeTrace(ctx, target, resolver)
	}
	return runCommandTrace(ctx, runner, target)
}

func runCommandTrace(ctx context.Context, runner CommandRunner, target string) (string, error) {
	ctx1, cancel1 := context.WithTimeout(ctx, 8*time.Second)
	defer cancel1()

	output, err := runner(ctx1, "traceroute", []string{"-q1", "-N32", "-w1", target}, 8*time.Second)
	if err != nil && ctx1.Err() == context.DeadlineExceeded {
		ctx2, cancel2 := context.WithTimeout(ctx, 8*time.Second)
		defer cancel2()

		output, err = runner(ctx2, "traceroute", []string{"-q1", "-N32", "-w1", "-n", target}, 8*time.Second)
		if err != nil && ctx2.Err() == context.DeadlineExceeded {
			return "", errTraceTimeout
		}
	}

	return output, err
}

func runNativeTrace(ctx context.Context, target string, resolver IPResolver) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	dstIP, err := resolveTraceTarget(ctx, resolver, target)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", errTraceTimeout
		}
		return "", err
	}

	result, err := ntraceTraceroute(ctx, ntrace.ICMPTrace, ntrace.Config{
		DstIP:            dstIP,
		BeginHop:         1,
		MaxHops:          30,
		NumMeasurements:  1,
		MaxAttempts:      1,
		ParallelRequests: 32,
		Timeout:          time.Second,
		Lang:             "en",
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return "", errTraceTimeout
		}
		return "", err
	}

	return formatNativeTrace(target, dstIP, result), nil
}

func resolveTraceTarget(ctx context.Context, resolver IPResolver, target string) (net.IP, error) {
	if ip := net.ParseIP(target); ip != nil {
		return ip, nil
	}
	addrs, err := lookupIPAddrs(ctx, resolver, target)
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if addr.IP != nil {
			return addr.IP, nil
		}
	}
	return nil, fmt.Errorf("no address found for %s", target)
}

func formatNativeTrace(target string, dstIP net.IP, result *ntrace.Result) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("traceroute to %s (%s), 30 hops max, 80 byte packets", target, dstIP.String()))
	if result == nil {
		return sb.String()
	}
	for i, probes := range result.Hops {
		ttl := i + 1
		if len(probes) > 0 && probes[0].TTL > 0 {
			ttl = probes[0].TTL
		}
		sb.WriteString(fmt.Sprintf("\n%2d", ttl))
		if len(probes) == 0 {
			sb.WriteString("  *")
			continue
		}
		for _, hop := range probes {
			if !hop.Success || hop.Address == nil {
				sb.WriteString("  *")
				continue
			}
			name := traceHopAddress(hop)
			if hop.Hostname != "" {
				name = fmt.Sprintf("%s (%s)", hop.Hostname, name)
			}
			sb.WriteString(fmt.Sprintf("  %s  %.3f ms", name, float64(hop.RTT.Microseconds())/1000))
		}
	}
	return sb.String()
}

func traceHopAddress(hop ntrace.Hop) string {
	switch addr := hop.Address.(type) {
	case *net.IPAddr:
		return addr.IP.String()
	case *net.UDPAddr:
		return addr.IP.String()
	case *net.TCPAddr:
		return addr.IP.String()
	default:
		raw := strings.TrimSpace(hop.Address.String())
		if host, _, err := net.SplitHostPort(raw); err == nil {
			return strings.Trim(host, "[]")
		}
		return raw
	}
}

func resolveCommandTarget(ctx context.Context, resolver IPResolver, target string) (string, error) {
	if resolver == nil {
		return target, nil
	}
	if ip := net.ParseIP(target); ip != nil {
		return ip.String(), nil
	}
	addrs, err := lookupIPAddrs(ctx, resolver, target)
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		if addr.IP != nil {
			return addr.IP.String(), nil
		}
	}
	return "", fmt.Errorf("no address found for %s", target)
}

var traceAllStarsRegex = regexp.MustCompile(`^\s*\d+(?:\s+\*)+$`)

func postProcessTrace(output string) string {
	lines := strings.Split(output, "\n")

	var reversed []string
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			reversed = append(reversed, lines[i])
		}
	}

	if len(reversed) == 0 {
		return ""
	}

	total := 0
	for len(reversed) > 0 && traceAllStarsRegex.MatchString(reversed[0]) {
		reversed = reversed[1:]
		total++
	}

	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}

	result := strings.Join(reversed, "\n")
	if total > 0 {
		result += fmt.Sprintf("\n\n%d hops not responding.", total)
	}
	return result
}

var tcpingNoiseRegex = regexp.MustCompile(`\n\nPing (?:stopped|interrupted).\n\n`)

func TCPingHandler(runner CommandRunner, resolvers ...IPResolver) http.HandlerFunc {
	resolver := optionalResolver(resolvers)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		target := readBodyTarget(w, r)
		if target == "" {
			return
		}

		if runner == nil {
			output, err := runNativeTCPing(r.Context(), resolver, target, 5)
			if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "Request Timeout", http.StatusRequestTimeout)
				return
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(output))
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		output, err := runner(ctx, "tcping", []string{"--no-color", "-c", "5", target}, 10*time.Second)
		if err != nil && ctx.Err() == context.DeadlineExceeded {
			http.Error(w, "Request Timeout", http.StatusRequestTimeout)
			return
		}
		if err != nil && output == "" {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		output = tcpingNoiseRegex.ReplaceAllString(output, "\n")

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(output))
	}
}

func runNativeTCPing(ctx context.Context, resolver IPResolver, target string, count int) (string, error) {
	host, port, err := parseTCPingTarget(target)
	if err != nil {
		return "", err
	}
	if count <= 0 {
		count = 5
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	addresses, err := resolveTCPingAddresses(ctx, resolver, host, port)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	success := 0
	dialer := net.Dialer{}
	for i := 1; i <= count; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		probeCtx, probeCancel := context.WithTimeout(ctx, 2*time.Second)
		start := time.Now()
		conn, err := dialTCPAddresses(probeCtx, dialer, addresses)
		elapsed := time.Since(start)
		probeTimedOut := errors.Is(probeCtx.Err(), context.DeadlineExceeded)
		requestTimedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		probeCancel()

		if err == nil {
			success++
			conn.Close()
			sb.WriteString(fmt.Sprintf("Connected to %s:%s: seq=%d time=%.2f ms\n", host, port, i, float64(elapsed.Microseconds())/1000))
			continue
		}
		if requestTimedOut {
			return "", context.DeadlineExceeded
		}
		errText := err.Error()
		if probeTimedOut {
			errText = "timeout"
		}
		sb.WriteString(fmt.Sprintf("Connect to %s:%s: seq=%d failed: %s\n", host, port, i, errText))
	}

	sb.WriteString(fmt.Sprintf("\nPing statistics for %s:%s\n", host, port))
	sb.WriteString(fmt.Sprintf(" %d probes sent, %d successful, %d failed.", count, success, count-success))
	return sb.String(), nil
}

func parseTCPingTarget(target string) (host, port string, err error) {
	fields := strings.Fields(target)
	switch len(fields) {
	case 1:
		host, port, err = net.SplitHostPort(fields[0])
		if err != nil {
			host, port, err = splitHostPortCompat(fields[0])
		}
	case 2:
		host, port = fields[0], fields[1]
	default:
		err = fmt.Errorf("expected target as host port or host:port")
	}
	if err != nil {
		return "", "", err
	}
	if host == "" || port == "" {
		return "", "", fmt.Errorf("host and port are required")
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum <= 0 || portNum > 65535 {
		return "", "", fmt.Errorf("invalid port: %s", port)
	}
	return strings.Trim(host, "[]"), port, nil
}

func splitHostPortCompat(target string) (string, string, error) {
	if strings.Count(target, ":") != 1 {
		return "", "", fmt.Errorf("expected target as host:port")
	}
	parts := strings.SplitN(target, ":", 2)
	return parts[0], parts[1], nil
}

func resolveTCPingAddresses(ctx context.Context, resolver IPResolver, host, port string) ([]string, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []string{net.JoinHostPort(ip.String(), port)}, nil
	}

	addrs, err := lookupIPAddrs(ctx, resolver, host)
	if err != nil {
		return nil, err
	}
	addresses := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IP != nil {
			addresses = append(addresses, net.JoinHostPort(addr.IP.String(), port))
		}
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("no address found for %s", host)
	}
	return addresses, nil
}

func dialTCPAddresses(ctx context.Context, dialer net.Dialer, addresses []string) (net.Conn, error) {
	var lastErr error
	for _, address := range addresses {
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			return conn, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no address found")
	}
	return nil, lastErr
}

func readBodyTarget(w http.ResponseWriter, r *http.Request) string {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return ""
	}
	target := strings.TrimSpace(string(body))
	if target == "" {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return ""
	}
	return target
}
