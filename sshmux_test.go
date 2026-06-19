package devssh

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installFakeSSH writes a fake `ssh` script into a temp dir, prepends it to
// PATH, and returns the directory plus the path to a log file the script
// appends every invocation to. Each line in the log is the args joined by
// "|".
func installFakeSSH(t *testing.T, behavior string) (binDir, logPath string) {
	t.Helper()
	binDir = t.TempDir()
	logPath = filepath.Join(binDir, "invocations.log")

	script := `#!/usr/bin/env bash
log="$0.invocations"
# Resolve the log file path relative to this script.
log="` + logPath + `"
printf '%s\n' "$(IFS='|'; echo "$*")" >> "$log"
` + behavior + `
exit 0
`
	sshPath := filepath.Join(binDir, "ssh")
	if err := os.WriteFile(sshPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}

	prev := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+prev)
	t.Cleanup(func() { os.Setenv("PATH", prev) })

	// Ensure no leftover log.
	_ = os.Remove(logPath)
	return binDir, logPath
}

func readInvocations(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read invocations log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func TestStartMux_BuildsExpectedCommand(t *testing.T) {
	_, logPath := installFakeSSH(t, "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	mux, err := StartMux(context.Background(), "alpha", nil)
	if err != nil {
		t.Fatalf("StartMux: %v", err)
	}
	defer mux.Stop()

	invocations := readInvocations(t, logPath)
	if len(invocations) < 2 {
		t.Fatalf("expected at least 2 ssh invocations (master + check), got %v", invocations)
	}

	master := invocations[0]
	if !strings.Contains(master, "-M") || !strings.Contains(master, "-N") || !strings.Contains(master, "-f") {
		t.Errorf("master invocation missing -M/-N/-f: %s", master)
	}
	if !strings.Contains(master, "ControlMaster=yes") {
		t.Errorf("master invocation missing ControlMaster=yes: %s", master)
	}
	if !strings.HasSuffix(master, "|alpha") {
		t.Errorf("master invocation should end with host alpha: %s", master)
	}

	check := invocations[1]
	if !strings.Contains(check, "-O|check") {
		t.Errorf("expected check invocation, got: %s", check)
	}
}

func TestMux_AddLocalForward(t *testing.T) {
	_, logPath := installFakeSSH(t, "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	mux, err := StartMux(context.Background(), "alpha", nil)
	if err != nil {
		t.Fatalf("StartMux: %v", err)
	}
	defer mux.Stop()

	if err := mux.AddLocalForward(8080, 8080); err != nil {
		t.Fatalf("AddLocalForward: %v", err)
	}

	invocations := readInvocations(t, logPath)
	last := invocations[len(invocations)-1]
	if !strings.Contains(last, "-O|forward") || !strings.Contains(last, "-L|8080:127.0.0.1:8080") {
		t.Errorf("AddLocalForward invocation looks wrong: %s", last)
	}
	if !strings.Contains(last, "ControlMaster=no") {
		t.Errorf("internal invocation must set ControlMaster=no: %s", last)
	}
	if !strings.Contains(last, "BatchMode=yes") {
		t.Errorf("internal invocation must set BatchMode=yes: %s", last)
	}
}

func TestMux_AddRemoteForward_Streamlocal(t *testing.T) {
	_, logPath := installFakeSSH(t, "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	mux, err := StartMux(context.Background(), "alpha", nil)
	if err != nil {
		t.Fatalf("StartMux: %v", err)
	}
	defer mux.Stop()

	if err := mux.AddRemoteForward("/tmp/devssh-browser-abc.sock", "127.0.0.1:5555"); err != nil {
		t.Fatalf("AddRemoteForward: %v", err)
	}

	invocations := readInvocations(t, logPath)
	last := invocations[len(invocations)-1]
	if !strings.Contains(last, "-O|forward") {
		t.Errorf("expected forward subcommand: %s", last)
	}
	if !strings.Contains(last, "/tmp/devssh-browser-abc.sock:127.0.0.1:5555") {
		t.Errorf("expected forward spec, got: %s", last)
	}
}

func TestMux_Stop_IsIdempotent(t *testing.T) {
	_, _ = installFakeSSH(t, "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	mux, err := StartMux(context.Background(), "alpha", nil)
	if err != nil {
		t.Fatalf("StartMux: %v", err)
	}
	if err := mux.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Second call should not panic or return a different error.
	if err := mux.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}
