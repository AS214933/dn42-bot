package handler

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ntrace "github.com/nxtrace/NTrace-core/trace"
)

func TestPingHandler_Success(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		if name != "ping" {
			t.Fatalf("expected command 'ping', got %q", name)
		}
		if len(args) != 5 || args[0] != "-c" || args[1] != "5" || args[2] != "-w" || args[3] != "6" || args[4] != "172.20.0.1" {
			t.Fatalf("unexpected args: %v", args)
		}
		if timeout != 8*time.Second {
			t.Fatalf("expected timeout 8s, got %v", timeout)
		}
		return "PING 172.20.0.1 (172.20.0.1) 56(84) bytes of data.\n64 bytes from 172.20.0.1: icmp_seq=1 ttl=64 time=1.23 ms", nil
	}
	handler := PingHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader("172.20.0.1"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PING 172.20.0.1") {
		t.Errorf("expected PING in output, got %q", rec.Body.String())
	}
}

func TestPingHandler_Timeout(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	handler := PingHandler(runner)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader("172.20.0.1")).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestTimeout {
		t.Errorf("expected status 408, got %d", rec.Code)
	}
}

func TestPingHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		t.Fatal("runner should not be called")
		return "", nil
	}
	handler := PingHandler(runner)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestPingHandler_EmptyBody(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		t.Fatal("runner should not be called for empty target")
		return "", nil
	}
	handler := PingHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestPingHandler_WhitespaceOnlyBody(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		t.Fatal("runner should not be called for whitespace-only target")
		return "", nil
	}
	handler := PingHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader("   \n\t  "))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestPingHandler_PassthroughOnNonTimeoutError(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		return "some output with error", context.Canceled
	}
	handler := PingHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader("172.20.0.1"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 (output available), got %d", rec.Code)
	}
	if rec.Body.String() != "some output with error" {
		t.Errorf("expected output passthrough, got %q", rec.Body.String())
	}
}

func TestTraceHandler_Success(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		if name != "traceroute" {
			t.Fatalf("expected command 'traceroute', got %q", name)
		}
		if len(args) != 4 || args[0] != "-q1" || args[1] != "-N32" || args[2] != "-w1" || args[3] != "172.20.0.1" {
			t.Fatalf("unexpected args: %v", args)
		}
		if timeout != 8*time.Second {
			t.Fatalf("expected timeout 8s, got %v", timeout)
		}
		return "traceroute to 172.20.0.1 (172.20.0.1), 30 hops max, 60 byte packets\n" +
			" 1  10.0.0.1  1.234 ms\n" +
			" 2  *\n" +
			" 3  172.20.0.1  5.678 ms", nil
	}
	handler := TraceHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader("172.20.0.1"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "traceroute to 172.20.0.1") {
		t.Errorf("expected traceroute header in output, got %q", body)
	}
	if !strings.Contains(body, "1  10.0.0.1  1.234 ms") {
		t.Errorf("expected hop 1 in output, got %q", body)
	}
}

func TestTraceHandler_TimeoutRetry(t *testing.T) {
	t.Parallel()
	callCount := 0
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		callCount++
		if callCount == 1 {
			<-ctx.Done()
			return "", ctx.Err()
		}
		for _, a := range args {
			if a == "-n" {
				return "traceroute to 172.20.0.1\n 1  10.0.0.1  1.234 ms", nil
			}
		}
		t.Fatal("expected -n flag in retry")
		return "", nil
	}
	handler := TraceHandler(runner)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader("172.20.0.1")).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (retry), got %d", callCount)
	}
	if !strings.Contains(rec.Body.String(), "10.0.0.1") {
		t.Errorf("expected retry output, got %q", rec.Body.String())
	}
}

func TestTraceHandler_DoubleTimeout(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	handler := TraceHandler(runner)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader("172.20.0.1")).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestTimeout {
		t.Errorf("expected status 408, got %d", rec.Code)
	}
}

func TestTraceHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		t.Fatal("runner should not be called")
		return "", nil
	}
	handler := TraceHandler(runner)

	req := httptest.NewRequest(http.MethodGet, "/trace", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestTraceHandler_EmptyBody(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		t.Fatal("runner should not be called")
		return "", nil
	}
	handler := TraceHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestTraceHandler_PostProcessing_RemovesTrailingStarHops(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		return "traceroute to 172.20.0.1\n" +
			" 1  10.0.0.1  1.234 ms\n" +
			" 2  *\n" +
			" 3  * * *\n" +
			" 4  * * *", nil
	}
	handler := TraceHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader("172.20.0.1"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, " 4  * * *") {
		t.Errorf("trailing star hop 4 should be removed, got %q", body)
	}
	if strings.Contains(body, " 3  * * *") {
		t.Errorf("trailing star hop 3 should be removed, got %q", body)
	}
	if !strings.Contains(body, "3 hops not responding.") {
		t.Errorf("expected '3 hops not responding.', got %q", body)
	}
	if !strings.Contains(body, " 1  10.0.0.1  1.234 ms") {
		t.Errorf("hop 1 should remain, got %q", body)
	}
}

func TestTraceHandler_PostProcessing_NoStarHops(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		return "traceroute to 172.20.0.1\n" +
			" 1  10.0.0.1  1.234 ms\n" +
			" 2  10.0.0.2  2.345 ms\n" +
			" 3  172.20.0.1  5.678 ms", nil
	}
	handler := TraceHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader("172.20.0.1"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "hops not responding") {
		t.Errorf("should not have 'hops not responding' when no star hops, got %q", body)
	}
}

func TestTraceHandler_PostProcessing_OnlyStarHops(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		return "traceroute to 172.20.0.1\n" +
			" 1  * * *\n" +
			" 2  * * *\n" +
			" 3  * * *", nil
	}
	handler := TraceHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader("172.20.0.1"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "3 hops not responding.") {
		t.Errorf("expected '3 hops not responding.', got %q", body)
	}
	if strings.Contains(body, "* * *") {
		t.Errorf("all star hops should be removed, got %q", body)
	}
}

func TestTraceHandler_PostProcessing_MixedTrailingStarHops(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		return "traceroute to 172.20.0.1\n" +
			" 1  10.0.0.1  1.234 ms\n" +
			" 2  * 10.0.0.2  2.345 ms\n" +
			" 3  * * *", nil
	}
	handler := TraceHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader("172.20.0.1"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "1 hops not responding.") {
		t.Errorf("expected '1 hops not responding.', got %q", body)
	}
	if !strings.Contains(body, "* 10.0.0.2  2.345 ms") {
		t.Errorf("hop 2 should remain, got %q", body)
	}
}

func TestTraceHandler_PostProcessing_EmptyLinesFiltered(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		return "traceroute to 172.20.0.1\n\n" +
			" 1  10.0.0.1  1.234 ms\n\n\n" +
			" 2  172.20.0.1  5.678 ms\n", nil
	}
	handler := TraceHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader("172.20.0.1"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "\n\n\n") {
		t.Errorf("consecutive empty lines should be filtered, got %q", body)
	}
}

func TestTraceHandler_RetryArgsContainNFlag(t *testing.T) {
	t.Parallel()
	callCount := 0
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		callCount++
		if callCount == 1 {
			<-ctx.Done()
			return "", ctx.Err()
		}
		found := false
		for _, a := range args {
			if a == "-n" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("retry should include -n flag, args: %v", args)
		}
		return "traceroute to 172.20.0.1\n 1  10.0.0.1  1.234 ms", nil
	}
	handler := TraceHandler(runner)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader("172.20.0.1")).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestTraceHandler_NativeNextTraceSuccess(t *testing.T) {
	oldTraceroute := ntraceTraceroute
	ntraceTraceroute = func(ctx context.Context, method ntrace.Method, config ntrace.Config) (*ntrace.Result, error) {
		if method != ntrace.ICMPTrace {
			t.Fatalf("method = %q, want %q", method, ntrace.ICMPTrace)
		}
		if config.BeginHop != 1 {
			t.Fatalf("BeginHop = %d, want 1", config.BeginHop)
		}
		if config.MaxHops != 30 {
			t.Fatalf("MaxHops = %d, want 30", config.MaxHops)
		}
		if config.NumMeasurements != 1 {
			t.Fatalf("NumMeasurements = %d, want 1", config.NumMeasurements)
		}
		return &ntrace.Result{Hops: [][]ntrace.Hop{
			{
				{
					Success: true,
					Address: &net.IPAddr{IP: net.ParseIP("10.0.0.1")},
					TTL:     1,
					RTT:     1234 * time.Microsecond,
				},
			},
			{
				{
					Success: false,
					TTL:     2,
				},
			},
			{
				{
					Success: true,
					Address: &net.IPAddr{IP: net.ParseIP("172.20.0.1")},
					TTL:     3,
					RTT:     5678 * time.Microsecond,
				},
			},
		}}, nil
	}
	t.Cleanup(func() { ntraceTraceroute = oldTraceroute })

	handler := TraceHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader("172.20.0.1"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "traceroute to 172.20.0.1 (172.20.0.1)") {
		t.Fatalf("expected traceroute header, got %q", body)
	}
	if !strings.Contains(body, " 1  10.0.0.1  1.234 ms") {
		t.Fatalf("expected first hop, got %q", body)
	}
	if !strings.Contains(body, " 2  *") {
		t.Fatalf("expected timeout hop, got %q", body)
	}
}

