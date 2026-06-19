package devssh

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Options configures a devssh session started by NewSession or Run.
//
// Host is the OpenSSH host alias or target to pass to ssh. It may be any
// normal OpenSSH destination, including a Host entry generated in an SSH config
// file by another tool. SSHOptions are appended to the ControlMaster ssh
// command before Host, which is where callers can pass inputs such as
// "-F", "/path/to/config". SSHArgs are appended to the final interactive ssh
// command after Host and may contain extra ssh flags or a remote command.
type Options struct {
	Host       string
	SSHOptions []string
	SSHArgs    []string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	DisableBrowser                    bool
	DisableNotifications              bool
	DisablePortMonitor                bool
	DisableXdgOpen                    bool
	DisableDefaultReversePortForwards bool
	InstallXdgOpen                    bool
	ReversePortForwards               []ReversePortForward
	Verbose                           bool
}

// DefaultOptions returns Options populated with the same feature defaults used
// by the devssh CLI. The returned value can be modified before calling Run.
func DefaultOptions(host string) Options {
	return Options{
		Host:   strings.TrimSpace(host),
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Session owns a live devssh ControlMaster and all helper services/forwards
// started for a host. Call Close when the session is no longer needed.
type Session struct {
	options         Options
	mux             *Mux
	browserSvc      *BrowserService
	notifySvc       *NotificationService
	monitor         *PortMonitorController
	reverseForwards []ReversePortForward
	logDir          string
	closeOnce       sync.Once
	closeErr        error
}

var sessionStateMu sync.Mutex

// NewSession starts the devssh ControlMaster, remote preflight, local helper
// services, remote forwards, helper script upload, and optional port monitor.
// It does not start the final interactive ssh command; call Interactive for
// that, or use Run to do both with automatic cleanup.
func NewSession(ctx context.Context, opts Options) (*Session, error) {
	opts = normalizeOptions(opts)
	if opts.Host == "" {
		return nil, fmt.Errorf("host is required")
	}

	sessionStateMu.Lock()
	initializeSessionID(opts.Host)
	if err := initDebugLogger(); err != nil {
		fmt.Fprintf(opts.Stderr, "Warning: failed to initialize debug logger: %v\n", err)
	}

	s := &Session{
		options:         opts,
		reverseForwards: reverseForwardsForOptions(opts),
		logDir:          getSessionLogDirectory(),
	}

	logDebug("devssh starting for host %q", opts.Host)
	startupStart := time.Now()
	if opts.Verbose {
		fmt.Fprintf(opts.Stderr, "Logs: %s\n", s.logDir)
	}

	if err := s.start(ctx); err != nil {
		if closeErr := s.Close(); closeErr != nil {
			logDebug("cleanup after startup error: %v", closeErr)
		}
		return nil, err
	}
	logElapsed("startup before interactive shell", startupStart)
	return s, nil
}

// Run starts a devssh session, runs the final interactive ssh invocation, and
// closes the session before returning.
func Run(ctx context.Context, opts Options) error {
	session, err := NewSession(ctx, opts)
	if err != nil {
		return err
	}
	defer func() {
		if err := session.Close(); err != nil {
			logDebug("session.Close: %v", err)
		}
	}()
	return session.Interactive(ctx)
}

// Interactive hands control to the final ssh invocation using the session's
// configured SSHArgs and stdio.
func (s *Session) Interactive(ctx context.Context) error {
	if s == nil || s.mux == nil {
		return fmt.Errorf("session is not started")
	}
	return s.mux.InteractiveShellWithStdio(ctx, s.options.SSHArgs, s.options.Stdin, s.options.Stdout, s.options.Stderr)
}

// Close stops the port monitor, local helper services, ControlMaster, and
// debug log. It is safe to call more than once for the underlying Mux.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		defer sessionStateMu.Unlock()
		if s.monitor != nil {
			s.monitor.Stop()
			s.monitor.Wait()
			s.monitor = nil
		}
		if s.notifySvc != nil {
			s.notifySvc.Stop()
			s.notifySvc = nil
		}
		if s.browserSvc != nil {
			s.browserSvc.Stop()
			s.browserSvc = nil
		}

		if s.mux != nil {
			s.closeErr = s.mux.Stop()
			s.mux = nil
		}
		closeDebugLogger()
	})
	return s.closeErr
}

// LogDirectory returns the session-specific directory containing debug logs.
func (s *Session) LogDirectory() string {
	if s == nil {
		return ""
	}
	return s.logDir
}

// Mux returns the underlying OpenSSH ControlMaster wrapper for advanced
// callers that need to run additional commands or manage forwards directly.
func (s *Session) Mux() *Mux {
	if s == nil {
		return nil
	}
	return s.mux
}

