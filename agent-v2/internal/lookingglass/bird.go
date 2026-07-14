package lookingglass

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

var (
	errBirdOutputLimit = errors.New("BIRD output limit reached")
	errBirdProtocol    = errors.New("invalid BIRD control protocol response")
)

type birdClient struct {
	socketPath     string
	maxOutputBytes int
}

func (c birdClient) Query(ctx context.Context, query string) (string, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", c.socketPath)
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
	greeting, err := readBirdResponse(reader, 0)
	if err != nil {
		return "", fmt.Errorf("read BIRD greeting: %w", err)
	}
	if greeting.terminalCode != "0001" {
		return "", fmt.Errorf("%w: unexpected greeting status %q", errBirdProtocol, greeting.terminalCode)
	}
	if err := writeBirdCommand(conn, "restrict"); err != nil {
		return "", fmt.Errorf("restrict BIRD session: %w", err)
	}
	restriction, err := readBirdResponse(reader, 4096)
	if err != nil {
		return "", fmt.Errorf("verify BIRD restriction: %w", err)
	}
	if restriction.terminalCode != "0016" || !strings.Contains(restriction.output, "Access restricted") {
		return "", errors.New("BIRD did not confirm restricted access")
	}

	if err := writeBirdCommand(conn, query); err != nil {
		return "", fmt.Errorf("write BIRD query: %w", err)
	}
	response, err := readBirdResponse(reader, c.maxOutputBytes)
	if errors.Is(err, errBirdOutputLimit) {
		return response.output, nil
	}
	if err != nil {
		return "", fmt.Errorf("read BIRD query response: %w", err)
	}
	return response.output, nil
}

func writeBirdCommand(conn net.Conn, command string) error {
	value := command + "\n"
	written, err := io.WriteString(conn, value)
	if err == nil && written != len(value) {
		return io.ErrShortWrite
	}
	return err
}

type birdResponse struct {
	output       string
	terminalCode string
}

func readBirdResponse(reader *bufio.Reader, maxOutputBytes int) (birdResponse, error) {
	var response birdResponse
	var output strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return response, err
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")

		code, text, delimiter, prefixed := parseBirdLine(line)
		emit := prefixed && code != "0000"
		if !prefixed {
			switch {
			case strings.HasPrefix(text, " "):
				text = strings.TrimPrefix(text, " ")
				emit = true
			case strings.HasPrefix(text, "+"):
				continue
			default:
				return response, fmt.Errorf("%w: malformed reply line", errBirdProtocol)
			}
		}
		if emit && maxOutputBytes > 0 {
			remaining := maxOutputBytes - output.Len()
			if remaining <= 0 {
				response.output = output.String()
				return response, errBirdOutputLimit
			}
			piece := text + "\n"
			if len(piece) > remaining {
				output.WriteString(piece[:remaining])
				response.output = output.String()
				return response, errBirdOutputLimit
			}
			output.WriteString(piece)
		}

		if prefixed && delimiter != '-' {
			response.output = output.String()
			response.terminalCode = code
			return response, nil
		}
	}
}

func parseBirdLine(line string) (code, text string, delimiter byte, prefixed bool) {
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
