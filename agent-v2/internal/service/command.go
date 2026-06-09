package service

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"
)

func RunCommand(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return "", err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	timer := time.AfterFunc(timeout, func() {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	})

	select {
	case err := <-done:
		timer.Stop()
		if err != nil {
			return buf.String(), err
		}
		return buf.String(), nil
	case <-ctx.Done():
		timer.Stop()
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return "", ctx.Err()
	}
}