func (s *Session) start(ctx context.Context) error {
	fmt.Fprintf(s.options.Stderr, "Connecting to %s...\n", s.options.Host)
	start := time.Now()
	mux, err := StartMux(ctx, s.options.Host, s.options.SSHOptions)
	logElapsed("start ssh master", start)
	if err != nil {
		return fmt.Errorf("start ssh master: %w", err)
	}
	s.mux = mux

	start = time.Now()
	preflightRemote(ctx, mux, s.options.Stderr)
	logElapsed("remote preflight", start)

	if !s.options.DisableBrowser {
		start = time.Now()
		if svc, err := NewBrowserServiceWithStderr(ctx, s.options.Stderr); err != nil {
			fmt.Fprintf(s.options.Stderr, "Warning: failed to start browser service: %v\n", err)
		} else {
			s.browserSvc = svc
		}
		logElapsed("start browser service", start)
	}

	if !s.options.DisableNotifications {
		start = time.Now()
		if svc, err := NewNotificationService(ctx); err != nil {
			fmt.Fprintf(s.options.Stderr, "Warning: failed to start notification service: %v\n", err)
		} else {
			s.notifySvc = svc
		}
		logElapsed("start notification service", start)
	}

	if s.browserSvc != nil {
		start = time.Now()
		if err := mux.AddRemoteForward(s.browserSvc.SocketPath, fmt.Sprintf("127.0.0.1:%d", s.browserSvc.Port)); err != nil {
			fmt.Fprintf(s.options.Stderr, "Warning: failed to forward browser socket (browser opening disabled): %v\n", err)
			s.browserSvc.Stop()
			s.browserSvc = nil
		}
		logElapsed("forward browser socket", start)
	}
	if s.notifySvc != nil {
		start = time.Now()
		if err := mux.AddRemoteForward(s.notifySvc.SocketPath, fmt.Sprintf("127.0.0.1:%d", s.notifySvc.Port)); err != nil {
			fmt.Fprintf(s.options.Stderr, "Warning: failed to forward notification socket (notifications disabled): %v\n", err)
			s.notifySvc.Stop()
			s.notifySvc = nil
		}
		logElapsed("forward notification socket", start)
	}

	start = time.Now()
	boundForwards := GetBoundReverseForwardsFrom(s.reverseForwards)
	logElapsed("probe reverse forward ports", start)
	if len(boundForwards) > 0 {
		LogReverseForwardsTo(s.options.Stderr, boundForwards)
		for _, fw := range boundForwards {
			remote := fw.RemoteSpec()
			local := fw.LocalSpec()
			start = time.Now()
			if err := mux.AddRemoteForward(remote, local); err != nil {
				fmt.Fprintf(s.options.Stderr, "Warning: failed to forward %s to %s: %v\n", fw.localLabel(), fw.remoteLabel(), err)
			}
			logElapsed(fmt.Sprintf("forward reverse %s to %s", fw.localLabel(), fw.remoteLabel()), start)
		}
	}

	start = time.Now()
	if err := prepareRemoteScripts(ctx, mux, prepareOpts{
		hasBrowser:      s.browserSvc != nil,
		hasNotification: s.notifySvc != nil,
		installXdgOpen:  s.options.InstallXdgOpen,
		uploadXdgOpen:   !s.options.DisableXdgOpen,
	}, s.options.Stderr); err != nil {
		fmt.Fprintf(s.options.Stderr, "Warning: failed to upload helper scripts: %v\n", err)
	}
	logElapsed("prepare remote scripts", start)

	if s.notifySvc != nil {
		fmt.Fprintf(s.options.Stderr, "Command completion notifications available! To enable, add to your shell config:\n")
		fmt.Fprintf(s.options.Stderr, "  # bash (~/.bashrc) or zsh (~/.zshrc)\n")
		fmt.Fprintf(s.options.Stderr, "  if [ -f \"$HOME/notification-sender.sh\" ]; then\n")
		fmt.Fprintf(s.options.Stderr, "      source \"$HOME/notification-sender.sh\"\n")
		fmt.Fprintf(s.options.Stderr, "  fi\n")
		fmt.Fprintf(s.options.Stderr, "  # fish with the done plugin (~/.config/fish/config.fish)\n")
		fmt.Fprintf(s.options.Stderr, "  set -U __done_allow_nongraphical 1\n")
		fmt.Fprintf(s.options.Stderr, "  set -U __done_notification_command \"~/notification-sender.sh send \\$title \\$message\"\n\n")
	}

	if !s.options.DisablePortMonitor {
		start = time.Now()
		monitor, err := StartPortMonitor(ctx, mux, s.reverseForwards)
		if err != nil {
			fmt.Fprintf(s.options.Stderr, "Warning: failed to start port monitor: %v\n", err)
		} else {
			s.monitor = monitor
		}
		logElapsed("start port monitor", start)
	}

	return nil
}

