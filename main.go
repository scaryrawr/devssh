package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	args := ParseArgs()

	if args.Logs {
		ListRecentLogFiles()
		return nil
	}

	cfg, err := LoadAppConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		cfg = AppConfig{}
	}

	// Resolve host (picker if not provided).
	host := args.Host
	if host == "" {
		picked, err := SelectHost()
		if err != nil {
			return fmt.Errorf("select host: %w", err)
		}
		host = picked
	}

	// Merge config defaults + host overrides.
	WellKnownPorts = cfg.ReversePortForwardsForHost(host)
	installXdg := args.InstallXdgOpen || cfg.InstallXdgOpenForHost(host)

	// Initialize logging now that we have a host.
	initializeSessionID(host)
	if err := initDebugLogger(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize debug logger: %v\n", err)
	}
	defer closeDebugLogger()
	logDebug("devssh starting for host %q", host)
	startupStart := time.Now()

	if args.Verbose {
		fmt.Fprintf(os.Stderr, "Logs: %s\n", getSessionLogDirectory())
	}

	// Start the ssh ControlMaster.
	fmt.Fprintf(os.Stderr, "Connecting to %s...\n", host)
	start := time.Now()
	mux, err := StartMux(ctx, host, nil)
	logElapsed("start ssh master", start)
	if err != nil {
		return fmt.Errorf("start ssh master: %w", err)
	}
	defer func() {
		if err := mux.Stop(); err != nil {
			logDebug("mux.Stop: %v", err)
		}
	}()

	// Preflight remote: warn (don't fail) on missing optional deps.
	start = time.Now()
	preflightRemote(ctx, mux)
	logElapsed("remote preflight", start)

	// Local services (best-effort; failures degrade gracefully).
	var browserSvc *BrowserService
	if !args.NoBrowser {
		start = time.Now()
		if svc, err := NewBrowserService(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to start browser service: %v\n", err)
		} else {
			browserSvc = svc
			defer browserSvc.Stop()
		}
		logElapsed("start browser service", start)
	}

	var notifySvc *NotificationService
	if !args.NoNotifications {
		start = time.Now()
		if svc, err := NewNotificationService(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to start notification service: %v\n", err)
		} else {
			notifySvc = svc
			defer notifySvc.Stop()
		}
		logElapsed("start notification service", start)
	}

	// Stream-local remote forwards for the services. If either fails, disable
	// that feature and continue.
	if browserSvc != nil {
		start = time.Now()
		if err := mux.AddRemoteForward(browserSvc.SocketPath, fmt.Sprintf("127.0.0.1:%d", browserSvc.Port)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to forward browser socket (browser opening disabled): %v\n", err)
			browserSvc.Stop()
			browserSvc = nil
		}
		logElapsed("forward browser socket", start)
	}
	if notifySvc != nil {
		start = time.Now()
		if err := mux.AddRemoteForward(notifySvc.SocketPath, fmt.Sprintf("127.0.0.1:%d", notifySvc.Port)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to forward notification socket (notifications disabled): %v\n", err)
			notifySvc.Stop()
			notifySvc = nil
		}
		logElapsed("forward notification socket", start)
	}

	// Reverse-forward local AI services (LM Studio, Ollama, ...).
	start = time.Now()
	boundForwards := GetBoundReverseForwards()
	logElapsed("probe reverse forward ports", start)
	if len(boundForwards) > 0 {
		LogReverseForwards(boundForwards)
		for _, fw := range boundForwards {
			spec := fmt.Sprintf("%d", fw.Port)
			local := fmt.Sprintf("127.0.0.1:%d", fw.Port)
			start = time.Now()
			if err := mux.AddRemoteForward(spec, local); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to forward port %d: %v\n", fw.Port, err)
			}
			logElapsed(fmt.Sprintf("forward reverse port %d", fw.Port), start)
		}
	}

	// Upload helper scripts to the remote in a single shell command.
	start = time.Now()
	if err := prepareRemoteScripts(ctx, mux, prepareOpts{
		hasBrowser:      browserSvc != nil,
		hasNotification: notifySvc != nil,
		installXdgOpen:  installXdg,
		uploadXdgOpen:   !args.NoXdgOpen,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to upload helper scripts: %v\n", err)
	}
	logElapsed("prepare remote scripts", start)

	if notifySvc != nil {
		fmt.Fprintf(os.Stderr, "Command completion notifications available! To enable, add to your shell config:\n")
		fmt.Fprintf(os.Stderr, "  # bash (~/.bashrc) or zsh (~/.zshrc)\n")
		fmt.Fprintf(os.Stderr, "  if [ -f \"$HOME/notification-sender.sh\" ]; then\n")
		fmt.Fprintf(os.Stderr, "      source \"$HOME/notification-sender.sh\"\n")
		fmt.Fprintf(os.Stderr, "  fi\n")
		fmt.Fprintf(os.Stderr, "  # fish with the done plugin (~/.config/fish/config.fish)\n")
		fmt.Fprintf(os.Stderr, "  set -U __done_allow_nongraphical 1\n")
		fmt.Fprintf(os.Stderr, "  set -U __done_notification_command \"~/notification-sender.sh send \\$title \\$message\"\n\n")
	}

	// Start the remote port monitor.
	var monitor *PortMonitorController
	if !args.NoPortMonitor {
		start = time.Now()
		m, err := StartPortMonitor(ctx, mux)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to start port monitor: %v\n", err)
		} else {
			monitor = m
		}
		logElapsed("start port monitor", start)
	}
	if monitor != nil {
		defer func() {
			monitor.Stop()
			monitor.Wait()
		}()
	}

	logElapsed("startup before interactive shell", startupStart)

	// Hand control to the interactive shell.
	return mux.InteractiveShell(ctx, args.RemainingArgs)
}

