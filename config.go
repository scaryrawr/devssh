package devssh

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const configEnvVar = "DEVSSH_CONFIG"

// HostConfig captures per-host configuration overrides.
type HostConfig struct {
	ReversePortForward []ReversePortForward `json:"reversePortForward,omitempty"`
	InstallXdgOpen     *bool                `json:"installXdgOpen,omitempty"`
}

// AppConfig captures global and per-host configuration.
type AppConfig struct {
	ReversePortForward []ReversePortForward  `json:"reversePortForward,omitempty"`
	InstallXdgOpen     *bool                 `json:"installXdgOpen,omitempty"`
	Hosts              map[string]HostConfig `json:"hosts,omitempty"`
}

// getConfigFilePath resolves the configuration file path, honoring
// DEVSSH_CONFIG when set.
func getConfigFilePath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(configEnvVar)); override != "" {
		return override, nil
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}

	return filepath.Join(configDir, "devssh", "config.json"), nil
}

// LoadAppConfig loads the configuration file, returning an empty
// configuration if the file is absent or empty.
func LoadAppConfig() (AppConfig, error) {
	path, err := getConfigFilePath()
	if err != nil {
		return AppConfig{}, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return AppConfig{}, nil
	}

	if err != nil {
		return AppConfig{}, fmt.Errorf("read config file %s: %w", path, err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return AppConfig{}, nil
	}

	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("parse config file %s: %w", path, err)
	}
	if cfg.Hosts == nil {
		cfg.Hosts = make(map[string]HostConfig)
	}

	return cfg, nil
}

// ReversePortForwardsForHost returns defaults merged with top-level and
// per-host overrides for the given host alias.
func (c AppConfig) ReversePortForwardsForHost(host string) []ReversePortForward {
	return c.ReversePortForwardsForHostWithDefaults(host, WellKnownPorts)
}

// ReversePortForwardsForHostWithDefaults returns defaults merged with
// top-level and per-host overrides for the given host alias.
func (c AppConfig) ReversePortForwardsForHostWithDefaults(host string, defaults []ReversePortForward) []ReversePortForward {
	hostForwards := []ReversePortForward(nil)
	if hc, ok := c.Hosts[host]; ok {
		hostForwards = hc.ReversePortForward
	}

	return MergeReversePortForwards(defaults, c.ReversePortForward, hostForwards)
}

// InstallXdgOpenForHost reports whether the xdg-open shim should also be
// symlinked into /usr/local/bin on the remote. The user-local
// ~/.local/bin/xdg-open shim is installed by default. Per-host overrides take
// precedence over the global setting; both default to false.
func (c AppConfig) InstallXdgOpenForHost(host string) bool {
	if hc, ok := c.Hosts[host]; ok && hc.InstallXdgOpen != nil {
		return *hc.InstallXdgOpen
	}
	if c.InstallXdgOpen != nil {
		return *c.InstallXdgOpen
	}
	return false
}

// SaveAppConfig persists the configuration to disk atomically, creating
// directories as needed.
func SaveAppConfig(cfg AppConfig) error {
	path, err := getConfigFilePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write temp config file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace config file %s: %w", path, err)
	}
	return nil
}
