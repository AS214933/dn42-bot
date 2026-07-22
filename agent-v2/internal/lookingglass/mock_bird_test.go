package lookingglass

import (
	"bufio"
	"net"
	"path/filepath"
	"strings"
	"testing"
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
