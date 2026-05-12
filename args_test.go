package main

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