func normalizeOptions(opts Options) Options {
	opts.Host = strings.TrimSpace(opts.Host)
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	opts.SSHOptions = append([]string(nil), opts.SSHOptions...)
	opts.SSHArgs = append([]string(nil), opts.SSHArgs...)
	opts.ReversePortForwards = append([]ReversePortForward(nil), opts.ReversePortForwards...)
	return opts
}

func reverseForwardsForOptions(opts Options) []ReversePortForward {
	if opts.DisableDefaultReversePortForwards {
		return MergeReversePortForwards(opts.ReversePortForwards)
	}
	return MergeReversePortForwards(DefaultReversePortForwards(), opts.ReversePortForwards)
}

// preflightRemote runs a single SSH command to check for the optional remote
// tools we depend on. Missing tools are warned-about but never fatal.
func preflightRemote(ctx context.Context, mux *Mux, stderr io.Writer) {
	const probe = "for t in bash jq ss curl base64 chmod; do command -v $t >/dev/null 2>&1 || echo MISSING:$t; done"
	stdout, _, err := mux.Run(ctx, "sh", "-c", "'"+probe+"'")
	if err != nil {
		logDebug("preflight failed: %v", err)
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.HasPrefix(line, "MISSING:") {
			tool := strings.TrimPrefix(line, "MISSING:")
			fmt.Fprintf(stderr, "Warning: remote is missing %q; some features may be disabled.\n", tool)
		}
	}
}

type prepareOpts struct {
	hasBrowser      bool
	hasNotification bool
	installXdgOpen  bool
	uploadXdgOpen   bool
}

const (
	xdgOpenNotOnPathMarker      = "DEVSSH_XDG_OPEN_NOT_ON_PATH:"
	xdgOpenUserLinkExistsMarker = "DEVSSH_XDG_OPEN_USER_LINK_EXISTS:"
)

func buildXdgOpenUserInstallCommand() string {
	return fmt.Sprintf(`(mkdir -p "$HOME/.local/bin" && if [ ! -e "$HOME/.local/bin/xdg-open" ] || [ -L "$HOME/.local/bin/xdg-open" ]; then ln -sfn "$HOME/xdg-open.sh" "$HOME/.local/bin/xdg-open"; else printf '%%s\n' "%s$HOME/.local/bin/xdg-open"; fi)`, xdgOpenUserLinkExistsMarker)
}

func buildXdgOpenPathCheckCommand() string {
	return fmt.Sprintf(`(resolved="$(command -v xdg-open 2>/dev/null || true)"; if [ -z "$resolved" ] || ! { [ "$HOME/xdg-open.sh" -ef "$resolved" ] 2>/dev/null; }; then printf '%%s\n' "%s${resolved:-<missing>}"; fi)`, xdgOpenNotOnPathMarker)
}

func parseXdgOpenSetupStdout(stdout string) (userLinkConflict, resolvedPath string) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, xdgOpenUserLinkExistsMarker) {
			userLinkConflict = strings.TrimPrefix(line, xdgOpenUserLinkExistsMarker)
		}
		if strings.HasPrefix(line, xdgOpenNotOnPathMarker) {
			resolvedPath = strings.TrimPrefix(line, xdgOpenNotOnPathMarker)
		}
	}
	return userLinkConflict, resolvedPath
}

func buildXdgOpenInstallCommand(binDir string) string {
	cleanBinDir := strings.TrimRight(binDir, "/")
	if cleanBinDir == "" {
		cleanBinDir = "/"
	}

	linkPath := cleanBinDir + "/xdg-open"
	if cleanBinDir == "/" {
		linkPath = "/xdg-open"
	}

	quotedBinDir := quoteForShell(cleanBinDir)
	quotedLinkPath := quoteForShell(linkPath)
	return fmt.Sprintf(`(if [ -d %[1]s ] && [ -w %[1]s ]; then ln -sfn "$HOME/xdg-open.sh" %[2]s; else sudo -n mkdir -p %[1]s && sudo -n ln -sfn "$HOME/xdg-open.sh" %[2]s; fi) && test -L %[2]s && [ "$HOME/xdg-open.sh" -ef %[2]s ]`, quotedBinDir, quotedLinkPath)
}