func TestTCPingHandler_Success(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		if name != "tcping" {
			t.Fatalf("expected command 'tcping', got %q", name)
		}
		if len(args) != 4 || args[0] != "--no-color" || args[1] != "-c" || args[2] != "5" || args[3] != "172.20.0.1:80" {
			t.Fatalf("unexpected args: %v", args)
		}
		if timeout != 10*time.Second {
			t.Fatalf("expected timeout 10s, got %v", timeout)
		}
		return "Ping statistics for 172.20.0.1:80\n 5 probes sent, 5 successful, 0 failed.", nil
	}
	handler := TCPingHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/tcping", strings.NewReader("172.20.0.1:80"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Ping statistics") {
		t.Errorf("expected ping stats in output, got %q", rec.Body.String())
	}
}

func TestTCPingHandler_NativeSuccessHostPortFields(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split listener addr: %v", err)
	}

	handler := TCPingHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/tcping", strings.NewReader("127.0.0.1 "+port))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Connected to 127.0.0.1:"+port) {
		t.Fatalf("expected connected probes, got %q", body)
	}
	if !strings.Contains(body, "5 probes sent, 5 successful, 0 failed") {
		t.Fatalf("expected success stats, got %q", body)
	}
}

func TestTCPingHandler_NativeInvalidTarget(t *testing.T) {
	t.Parallel()
	handler := TCPingHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/tcping", strings.NewReader("127.0.0.1 not-a-port"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestTCPingHandler_Timeout(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	handler := TCPingHandler(runner)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/tcping", strings.NewReader("172.20.0.1:80")).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestTimeout {
		t.Errorf("expected status 408, got %d", rec.Code)
	}
}

func TestTCPingHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		t.Fatal("runner should not be called")
		return "", nil
	}
	handler := TCPingHandler(runner)

	req := httptest.NewRequest(http.MethodGet, "/tcping", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestTCPingHandler_EmptyBody(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		t.Fatal("runner should not be called")
		return "", nil
	}
	handler := TCPingHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/tcping", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestTCPingHandler_OutputCleaning_Stopped(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		return "Ping statistics for 172.20.0.1:80\n\nPing stopped.\n\n 5 probes sent, 5 successful.", nil
	}
	handler := TCPingHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/tcping", strings.NewReader("172.20.0.1:80"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "Ping stopped") {
		t.Errorf("'Ping stopped.' should be removed, got %q", body)
	}
	if !strings.Contains(body, "5 probes sent") {
		t.Errorf("output content should be preserved, got %q", body)
	}
}

func TestTCPingHandler_OutputCleaning_Interrupted(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		return "Ping statistics for 172.20.0.1:80\n\nPing interrupted.\n\n 5 probes sent.", nil
	}
	handler := TCPingHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/tcping", strings.NewReader("172.20.0.1:80"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "Ping interrupted") {
		t.Errorf("'Ping interrupted.' should be removed, got %q", body)
	}
}

func TestTCPingHandler_OutputCleaning_NoMatch(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		return "Ping statistics for 172.20.0.1:80\n 5 probes sent, 5 successful, 0 failed.", nil
	}
	handler := TCPingHandler(runner)

	req := httptest.NewRequest(http.MethodPost, "/tcping", strings.NewReader("172.20.0.1:80"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if body != "Ping statistics for 172.20.0.1:80\n 5 probes sent, 5 successful, 0 failed." {
		t.Errorf("output should be unchanged, got %q", body)
	}
}
