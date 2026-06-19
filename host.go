package devssh

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/kevinburke/ssh_config"
)

// SSHHost is one selectable host alias resolved from ~/.ssh/config.
type SSHHost struct {
	Alias    string
	HostName string
	User     string
	Port     string
}

// String returns a human-readable single-line description of the host
// suitable for the picker.
func (h SSHHost) String() string {
	target := h.HostName
	if target == "" {
		target = h.Alias
	}

	parts := target
	if h.User != "" {
		parts = h.User + "@" + parts
	}
	if h.Port != "" && h.Port != "22" {
		parts = parts + ":" + h.Port
	}

	if parts == h.Alias {
		return h.Alias
	}
	return fmt.Sprintf("%s  →  %s", h.Alias, parts)
}

// resolveHost looks up the resolved fields for the given alias against the
// given decoded SSH config.
func resolveHost(cfg *ssh_config.Config, alias string) SSHHost {
	get := func(key string) string {
		v, _ := cfg.Get(alias, key)
		return strings.TrimSpace(v)
	}
	return SSHHost{
		Alias:    alias,
		HostName: get("HostName"),
		User:     get("User"),
		Port:     get("Port"),
	}
}

// isWildcardPattern reports whether a host pattern contains characters that
// make it a non-concrete alias (wildcards, negation, ranges).
func isWildcardPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?!")
}

// ListSSHHosts parses ~/.ssh/config and returns the concrete host aliases
// (skipping wildcards, negations, and multi-pattern entries). The result is
// deduplicated and sorted alphabetically.
func ListSSHHosts() ([]SSHHost, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	configPath := home + "/.ssh/config"
	f, err := os.Open(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", configPath, err)
	}
	defer f.Close()

	cfg, err := ssh_config.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", configPath, err)
	}

	seen := make(map[string]struct{})
	var aliases []string
	for _, host := range cfg.Hosts {
		for _, pattern := range host.Patterns {
			alias := pattern.String()
			if alias == "" || isWildcardPattern(alias) {
				continue
			}
			if _, ok := seen[alias]; ok {
				continue
			}
			seen[alias] = struct{}{}
			aliases = append(aliases, alias)
		}
	}

	sort.Strings(aliases)

	hosts := make([]SSHHost, 0, len(aliases))
	for _, alias := range aliases {
		hosts = append(hosts, resolveHost(cfg, alias))
	}

	return hosts, nil
}

// SelectHost prompts the user to pick a host from ~/.ssh/config. It returns
// the selected alias (not the resolved hostname).
func SelectHost() (string, error) {
	hosts, err := ListSSHHosts()
	if err != nil {
		return "", err
	}
	if len(hosts) == 0 {
		return "", fmt.Errorf("no concrete Host entries found in ~/.ssh/config")
	}

	options := make([]string, len(hosts))
	for i, h := range hosts {
		options[i] = h.String()
	}

	idx, err := showSelection("Choose a host:", options)
	if err != nil {
		return "", fmt.Errorf("host selection failed: %w", err)
	}

	return hosts[idx].Alias, nil
}
