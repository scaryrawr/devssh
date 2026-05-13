package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// CommandLineArgs holds the parsed command-line arguments for devssh.
type CommandLineArgs struct {
	Host            string
	Logs            bool
	InstallXdgOpen  bool
	NoXdgOpen       bool
	NoPortMonitor   bool
	NoBrowser       bool
	NoNotifications bool
	Verbose         bool
	RemainingArgs   []string
}

// ParseArgs parses os.Args[1:] and returns the resolved CommandLineArgs.
//
// Usage:
//
//	devssh [flags] [host] [-- ssh-flags-or-remote-command...]
//
// When host is omitted, the user is shown a picker built from
// ~/.ssh/config.
func ParseArgs() CommandLineArgs {
	fs := flag.NewFlagSet("devssh", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `devssh - SSH wrapper with port forwarding, browser, and notification helpers.

Usage:
  devssh [flags] [host] [-- ssh-flags-or-remote-command...]

If [host] is omitted, devssh parses ~/.ssh/config and offers a picker.
Anything after '--' is passed through to the interactive ssh invocation.

Flags:
`)
		fs.PrintDefaults()
	}

	logsFlag := fs.Bool("logs", false, "List recent log files and exit")
	installXdg := fs.Bool("install-xdg-open", false, "Also symlink the xdg-open shim into /usr/local/bin on the remote (requires sudo)")
	noXdg := fs.Bool("no-xdg-open", false, "Skip uploading and installing the xdg-open shim")
	noPort := fs.Bool("no-port-monitor", false, "Do not start the remote port monitor")
	noBrowser := fs.Bool("no-browser", false, "Do not start the local browser opener service")
	noNotify := fs.Bool("no-notifications", false, "Do not start the local notification service")
	verbose := fs.Bool("verbose", false, "Verbose stderr output (also -v)")
	vShort := fs.Bool("v", false, "Verbose stderr output")

	args := os.Args[1:]

	// Split args at the first standalone "--": everything before it is
	// parsed by flag, everything after is treated as remaining args.
	var preDash, postDash []string
	dashIdx := -1
	for i, a := range args {
		if a == "--" {
			dashIdx = i
			break
		}
	}
	if dashIdx == -1 {
		preDash = args
	} else {
		preDash = args[:dashIdx]
		postDash = args[dashIdx+1:]
	}

	if err := fs.Parse(preDash); err != nil {
		// flag.ExitOnError already prints, but keep the linter happy.
		os.Exit(2)
	}

	positional := fs.Args()
	host := ""
	if len(positional) > 0 {
		host = positional[0]
		// Anything else positional that arrived before -- becomes
		// remaining args.
		postDash = append(positional[1:], postDash...)
	}

	return CommandLineArgs{
		Host:            strings.TrimSpace(host),
		Logs:            *logsFlag,
		InstallXdgOpen:  *installXdg,
		NoXdgOpen:       *noXdg,
		NoPortMonitor:   *noPort,
		NoBrowser:       *noBrowser,
		NoNotifications: *noNotify,
		Verbose:         *verbose || *vShort,
		RemainingArgs:   postDash,
	}
}
