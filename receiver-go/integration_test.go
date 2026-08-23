package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const cargoIntegrationTimeout = 30 * time.Second

func TestRustSenderToGoReceiver(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping external Cargo integration in short mode")
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is not installed")
	}
	listener, done := startTestServer(t)
	defer stopTestServer(t, listener, done)
	port := listener.Addr().(*net.TCPAddr).Port

	tests := []struct {
		name        string
		algorithm   string
		numerator   string
		denominator string
		seed        string
		want        string
	}{
		{"clean CRC", "crc32-iso-hdlc", "0", "1", "42", "Status: ok\nMessage: A"},
		{"corrected Hamming noise vector", "hamming-secded-13-8", "1", "10", "3", "Status: corrected\nMessage: A"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), cargoIntegrationTimeout)
			defer cancel()
			command := commandContext(ctx,
				"cargo", "run", "--quiet", "--manifest-path", "../sender-rust/Cargo.toml", "--",
				"--host", "127.0.0.1", "--port", fmt.Sprint(port), "--message", "A",
				"--algorithm", test.algorithm, "--numerator", test.numerator,
				"--denominator", test.denominator, "--seed", test.seed, "--request-id", "go-rust-integration",
			)
			output, err := command.CombinedOutput()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatalf("sender exceeded %s timeout\n%s", cargoIntegrationTimeout, output)
			}
			if err != nil {
				t.Fatalf("sender failed: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), test.want) {
				t.Fatalf("sender output %q does not contain %q", output, test.want)
			}
		})
	}
}

func TestCommandContextKillsProcessGroupOnTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess timeout integration in short mode")
	}
	marker := filepath.Join(t.TempDir(), "child-survived")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	command := commandContext(ctx, "sh", "-c", `sleep 0.3; touch "$1"`, "sh", marker)
	if err := command.Run(); err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("command error = %v, context error = %v; want deadline exceeded", err, ctx.Err())
	}
	time.Sleep(350 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("timed-out subprocess was not killed: %v", err)
	}
}

func commandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return command
}