// preflightRemote runs a single SSH command to check for the optional remote
// tools we depend on. Missing tools are warned-about but never fatal.
func preflightRemote(ctx context.Context, mux *Mux) {
	const probe = "for t in bash jq ss curl base64 chmod; do command -v $t >/dev/null 2>&1 || echo MISSING:$t; done"
	stdout, _, err := mux.Run(ctx, "sh", "-c", "'"+probe+"'")
	if err != nil {
		logDebug("preflight failed: %v", err)
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.HasPrefix(line, "MISSING:") {
			tool := strings.TrimPrefix(line, "MISSING:")
			fmt.Fprintf(os.Stderr, "Warning: remote is missing %q; some features may be disabled.\n", tool)
		}
	}
}

type prepareOpts struct {
	hasBrowser      bool
	hasNotification bool
	installXdgOpen  bool
	uploadXdgOpen   bool
}

// prepareRemoteScripts writes all helper scripts to the remote in a single
// SSH call using base64-encoded payloads.
func prepareRemoteScripts(ctx context.Context, mux *Mux, opts prepareOpts) error {
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

	if opts.installXdgOpen && opts.uploadXdgOpen {
		cmdParts = append(cmdParts,
			"(test -L /usr/local/bin/xdg-open || sudo ln -sf ~/xdg-open.sh /usr/local/bin/xdg-open)")
	}

	// Clean up stale sockets from prior sessions.
	if cleanup := buildStaleSocketCleanupCommand(opts.hasBrowser, opts.hasNotification); cleanup != "" {
		cmdParts = append(cmdParts, cleanup)
	}

	fullCmd := strings.Join(cmdParts, " && ")

	wrapped := wrapBashLoginCommand(fullCmd)
	stdout, stderr, err := mux.Run(ctx, wrapped...)
	if err != nil {
		return fmt.Errorf("upload scripts: %w (stdout: %s, stderr: %s)", err,
			strings.TrimSpace(stdout), strings.TrimSpace(stderr))
	}

	fmt.Fprintln(os.Stderr, "Helper scripts uploaded.")
	if opts.installXdgOpen && opts.uploadXdgOpen {
		fmt.Fprintln(os.Stderr, "xdg-open shim installed at /usr/local/bin/xdg-open")
	} else if opts.uploadXdgOpen {
		fmt.Fprintln(os.Stderr, "xdg-open shim available at ~/xdg-open.sh (use --install-xdg-open to symlink into /usr/local/bin)")
	}
	if opts.hasBrowser {
		fmt.Fprintf(os.Stderr, "\nBrowser opener available! To enable browser forwarding, add to your shell config:\n")
		fmt.Fprintf(os.Stderr, "  export BROWSER=\"$HOME/browser-opener.sh\"\n\n")
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
