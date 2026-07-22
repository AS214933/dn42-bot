package birdctl

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type mockBirdServer struct {
	socket   string
	commands <-chan string
	errors   <-chan error
}

func startMockBirdServer(t *testing.T, restriction, queryResponse string) mockBirdServer {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "bird.ctl"))
	if err != nil {
		t.Fatal(err)
	}
	commands := make(chan string, 2)
	errors := make(chan error, 1)

	go func() {
		defer close(commands)
		conn, err := listener.Accept()
		if err != nil {
			errors <- err
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		if _, err := conn.Write([]byte("0001 BIRD 2.16.1 ready.\n")); err != nil {
			errors <- err
			return
		}
		command, err := reader.ReadString('\n')
		if err != nil {
			errors <- err
			return
		}
		commands <- strings.TrimSpace(command)
		if _, err := conn.Write([]byte(restriction)); err != nil {
			errors <- err
			return
		}
		command, err = reader.ReadString('\n')
		if err != nil {
			return
		}
		commands <- strings.TrimSpace(command)
		if _, err := conn.Write([]byte(queryResponse)); err != nil {
			errors <- err
		}
	}()

	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case err := <-errors:
			if err != nil {
				t.Errorf("mock BIRD server: %v", err)
			}
		default:
		}
	})

	return mockBirdServer{socket: listener.Addr().String(), commands: commands, errors: errors}
}

func TestBirdClientRestrictsBeforeQuery(t *testing.T) {
	t.Parallel()
	server := startMockBirdServer(t,
		"0016 Access restricted\n",
		"2002-Name       Proto      Table      State  Since         Info\n Name continuation\n0000 \n",
	)
	client := Client{SocketPath: server.socket, MaxOutputBytes: 64 * 1024, Restrict: true}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	output, err := client.Query(ctx, "show protocols")
	if err != nil {
		t.Fatal(err)
	}
	if output != "Name       Proto      Table      State  Since         Info\nName continuation\n" {
		t.Fatalf("unexpected output %q", output)
	}
	if got := <-server.commands; got != "restrict" {
		t.Fatalf("first command = %q, want restrict", got)
	}
	if got := <-server.commands; got != "show protocols" {
		t.Fatalf("second command = %q, want query", got)
	}
}

func TestBirdClientRefusesUnconfirmedRestriction(t *testing.T) {
	t.Parallel()
	server := startMockBirdServer(t, "0017 Restriction unavailable\n", "0000 should not be sent\n")
	client := Client{SocketPath: server.socket, MaxOutputBytes: 1024, Restrict: true}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := client.Query(ctx, "show protocols"); err == nil {
		t.Fatal("expected restriction verification failure")
	}
	if got := <-server.commands; got != "restrict" {
		t.Fatalf("first command = %q, want restrict", got)
	}
	if query, ok := <-server.commands; ok {
		t.Fatalf("query was sent without a verified restriction: %q", query)
	}
}

func TestBirdClientLimitsOutput(t *testing.T) {
	t.Parallel()
	server := startMockBirdServer(t, "0016 Access restricted\n", "1000-"+strings.Repeat("x", 100)+"\n0000 \n")
	client := Client{SocketPath: server.socket, MaxOutputBytes: 16, Restrict: true}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	output, err := client.Query(ctx, "show route")
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 16 {
		t.Fatalf("output length = %d, want 16", len(output))
	}
}

func TestBirdClientDefaultSizeOutputLimit(t *testing.T) {
	t.Parallel()
	const limit = 64 * 1024
	server := startMockBirdServer(t, "0016 Access restricted\n", "1000-"+strings.Repeat("x", limit+100)+"\n0000 \n")
	client := Client{SocketPath: server.socket, MaxOutputBytes: limit, Restrict: true}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	output, err := client.Query(ctx, "show route")
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != limit {
		t.Fatalf("output length = %d, want %d", len(output), limit)
	}
}

func TestBirdClientHonorsDeadline(t *testing.T) {
	t.Parallel()
	server := startMockBirdServer(t, "", "")
	client := Client{SocketPath: server.socket, MaxOutputBytes: 1024, Restrict: true}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if _, err := client.Query(ctx, "show protocols"); err == nil {
		t.Fatal("expected BIRD deadline error")
	}
}

func TestBirdClientReturnsBirdErrorText(t *testing.T) {
	t.Parallel()
	server := startMockBirdServer(t, "0016 Access restricted\n", "9001 syntax error\n")
	client := Client{SocketPath: server.socket, MaxOutputBytes: 1024, Restrict: true}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	output, err := client.Query(ctx, "show route")
	if err != nil {
		t.Fatal(err)
	}
	if output != "syntax error\n" {
		t.Fatalf("output = %q, want BIRD error body", output)
	}
}

func TestBirdResponsePreservesBlankContinuation(t *testing.T) {
	t.Parallel()
	reader := bufio.NewReader(strings.NewReader("1000-first\n \n0000 \n"))
	response, err := readResponse(reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if response.output != "first\n\n" {
		t.Fatalf("output = %q, want blank continuation line", response.output)
	}
}

func TestBirdResponseRejectsMalformedLine(t *testing.T) {
	t.Parallel()
	reader := bufio.NewReader(strings.NewReader("malformed\n"))
	if _, err := readResponse(reader, 1024); !errors.Is(err, ErrProtocol) {
		t.Fatalf("error = %v, want ErrProtocol", err)
	}
}

type byteAtATimeReader struct {
	reader io.Reader
}

func (r byteAtATimeReader) Read(value []byte) (int, error) {
	if len(value) > 1 {
		value = value[:1]
	}
	return r.reader.Read(value)
}

func TestBirdResponseHandlesFragmentedReads(t *testing.T) {
	t.Parallel()
	fragmented := byteAtATimeReader{reader: strings.NewReader("1000-first\n second\n0000 \n")}
	response, err := readResponse(bufio.NewReader(fragmented), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if response.output != "first\nsecond\n" || response.terminalCode != "0000" {
		t.Fatalf("response = %+v", response)
	}
}

type shortWriteConn struct {
	net.Conn
}

func (shortWriteConn) Write(value []byte) (int, error) {
	return len(value) - 1, nil
}

func TestWriteBirdCommandDetectsShortWrite(t *testing.T) {
	t.Parallel()
	if err := writeCommand(shortWriteConn{}, "show protocols"); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want io.ErrShortWrite", err)
	}
}

func TestParseBirdLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line       string
		code, text string
		delimiter  byte
		prefixed   bool
	}{
		{line: "0000 done", code: "0000", text: "done", delimiter: ' ', prefixed: true},
		{line: "1000-more", code: "1000", text: "more", delimiter: '-', prefixed: true},
		{line: " continuation", text: " continuation"},
		{line: "123x invalid", text: "123x invalid"},
	}
	for _, test := range tests {
		code, text, delimiter, prefixed := parseLine(test.line)
		if code != test.code || text != test.text || delimiter != test.delimiter || prefixed != test.prefixed {
			t.Errorf("parseLine(%q) = (%q, %q, %q, %v)", test.line, code, text, delimiter, prefixed)
		}
	}
}
