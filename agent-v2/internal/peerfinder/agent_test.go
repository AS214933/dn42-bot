package peerfinder

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestParsePingOutput(t *testing.T) {
	output := `PING 172.20.0.1 (172.20.0.1) 56(84) bytes of data.

--- 172.20.0.1 ping statistics ---
4 packets transmitted, 3 received, 25% packet loss, time 3003ms
rtt min/avg/max/mdev = 1.234/2.345/3.456/0.111 ms
`
	result := ParsePingOutput(output)
	if !result.Reachable || result.Sent != 4 || result.Recv != 3 {
		t.Fatalf("packet stats = %+v", result)
	}
	if result.MinRTT == nil || *result.MinRTT != 1.234 {
		t.Fatalf("MinRTT = %v, want 1.234", result.MinRTT)
	}
	if result.Latency == nil || *result.Latency != 2.345 {
		t.Fatalf("Latency = %v, want 2.345", result.Latency)
	}
	if result.MaxRTT == nil || *result.MaxRTT != 3.456 {
		t.Fatalf("MaxRTT = %v, want 3.456", result.MaxRTT)
	}
	if result.Jitter == nil || *result.Jitter != 0.111 {
		t.Fatalf("Jitter = %v, want 0.111", result.Jitter)
	}
}

func TestVersionCommandReturnsSignedFrame(t *testing.T) {
	server := newTestServer(t, nil)
	nonce := fixedNonce()
	body := []byte(`{"command":"version"}`)

	respBody, respTS, respNonce := roundTripFrame(t, server, buildFrame(t, server, time.Now().Unix(), nonce, body))
	if string(respNonce) != string(nonce) {
		t.Fatalf("nonce = %x, want %x", respNonce, nonce)
	}
	if respTS == 0 {
		t.Fatal("response timestamp is zero")
	}
	var response map[string]string
	if err := json.Unmarshal(respBody, &response); err != nil {
		t.Fatal(err)
	}
	if response["version"] != AgentVersion {
		t.Fatalf("version = %q, want %q", response["version"], AgentVersion)
	}
	if _, ok := response["command"]; ok {
		t.Fatal("response must not contain command key")
	}
}

func TestPingCommandReturnsStructuredResult(t *testing.T) {
	var seenArgs []string
	runner := func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
		if name != "ping" {
			t.Fatalf("command = %q, want ping", name)
		}
		if timeout != 10*time.Second {
			t.Fatalf("timeout = %v, want 10s", timeout)
		}
		seenArgs = append([]string{}, args...)
		return `4 packets transmitted, 4 received, 0% packet loss
rtt min/avg/max/mdev = 0.100/0.200/0.300/0.010 ms
`, errors.New("exit status 1")
	}
	server := newTestServer(t, runner)
	body := []byte(`{"command":"ping","ip":"172.20.0.1"}`)

	respBody, _, _ := roundTripFrame(t, server, buildFrame(t, server, time.Now().Unix(), fixedNonce(), body))
	wantArgs := []string{"-n", "-q", "-c", strconv.Itoa(nbPings), "-w", "6", "172.20.0.1"}
	if len(seenArgs) != len(wantArgs) {
		t.Fatalf("args = %v, want %v", seenArgs, wantArgs)
	}
	for i, want := range wantArgs {
		if seenArgs[i] != want {
			t.Fatalf("args[%d] = %q, want %q; full args=%v", i, seenArgs[i], want, seenArgs)
		}
	}
	var response PingResult
	if err := json.Unmarshal(respBody, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Reachable || response.Sent != 4 || response.Recv != 4 {
		t.Fatalf("response packet stats = %+v", response)
	}
	if response.Version == nil || *response.Version != AgentVersion {
		t.Fatalf("version = %v, want %q", response.Version, AgentVersion)
	}
	if response.Latency == nil || *response.Latency != 0.2 {
		t.Fatalf("latency = %v, want 0.2", response.Latency)
	}
}

func TestRejectsBadSignature(t *testing.T) {
	server := newTestServer(t, nil)
	frame := buildFrame(t, server, time.Now().Unix(), fixedNonce(), []byte(`{"command":"version"}`))
	frame[0] ^= 0xff

	if body := roundTripNoResponse(t, server, frame); len(body) != 0 {
		t.Fatalf("unexpected response: %q", body)
	}
}

