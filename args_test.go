package devssh

import (
	"os"
	"reflect"
	"testing"
)

func withArgs(args []string, fn func()) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = append([]string{"devssh"}, args...)
	fn()
}

func TestParseArgs_PositionalHost(t *testing.T) {
	withArgs([]string{"alpha"}, func() {
		got := ParseArgs()
		if got.Host != "alpha" {
			t.Errorf("Host = %q, want %q", got.Host, "alpha")
		}
		if len(got.RemainingArgs) != 0 {
			t.Errorf("RemainingArgs = %v, want empty", got.RemainingArgs)
		}
	})
}

func TestParseArgs_NoHost(t *testing.T) {
	withArgs([]string{}, func() {
		got := ParseArgs()
		if got.Host != "" {
			t.Errorf("Host = %q, want empty", got.Host)
		}
	})
}

func TestParseArgs_DoubleDashSeparator(t *testing.T) {
	withArgs([]string{"alpha", "--", "-L", "3000:localhost:3000"}, func() {
		got := ParseArgs()
		if got.Host != "alpha" {
			t.Errorf("Host = %q, want alpha", got.Host)
		}
		want := []string{"-L", "3000:localhost:3000"}
		if !reflect.DeepEqual(got.RemainingArgs, want) {
			t.Errorf("RemainingArgs = %v, want %v", got.RemainingArgs, want)
		}
	})
}

func TestParseArgs_FlagsBeforeHost(t *testing.T) {
	withArgs([]string{"--verbose", "--install-xdg-open", "alpha"}, func() {
		got := ParseArgs()
		if got.Host != "alpha" {
			t.Errorf("Host = %q, want alpha", got.Host)
		}
		if !got.Verbose {
			t.Errorf("Verbose = false, want true")
		}
		if !got.InstallXdgOpen {
			t.Errorf("InstallXdgOpen = false, want true")
		}
	})
}

func TestParseArgs_LogsFlag(t *testing.T) {
	withArgs([]string{"--logs"}, func() {
		got := ParseArgs()
		if !got.Logs {
			t.Errorf("Logs = false, want true")
		}
	})
}

func TestParseArgs_DisableFlags(t *testing.T) {
	withArgs([]string{"--no-port-monitor", "--no-browser", "--no-notifications", "--no-xdg-open", "alpha"}, func() {
		got := ParseArgs()
		if !got.NoPortMonitor || !got.NoBrowser || !got.NoNotifications || !got.NoXdgOpen {
			t.Errorf("disable flags not all true: %+v", got)
		}
	})
}

func TestParseArgs_ExtraPositionalArgsTreatedAsRemaining(t *testing.T) {
	// `devssh alpha echo hi` -> host=alpha, remaining=[echo hi]
	withArgs([]string{"alpha", "echo", "hi"}, func() {
		got := ParseArgs()
		if got.Host != "alpha" {
			t.Errorf("Host = %q, want alpha", got.Host)
		}
		want := []string{"echo", "hi"}
		if !reflect.DeepEqual(got.RemainingArgs, want) {
			t.Errorf("RemainingArgs = %v, want %v", got.RemainingArgs, want)
		}
	})
}

func TestCommandLineArgsSessionOptionsMapsCLIFlagsAndConfig(t *testing.T) {
	trueValue := true
	args := CommandLineArgs{
		Host:            "devpod-generated",
		InstallXdgOpen:  false,
		NoXdgOpen:       true,
		NoPortMonitor:   true,
		NoBrowser:       true,
		NoNotifications: true,
		Verbose:         true,
		RemainingArgs:   []string{"-L", "3000:localhost:3000", "tmux"},
	}
	cfg := AppConfig{
		InstallXdgOpen: &trueValue,
		ReversePortForward: []ReversePortForward{
			{Port: 1234, Description: "disabled default", Enabled: false},
		},
		Hosts: map[string]HostConfig{
			"devpod-generated": {
				ReversePortForward: []ReversePortForward{
					{LocalPort: 8080, RemotePort: 18080, Description: "host service", Enabled: true},
				},
			},
		},
	}

	opts := args.SessionOptions(cfg)

	if opts.Host != args.Host {
		t.Fatalf("Host = %q, want %q", opts.Host, args.Host)
	}
	if !opts.DisableBrowser || !opts.DisableNotifications || !opts.DisablePortMonitor || !opts.DisableXdgOpen {
		t.Fatalf("disable flags were not mapped: %+v", opts)
	}
	if !opts.InstallXdgOpen {
		t.Fatal("expected config installXdgOpen to enable system xdg-open install")
	}
	if !opts.Verbose {
		t.Fatal("expected verbose flag to map")
	}
	if !reflect.DeepEqual(opts.SSHArgs, args.RemainingArgs) {
		t.Fatalf("SSHArgs = %v, want %v", opts.SSHArgs, args.RemainingArgs)
	}
	if !opts.DisableDefaultReversePortForwards {
		t.Fatal("expected CLI to pass a fully merged reverse-forward list")
	}

	byLocal := make(map[int]ReversePortForward)
	for _, forward := range opts.ReversePortForwards {
		if port := forward.effectiveLocalPort(); port != 0 {
			byLocal[port] = forward
		}
	}
	if got := byLocal[1234]; got.Enabled {
		t.Fatalf("expected config to disable default 1234 forward, got %+v", got)
	}
	if got := byLocal[8080]; got.RemotePort != 18080 || !got.Enabled {
		t.Fatalf("expected host-specific forward to be included, got %+v", got)
	}
}
