package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuoteForShell(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "''",
		},
		{
			name:     "simple command",
			input:    "echo hello",
			expected: "'echo hello'",
		},
		{
			name:     "command with single quotes",
			input:    "echo 'hello world'",
			expected: "'echo '\"'\"'hello world'\"'\"''",
		},
		{
			name:     "command with double quotes",
			input:    `echo "hello world"`,
			expected: `'echo "hello world"'`,
		},
		{
			name:     "command with semicolons",
			input:    "cmd1; cmd2",
			expected: "'cmd1; cmd2'",
		},
		{
			name:     "command with AND operator",
			input:    "cmd1 && cmd2",
			expected: "'cmd1 && cmd2'",
		},
		{
			name:     "command with OR operator",
			input:    "cmd1 || cmd2",
			expected: "'cmd1 || cmd2'",
		},
		{
			name:     "command with pipe",
			input:    "echo hello | grep h",
			expected: "'echo hello | grep h'",
		},
		{
			name:     "command with backticks",
			input:    "echo `whoami`",
			expected: "'echo `whoami`'",
		},
		{
			name:     "command with dollar expansion",
			input:    "echo $(whoami)",
			expected: "'echo $(whoami)'",
		},
		{
			name:     "command with multiple single quotes",
			input:    "it's a 'test'",
			expected: "'it'\"'\"'s a '\"'\"'test'\"'\"''",
		},
		{
			name:     "command with special characters",
			input:    "chmod +x ~/script.sh && ./script.sh",
			expected: "'chmod +x ~/script.sh && ./script.sh'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := quoteForShell(tt.input)
			if result != tt.expected {
				t.Errorf("quoteForShell(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildXdgOpenUserInstallCommand(t *testing.T) {
	homeDir := t.TempDir()
	scriptPath := filepath.Join(homeDir, "xdg-open.sh")
	linkPath := filepath.Join(homeDir, ".local", "bin", "xdg-open")

	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write xdg-open script: %v", err)
	}

	cmd := exec.Command("bash", "-c", buildXdgOpenUserInstallCommand())
	cmd.Env = append(os.Environ(), "HOME="+homeDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("user install command failed: %v\n%s", err, output)
	}

	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("read xdg-open symlink: %v", err)
	}
	if target != scriptPath {
		t.Fatalf("xdg-open symlink = %q, want %q", target, scriptPath)
	}
}

func TestBuildXdgOpenPathCheckCommand(t *testing.T) {
	homeDir := t.TempDir()
	scriptPath := filepath.Join(homeDir, "xdg-open.sh")
	localBin := filepath.Join(homeDir, ".local", "bin")

	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write xdg-open script: %v", err)
	}

	cmd := exec.Command("bash", "-c", buildXdgOpenUserInstallCommand()+" && "+buildXdgOpenPathCheckCommand())
	cmd.Env = append(os.Environ(), "HOME="+homeDir, "PATH="+localBin+":"+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("path check command failed: %v\n%s", err, output)
	}
	if strings.Contains(string(output), xdgOpenNotOnPathMarker) {
		t.Fatalf("path check reported shim missing from PATH: %s", output)
	}
}

func TestParseXdgOpenSetupStdout(t *testing.T) {
	conflict, resolved := parseXdgOpenSetupStdout("noise\n" + xdgOpenUserLinkExistsMarker + "/home/me/.local/bin/xdg-open\n" + xdgOpenNotOnPathMarker + "/usr/bin/xdg-open\n")
	if conflict != "/home/me/.local/bin/xdg-open" {
		t.Fatalf("conflict = %q", conflict)
	}
	if resolved != "/usr/bin/xdg-open" {
		t.Fatalf("resolved = %q", resolved)
	}
}

func TestBuildXdgOpenInstallCommand(t *testing.T) {
	homeDir := t.TempDir()
	binDir := filepath.Join(t.TempDir(), "bin dir")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	scriptPath := filepath.Join(homeDir, "xdg-open.sh")
	oldTarget := filepath.Join(homeDir, "old-xdg-open.sh")
	linkPath := filepath.Join(binDir, "xdg-open")

	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write xdg-open script: %v", err)
	}
	if err := os.WriteFile(oldTarget, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write old xdg-open script: %v", err)
	}
	if err := os.Symlink(oldTarget, linkPath); err != nil {
		t.Fatalf("create stale xdg-open symlink: %v", err)
	}

	cmd := exec.Command("bash", "-lc", buildXdgOpenInstallCommand(binDir))
	cmd.Env = append(os.Environ(), "HOME="+homeDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install command failed: %v\n%s", err, output)
	}

	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("read xdg-open symlink: %v", err)
	}
	if target != scriptPath {
		t.Fatalf("xdg-open symlink = %q, want %q", target, scriptPath)
	}
}

func TestWrapBashLoginCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple command",
			input:    "echo hello",
			expected: []string{"bash", "-lc", "'echo hello'"},
		},
		{
			name:     "empty command",
			input:    "",
			expected: []string{"bash", "-lc", "''"},
		},
		{
			name:     "complex command with operators",
			input:    "set -e; cmd1 && cmd2",
			expected: []string{"bash", "-lc", "'set -e; cmd1 && cmd2'"},
		},
		{
			name:     "command with single quotes",
			input:    "echo 'hello'",
			expected: []string{"bash", "-lc", "'echo '\"'\"'hello'\"'\"''"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wrapBashLoginCommand(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("wrapBashLoginCommand(%q) returned %d elements, want %d", tt.input, len(result), len(tt.expected))
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("wrapBashLoginCommand(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
				}
			}
		})
	}
}
