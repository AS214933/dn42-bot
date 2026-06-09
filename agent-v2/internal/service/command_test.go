package service

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunCommandSuccess(t *testing.T) {
	t.Parallel()
	output, err := RunCommand(context.Background(), "echo", []string{"hello"}, 5*time.Second)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if strings.TrimSpace(output) != "hello" {
		t.Fatalf("expected 'hello', got: %q", output)
	}
}

func TestRunCommandNonZeroExit(t *testing.T) {
	t.Parallel()
	_, err := RunCommand(context.Background(), "sh", []string{"-c", "exit 42"}, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for non-zero exit code, got nil")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *exec.ExitError, got %T", err)
	}
	if exitErr.ExitCode() != 42 {
		t.Fatalf("expected exit code 42, got %d", exitErr.ExitCode())
	}
}

func TestRunCommandTimeout(t *testing.T) {
	t.Parallel()
	_, err := RunCommand(context.Background(), "sleep", []string{"10"}, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestRunCommandTimeoutKillsProcessGroup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		_, err := RunCommand(ctx, "sleep", []string{"10"}, 500*time.Millisecond)
		done <- err
	}()

	err := <-done
	if err == nil {
		t.Fatal("expected timeout error")
	}

	time.Sleep(200 * time.Millisecond)

	check, _ := RunCommand(context.Background(), "pgrep", []string{"-f", "sleep 10"}, 2*time.Second)
	if strings.TrimSpace(check) != "" {
		t.Fatalf("orphan 'sleep 10' process still running: %s", check)
	}
}

func TestRunCommandCtxCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err := RunCommand(ctx, "sleep", []string{"10"}, 30*time.Second)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestRunCommandEmptyArgs(t *testing.T) {
	t.Parallel()
	output, err := RunCommand(context.Background(), "echo", []string{}, 5*time.Second)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if strings.TrimSpace(output) != "" {
		t.Fatalf("expected empty output, got: %q", output)
	}
}

func TestRunCommandCombinedOutput(t *testing.T) {
	t.Parallel()
	output, err := RunCommand(context.Background(), "sh", []string{"-c", "echo out; echo err >&2"}, 5*time.Second)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(output, "out") || !strings.Contains(output, "err") {
		t.Fatalf("expected combined stdout+stderr, got: %q", output)
	}
}

func TestRunCommandInvalidCommand(t *testing.T) {
	t.Parallel()
	_, err := RunCommand(context.Background(), "nonexistent_command_xyz", []string{}, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for nonexistent command, got nil")
	}
}