func TestRejectsReplayedNonce(t *testing.T) {
	server := newTestServer(t, nil)
	frame := buildFrame(t, server, time.Now().Unix(), fixedNonce(), []byte(`{"command":"version"}`))
	roundTripFrame(t, server, frame)

	if body := roundTripNoResponse(t, server, frame); len(body) != 0 {
		t.Fatalf("unexpected replay response: %q", body)
	}
}

func newTestServer(t *testing.T, runner CommandRunner) *Server {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	if runner == nil {
		runner = func(context.Context, string, []string, time.Duration) (string, error) {
			t.Fatal("runner should not be called")
			return "", nil
		}
	}
	server, err := NewServer(Config{
		Host:    "127.0.0.1",
		Port:    9000,
		HMACKey: key,
		Runner:  runner,
		Logger:  log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func fixedNonce() []byte {
	nonce := make([]byte, nonceSize)
	for i := range nonce {
		nonce[i] = byte(255 - i)
	}
	return nonce
}

func buildFrame(t *testing.T, server *Server, timestamp int64, nonce []byte, body []byte) []byte {
	t.Helper()
	if len(nonce) != nonceSize {
		t.Fatalf("nonce length = %d", len(nonce))
	}
	tsBuf := make([]byte, tsSize)
	binary.BigEndian.PutUint64(tsBuf, uint64(timestamp))
	frame := make([]byte, headerSize+len(body))
	copy(frame[:sigSize], server.sign(tsBuf, nonce, body))
	copy(frame[sigSize:sigSize+tsSize], tsBuf)
	copy(frame[sigSize+tsSize:sigSize+tsSize+nonceSize], nonce)
	binary.BigEndian.PutUint16(frame[headerSize-2:headerSize], uint16(len(body)))
	copy(frame[headerSize:], body)
	return frame
}

func roundTripFrame(t *testing.T, server *Server, frame []byte) ([]byte, uint64, []byte) {
	t.Helper()
	client, serverConn := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer serverConn.Close()
		server.handleConnection(serverConn, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345})
	}()
	_ = client.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := client.Write(frame); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(client, header); err != nil {
		t.Fatal(err)
	}
	bodyLen := binary.BigEndian.Uint16(header[headerSize-2:])
	body := make([]byte, bodyLen)
	if _, err := io.ReadFull(client, body); err != nil {
		t.Fatal(err)
	}
	sig := header[:sigSize]
	tsBuf := header[sigSize : sigSize+tsSize]
	nonce := append([]byte{}, header[sigSize+tsSize:sigSize+tsSize+nonceSize]...)
	if !hmacEqual(server.sign(tsBuf, nonce, body), sig) {
		t.Fatal("response signature mismatch")
	}
	<-done
	return body, binary.BigEndian.Uint64(tsBuf), nonce
}

func roundTripNoResponse(t *testing.T, server *Server, frame []byte) []byte {
	t.Helper()
	client, serverConn := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer serverConn.Close()
		server.handleConnection(serverConn, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345})
	}()
	_ = client.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := client.Write(frame); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	header := make([]byte, headerSize)
	n, err := client.Read(header)
	if err == nil {
		<-done
		return header[:n]
	}
	<-done
	return nil
}

func hmacEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var same byte
	for i := range a {
		same |= a[i] ^ b[i]
	}
	return same == 0
}

func TestDualStackListenerAcceptsBothIPv4AndIPv6(t *testing.T) {
	key := make([]byte, 32)

	tmpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := tmpLn.Addr().(*net.TCPAddr).Port
	_ = tmpLn.Close()

	server, err := NewServer(Config{
		Host:    "::",
		Port:    port,
		HMACKey: key,
		Runner: func(context.Context, string, []string, time.Duration) (string, error) {
			return "", nil
		},
		Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := server.Listen(context.Background())
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	dialAndAccept := func(addr string) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			conn, err := ln.Accept()
			if err != nil {
				t.Errorf("accept from %s failed: %v", addr, err)
				return
			}
			_ = conn.Close()
		}()

		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial %s failed: %v", addr, err)
		}
		_ = conn.Close()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for accept from %s", addr)
		}
	}

	dialAndAccept(fmt.Sprintf("127.0.0.1:%d", port))
	dialAndAccept(fmt.Sprintf("[::1]:%d", port))
}