// prepareRemoteScripts writes all helper scripts to the remote in a single
// SSH call using base64-encoded payloads.
func prepareRemoteScripts(ctx context.Context, mux *Mux, opts prepareOpts, stderr io.Writer) error {
	var cmdParts []string

	portB64 := base64.StdEncoding.EncodeToString([]byte(portMonitorScript))
	cmdParts = append(cmdParts, fmt.Sprintf("printf %%s %s | base64 -d > ~/port-monitor.sh", portB64))

	if opts.hasBrowser {
		b := base64.StdEncoding.EncodeToString([]byte(browserOpenerScript))
		cmdParts = append(cmdParts, fmt.Sprintf("printf %%s %s | base64 -d > ~/browser-opener.sh", b))
	}

	if opts.hasNotification {
		b := base64.StdEncoding.EncodeToString([]byte(notificationSenderScript))
		cmdParts = append(cmdParts, fmt.Sprintf("printf %%s %s | base64 -d > ~/notification-sender.sh", b))
	}

	if opts.uploadXdgOpen {
		b := base64.StdEncoding.EncodeToString([]byte(xdgOpenScript))
		cmdParts = append(cmdParts, fmt.Sprintf("printf %%s %s | base64 -d > ~/xdg-open.sh", b))
	}

	chmodFiles := "~/port-monitor.sh"
	if opts.hasBrowser {
		chmodFiles += " ~/browser-opener.sh"
	}
	if opts.hasNotification {
		chmodFiles += " ~/notification-sender.sh"
	}
	if opts.uploadXdgOpen {
		chmodFiles += " ~/xdg-open.sh"
	}
	cmdParts = append(cmdParts, "chmod +x "+chmodFiles)

	if opts.uploadXdgOpen {
		cmdParts = append(cmdParts, buildXdgOpenUserInstallCommand())
	}

	if opts.installXdgOpen && opts.uploadXdgOpen {
		cmdParts = append(cmdParts, buildXdgOpenInstallCommand("/usr/local/bin"))
	}
	if opts.uploadXdgOpen {
		cmdParts = append(cmdParts, buildXdgOpenPathCheckCommand())
	}

	if cleanup := buildStaleSocketCleanupCommand(opts.hasBrowser, opts.hasNotification); cleanup != "" {
		cmdParts = append(cmdParts, cleanup)
	}

	fullCmd := strings.Join(cmdParts, " && ")

	wrapped := wrapBashLoginCommand(fullCmd)
	stdout, sshStderr, err := mux.Run(ctx, wrapped...)
	if err != nil {
		return fmt.Errorf("prepare remote scripts: %w (stdout: %s, stderr: %s)", err,
			strings.TrimSpace(stdout), strings.TrimSpace(sshStderr))
	}

	xdgOpenUserLinkConflict, xdgOpenResolvedPath := parseXdgOpenSetupStdout(stdout)

	fmt.Fprintln(stderr, "Helper scripts uploaded.")
	if opts.uploadXdgOpen {
		if xdgOpenUserLinkConflict == "" {
			fmt.Fprintln(stderr, "xdg-open shim installed at ~/.local/bin/xdg-open")
		} else {
			fmt.Fprintln(stderr, "xdg-open shim uploaded to ~/xdg-open.sh")
			fmt.Fprintf(stderr, "Warning: %s already exists and is not a symlink; leaving it unchanged.\n", xdgOpenUserLinkConflict)
		}
		if xdgOpenResolvedPath != "" {
			fmt.Fprintf(stderr, "Warning: xdg-open does not currently resolve to the devssh shim (resolved: %s).\n", xdgOpenResolvedPath)
			fmt.Fprintln(stderr, `Add this to your remote shell config: export PATH="$HOME/.local/bin:$PATH"`)
		}
	}
	if opts.installXdgOpen && opts.uploadXdgOpen {
		fmt.Fprintln(stderr, "xdg-open shim also installed at /usr/local/bin/xdg-open")
	}
	if opts.hasBrowser {
		fmt.Fprintf(stderr, "\nBrowser opener available! To enable browser forwarding, add to your shell config:\n")
		fmt.Fprintf(stderr, "  export BROWSER=\"$HOME/browser-opener.sh\"\n\n")
	}
	return nil
}

func buildStaleSocketCleanupCommand(hasBrowser, hasNotification bool) string {
	var commands []string

	if hasBrowser {
		commands = append(commands, `for socket in /tmp/devssh-browser-*.sock; do [ -S "$socket" ] || continue; if ! curl -s --max-time 1 --unix-socket "$socket" "http://localhost/" >/dev/null 2>&1; then rm -f "$socket"; fi; done`)
	}
	if hasNotification {
		commands = append(commands, `for socket in /tmp/devssh-notification-*.sock; do [ -S "$socket" ] || continue; if ! curl -s --max-time 1 --unix-socket "$socket" "http://localhost/" >/dev/null 2>&1; then rm -f "$socket"; fi; done`)
	}

	if len(commands) == 0 {
		return ""
	}
	return "if command -v curl >/dev/null 2>&1; then " + strings.Join(commands, " ; ") + "; fi"
}
