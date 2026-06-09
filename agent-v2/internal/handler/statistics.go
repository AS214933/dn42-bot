package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type CommandRunner func(ctx context.Context, name string, args []string, timeout time.Duration) (string, error)

func PingHandler(runner CommandRunner) http.HandlerFunc {
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

		output, err := runner(ctx, "ping", []string{"-c", "5", "-w", "6", target}, 8*time.Second)
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

func TraceHandler(runner CommandRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		target := readBodyTarget(w, r)
		if target == "" {
			return
		}

		output, err := runTrace(r.Context(), runner, target)
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

func runTrace(ctx context.Context, runner CommandRunner, target string) (string, error) {
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

func TCPingHandler(runner CommandRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		target := readBodyTarget(w, r)
		if target == "" {
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
