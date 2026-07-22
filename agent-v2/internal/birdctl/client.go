// Package birdctl talks to BIRD over its UNIX control socket.
// Protocol framing follows go-bird / bird-lg-go style status-number replies.
package birdctl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	DefaultSocketPath  = "/var/run/bird/bird.ctl"
	defaultMaxOutput   = 64 * 1024
	defaultDialTimeout = 5 * time.Second
)

var (
	ErrOutputLimit = errors.New("BIRD output limit reached")
	ErrProtocol    = errors.New("invalid BIRD control protocol response")
)

// Client is a one-shot BIRD control-socket client.
// Each Query dials the socket, optionally restricts the session, runs one
// command, and closes the connection.
type Client struct {
	SocketPath     string
	MaxOutputBytes int
	// Restrict enables "restrict" before the command (looking-glass mode).
	Restrict bool
}

// Query connects to the BIRD control socket and runs a single command.
// The returned string is the human-readable reply body with status codes
// stripped. Transport and framing failures return a non-nil error.
func Query(ctx context.Context, socketPath, command string) (string, error) {
	return Client{SocketPath: socketPath}.Query(ctx, command)
}

// QueryRestricted is like Query but enters restricted mode first.
func QueryRestricted(ctx context.Context, socketPath, command string) (string, error) {
	return Client{SocketPath: socketPath, Restrict: true}.Query(ctx, command)
}

func (c Client) Query(ctx context.Context, command string) (string, error) {
	socketPath := strings.TrimSpace(c.SocketPath)
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("empty BIRD command")
	}
	if strings.ContainsAny(command, "\r\n") {
		return "", errors.New("BIRD command must be a single line")
	}

	maxOutput := c.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = defaultMaxOutput
	}

	dialer := net.Dialer{}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultDialTimeout)
		defer cancel()
	}

	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return "", fmt.Errorf("connect to BIRD: %w", err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return "", fmt.Errorf("set BIRD deadline: %w", err)
		}
	}

	reader := bufio.NewReader(conn)
	greeting, err := readResponse(reader, 0)
	if err != nil {
		return "", fmt.Errorf("read BIRD greeting: %w", err)
	}
	if greeting.terminalCode != "0001" {
		return "", fmt.Errorf("%w: unexpected greeting status %q", ErrProtocol, greeting.terminalCode)
	}

	if c.Restrict {
		if err := writeCommand(conn, "restrict"); err != nil {
			return "", fmt.Errorf("restrict BIRD session: %w", err)
		}
		restriction, err := readResponse(reader, 4096)
		if err != nil {
			return "", fmt.Errorf("verify BIRD restriction: %w", err)
		}
		if restriction.terminalCode != "0016" || !strings.Contains(restriction.output, "Access restricted") {
			return "", errors.New("BIRD did not confirm restricted access")
		}
	}

	if err := writeCommand(conn, command); err != nil {
		return "", fmt.Errorf("write BIRD command: %w", err)
	}
	response, err := readResponse(reader, maxOutput)
	if errors.Is(err, ErrOutputLimit) {
		return response.output, nil
	}
	if err != nil {
		return "", fmt.Errorf("read BIRD command response: %w", err)
	}
	return response.output, nil
}

func writeCommand(conn net.Conn, command string) error {
	value := command + "\n"
	written, err := io.WriteString(conn, value)
	if err == nil && written != len(value) {
		return io.ErrShortWrite
	}
	return err
}

type response struct {
	output       string
	terminalCode string
}

func readResponse(reader *bufio.Reader, maxOutputBytes int) (response, error) {
	var resp response
	var output strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return resp, err
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")

		code, text, delimiter, prefixed := parseLine(line)
		emit := prefixed && code != "0000"
		if !prefixed {
			switch {
			case strings.HasPrefix(text, " "):
				text = strings.TrimPrefix(text, " ")
				emit = true
			case strings.HasPrefix(text, "+"):
				// table continuation marker
			default:
				return resp, fmt.Errorf("%w: malformed reply line", ErrProtocol)
			}
		}
		if emit && maxOutputBytes > 0 {
			remaining := maxOutputBytes - output.Len()
			if remaining <= 0 {
				resp.output = output.String()
				return resp, ErrOutputLimit
			}
			piece := text + "\n"
			if len(piece) > remaining {
				output.WriteString(piece[:remaining])
				resp.output = output.String()
				return resp, ErrOutputLimit
			}
			output.WriteString(piece)
		} else if emit && maxOutputBytes == 0 {
			// unlimited (used for greeting)
			output.WriteString(text + "\n")
		}

		if prefixed && delimiter != '-' {
			resp.output = output.String()
			resp.terminalCode = code
			return resp, nil
		}
	}
}

func parseLine(line string) (code, text string, delimiter byte, prefixed bool) {
	if len(line) < 4 || !isFourDigits(line[:4]) {
		return "", line, 0, false
	}
	if len(line) == 4 {
		return line[:4], "", ' ', true
	}
	if line[4] != ' ' && line[4] != '-' {
		return "", line, 0, false
	}
	return line[:4], line[5:], line[4], true
}

func isFourDigits(value string) bool {
	if len(value) != 4 {
		return false
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
