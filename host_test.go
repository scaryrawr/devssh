package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// withTempHomeSSHConfig writes a temp ~/.ssh/config and points HOME at it for
// the duration of the test. Returns the temp HOME.
func withTempHomeSSHConfig(t *testing.T, configContent string) string {
	t.Helper()
	tempHome := t.TempDir()
	sshDir := filepath.Join(tempHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(configContent), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	prev := os.Getenv("HOME")
	t.Setenv("HOME", tempHome)
	t.Cleanup(func() { os.Setenv("HOME", prev) })
	return tempHome
}

func TestIsWildcardPattern(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"server", false},
		{"server.example.com", false},
		{"user@host", false},
		{"*", true},
		{"*.example.com", true},
		{"server?", true},
		{"!badhost", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			if got := isWildcardPattern(tt.pattern); got != tt.want {
				t.Errorf("isWildcardPattern(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestListSSHHosts_Basic(t *testing.T) {
	withTempHomeSSHConfig(t, `
Host alpha
    HostName alpha.example.com
    User alice
    Port 2222

Host beta
    HostName beta.example.com
    User bob

Host *
    User defaultuser
    Port 22
`)

	hosts, err := ListSSHHosts()
	if err != nil {
		t.Fatalf("ListSSHHosts: %v", err)
	}

	aliases := make([]string, len(hosts))
	for i, h := range hosts {
		aliases[i] = h.Alias
	}
	sort.Strings(aliases)
	want := []string{"alpha", "beta"}
	if len(aliases) != 2 || aliases[0] != want[0] || aliases[1] != want[1] {
		t.Fatalf("aliases = %v, want %v", aliases, want)
	}

	byAlias := map[string]SSHHost{}
	for _, h := range hosts {
		byAlias[h.Alias] = h
	}
	if byAlias["alpha"].HostName != "alpha.example.com" {
		t.Errorf("alpha hostname = %q", byAlias["alpha"].HostName)
	}
	if byAlias["alpha"].User != "alice" {
		t.Errorf("alpha user = %q", byAlias["alpha"].User)
	}
	if byAlias["alpha"].Port != "2222" {
		t.Errorf("alpha port = %q", byAlias["alpha"].Port)
	}
}

func TestListSSHHosts_MultiPattern(t *testing.T) {
	withTempHomeSSHConfig(t, `
Host foo bar baz
    User shared

Host wild*
    User wildcarduser
`)

	hosts, err := ListSSHHosts()
	if err != nil {
		t.Fatalf("ListSSHHosts: %v", err)
	}
	gotAliases := make(map[string]bool)
	for _, h := range hosts {
		gotAliases[h.Alias] = true
	}
	for _, alias := range []string{"foo", "bar", "baz"} {
		if !gotAliases[alias] {
			t.Errorf("expected alias %q to be present, got %v", alias, gotAliases)
		}
	}
	if gotAliases["wild*"] {
		t.Errorf("expected wildcard alias to be excluded")
	}
}

func TestListSSHHosts_NoConfig(t *testing.T) {
	tempHome := t.TempDir()
	prev := os.Getenv("HOME")
	t.Setenv("HOME", tempHome)
	t.Cleanup(func() { os.Setenv("HOME", prev) })

	hosts, err := ListSSHHosts()
	if err != nil {
		t.Fatalf("ListSSHHosts on missing config: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("expected no hosts, got %v", hosts)
	}
}

func TestSSHHost_String(t *testing.T) {
	tests := []struct {
		name string
		host SSHHost
		want string
	}{
		{
			name: "fully resolved",
			host: SSHHost{Alias: "alpha", HostName: "alpha.example.com", User: "alice", Port: "2222"},
			want: "alpha  →  alice@alpha.example.com:2222",
		},
		{
			name: "default port hidden",
			host: SSHHost{Alias: "alpha", HostName: "alpha.example.com", User: "alice", Port: "22"},
			want: "alpha  →  alice@alpha.example.com",
		},
		{
			name: "no user",
			host: SSHHost{Alias: "alpha", HostName: "alpha.example.com"},
			want: "alpha  →  alpha.example.com",
		},
		{
			name: "bare alias",
			host: SSHHost{Alias: "alpha"},
			want: "alpha",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.host.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
