package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Mux owns an OpenSSH ControlMaster connection for the duration of a devssh
// session. All ssh invocations issued through Mux share the same multiplexed
// TCP connection and authenticated session.
//
// Lifecycle: StartMux starts a backgrounded master. Use the helpers
// (Run, AddLocalForward, ...) to issue commands. Call Stop to terminate the
// master and clean up the control socket file.
type Mux struct {
	Host       string
	SocketPath string

	stopOnce sync.Once
	stopErr  error
}

// muxSocketPath returns a per-session, alias-derived socket path short enough
// to fit inside the Unix domain socket name limit (~104 chars on macOS).
// %C-style hashing is used on the alias rather than the full path so we keep
// the file name short.
func muxSocketPath(alias string) (string, error) {
	pid := os.Getpid()
	sum := sha256.Sum256(fmt.Appendf(nil, "%s-%d", alias, pid))
	short := hex.EncodeToString(sum[:6])

	// Prefer XDG_RUNTIME_DIR if set; fall back to $TMPDIR/os.TempDir.
	base := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "devssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create mux dir %s: %w", dir, err)
	}

	return filepath.Join(dir, "cm-"+short+".sock"), nil
}

// StartMux starts a new ControlMaster against host and returns a Mux ready to
// be used. extraOpts are passed through to the master ssh invocation (for
// example, debug flags). The caller must invoke Stop to clean up.
func StartMux(ctx context.Context, host string, extraOpts []string) (*Mux, error) {
	socket, err := muxSocketPath(host)
	if err != nil {
		return nil, err
	}

	// Stale socket from a previous run will block bind; try to evict.
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		logDebug("remove stale mux socket %s: %v", socket, err)
	}

	args := []string{
		"-M",
		"-N",
		"-f",
		"-S", socket,
		"-o", "ControlMaster=yes",
		"-o", "ControlPath=" + socket,
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
	}
	args = append(args, extraOpts...)
	args = append(args, host)

	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	start := time.Now()
	if err := cmd.Run(); err != nil {
		logElapsed("ssh ControlMaster command", start)
		return nil, fmt.Errorf("start ssh ControlMaster for %s: %w (stderr: %s)", host, err, strings.TrimSpace(stderr.String()))
	}
	logElapsed("ssh ControlMaster command", start)

	m := &Mux{Host: host, SocketPath: socket}

	start = time.Now()
	if err := m.Check(); err != nil {
		logElapsed("ssh ControlMaster check", start)
		if stopErr := m.Stop(); stopErr != nil {
			logDebug("stop failed after ControlMaster check error: %v", stopErr)
		}
		return nil, fmt.Errorf("ControlMaster did not become ready: %w", err)
	}
	logElapsed("ssh ControlMaster check", start)

	return m, nil
}

// muxBaseOpts returns the SSH options applied to every command run against
// the established master socket.
func (m *Mux) muxBaseOpts() []string {
	return []string{
		"-S", m.SocketPath,
		"-o", "ControlMaster=no",
		"-o", "ControlPath=" + m.SocketPath,
		"-o", "BatchMode=yes",
	}
}

// Check verifies the master is reachable using `ssh -O check`.
func (m *Mux) Check() error {
	args := append(m.muxBaseOpts(), "-O", "check", m.Host)
	cmd := exec.Command("ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh -O check: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Command returns an *exec.Cmd that runs the given remote command through the
// established master. The caller is responsible for setting up stdio and
// invoking Run/Start/Wait. The supplied context is propagated to the command.
func (m *Mux) Command(ctx context.Context, remoteArgs ...string) *exec.Cmd {
	args := append(m.muxBaseOpts(), m.Host)
	args = append(args, remoteArgs...)
	return exec.CommandContext(ctx, "ssh", args...)
}

// Run executes a remote command and returns its captured stdout and stderr.
func (m *Mux) Run(ctx context.Context, remoteArgs ...string) (stdout, stderr string, err error) {
	cmd := m.Command(ctx, remoteArgs...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

// AddLocalForward establishes a local-to-remote TCP forward through the
// master: `ssh -O forward -L localPort:127.0.0.1:remotePort host`.
func (m *Mux) AddLocalForward(localPort, remotePort int) error {
	spec := fmt.Sprintf("%d:127.0.0.1:%d", localPort, remotePort)
	args := append(m.muxBaseOpts(), "-O", "forward", "-L", spec, m.Host)
	cmd := exec.Command("ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh -O forward -L %s: %w (stderr: %s)", spec, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// CancelLocalForward removes a previously-added local-to-remote forward.
func (m *Mux) CancelLocalForward(localPort, remotePort int) error {
	spec := fmt.Sprintf("%d:127.0.0.1:%d", localPort, remotePort)
	args := append(m.muxBaseOpts(), "-O", "cancel", "-L", spec, m.Host)
	cmd := exec.Command("ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh -O cancel -L %s: %w (stderr: %s)", spec, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// AddRemoteForward establishes a remote-to-local forward. remoteSpec is the
// remote endpoint (TCP port number, or absolute Unix socket path);
// localSpec is the local endpoint forwarded to. Example values:
//
//	remoteSpec="8080",                       localSpec="127.0.0.1:8080"
//	remoteSpec="/tmp/devssh-browser-x.sock", localSpec="127.0.0.1:5555"
func (m *Mux) AddRemoteForward(remoteSpec, localSpec string) error {
	spec := remoteSpec + ":" + localSpec
	args := append(m.muxBaseOpts(), "-O", "forward", "-R", spec, m.Host)
	cmd := exec.Command("ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh -O forward -R %s: %w (stderr: %s)", spec, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// CancelRemoteForward removes a previously-added remote-to-local forward.
func (m *Mux) CancelRemoteForward(remoteSpec, localSpec string) error {
	spec := remoteSpec + ":" + localSpec
	args := append(m.muxBaseOpts(), "-O", "cancel", "-R", spec, m.Host)
	cmd := exec.Command("ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh -O cancel -R %s: %w (stderr: %s)", spec, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// InteractiveShell hands control to an interactive ssh session over the
// existing master. extraArgs are appended after the host argument; they may
// contain user-supplied ssh flags or a remote command.
//
// stdio is wired straight through to the calling process.
func (m *Mux) InteractiveShell(ctx context.Context, extraArgs []string) error {
	args := []string{
		"-S", m.SocketPath,
		"-o", "ControlMaster=no",
		"-o", "ControlPath=" + m.SocketPath,
		"-t",
		m.Host,
	}
	args = append(args, extraArgs...)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Stop terminates the master via `ssh -O exit` and removes the control socket.
// Safe to call multiple times.
func (m *Mux) Stop() error {
	m.stopOnce.Do(func() {
		args := append(m.muxBaseOpts(), "-O", "exit", m.Host)
		cmd := exec.Command("ssh", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			// Don't fail loudly: master may already be gone if the
			// interactive session exited. Surface as a soft error.
			m.stopErr = fmt.Errorf("ssh -O exit: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
		}
		if err := os.Remove(m.SocketPath); err != nil && !os.IsNotExist(err) {
			logDebug("remove mux socket %s: %v", m.SocketPath, err)
		}
	})
	return m.stopErr
}
