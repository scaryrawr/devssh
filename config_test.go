package devssh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAppConfig_ReversePortForwardsForHost(t *testing.T) {
	original := WellKnownPorts
	defer func() { WellKnownPorts = original }()

	WellKnownPorts = []ReversePortForward{
		{Port: 1234, Description: "LM Studio", Enabled: true},
		{Port: 11434, Description: "Ollama", Enabled: true},
	}

	cfg := AppConfig{
		ReversePortForward: []ReversePortForward{
			{Port: 1234, Description: "LM Studio override", Enabled: false},
			{Port: 8081, Description: "Top level", Enabled: true},
		},
		Hosts: map[string]HostConfig{
			"myserver": {
				ReversePortForward: []ReversePortForward{
					{Port: 8081, Description: "Per host", Enabled: false},
					{Port: 9090, Description: "Per host extra", Enabled: true},
				},
			},
		},
	}

	merged := cfg.ReversePortForwardsForHost("myserver")

	byPort := make(map[int]ReversePortForward)
	for _, forward := range merged {
		byPort[forward.Port] = forward
	}

	if got, ok := byPort[1234]; !ok || got.Enabled {
		t.Fatalf("expected top-level override for 1234 to disable default, got %+v", got)
	}
	if got, ok := byPort[8081]; !ok || got.Enabled {
		t.Fatalf("expected host override for 8081 to win, got %+v", got)
	}
	if _, ok := byPort[9090]; !ok {
		t.Fatal("expected host custom port 9090 to be present")
	}
}

func TestAppConfig_ReversePortForwardsForHost_UnknownHost(t *testing.T) {
	original := WellKnownPorts
	defer func() { WellKnownPorts = original }()
	WellKnownPorts = []ReversePortForward{
		{Port: 1234, Description: "LM Studio", Enabled: true},
	}

	cfg := AppConfig{
		ReversePortForward: []ReversePortForward{
			{Port: 8081, Description: "Top level", Enabled: true},
		},
		Hosts: map[string]HostConfig{
			"other": {ReversePortForward: []ReversePortForward{{Port: 9090, Enabled: true}}},
		},
	}

	merged := cfg.ReversePortForwardsForHost("unknown")
	byPort := make(map[int]ReversePortForward)
	for _, f := range merged {
		byPort[f.Port] = f
	}
	if _, ok := byPort[9090]; ok {
		t.Fatal("expected unknown host to not pick up other host's overrides")
	}
	if _, ok := byPort[8081]; !ok {
		t.Fatal("expected top-level override to apply for unknown host")
	}
	if _, ok := byPort[1234]; !ok {
		t.Fatal("expected default to apply for unknown host")
	}
}

func TestAppConfig_InstallXdgOpenForHost(t *testing.T) {
	tval := true
	fval := false

	t.Run("default false", func(t *testing.T) {
		cfg := AppConfig{}
		if cfg.InstallXdgOpenForHost("any") {
			t.Fatal("expected default false")
		}
	})

	t.Run("global true", func(t *testing.T) {
		cfg := AppConfig{InstallXdgOpen: &tval}
		if !cfg.InstallXdgOpenForHost("any") {
			t.Fatal("expected global true to apply")
		}
	})

	t.Run("host override beats global", func(t *testing.T) {
		cfg := AppConfig{
			InstallXdgOpen: &tval,
			Hosts:          map[string]HostConfig{"h": {InstallXdgOpen: &fval}},
		}
		if cfg.InstallXdgOpenForHost("h") {
			t.Fatal("expected host override to disable")
		}
		if !cfg.InstallXdgOpenForHost("other") {
			t.Fatal("expected global true to still apply to other host")
		}
	})
}

func TestLoadAppConfig(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		configPath  string
		configData  string
		createFile  bool
		expectError bool
		expected    AppConfig
	}{
		{
			name:       "non-existent file",
			configPath: filepath.Join(tempDir, "nonexistent.json"),
			expected:   AppConfig{},
		},
		{
			name:       "empty file",
			configPath: filepath.Join(tempDir, "empty.json"),
			configData: "",
			createFile: true,
			expected:   AppConfig{},
		},
		{
			name:       "whitespace only file",
			configPath: filepath.Join(tempDir, "whitespace.json"),
			configData: "   \n\t  ",
			expected:   AppConfig{},
		},
		{
			name:       "valid structured config",
			configPath: filepath.Join(tempDir, "structured.json"),
			configData: `{
"reversePortForward": [
{"port": 8081, "description": "Top", "enabled": true},
{"localPort": 8082, "remotePort": 18082, "description": "Alternate", "enabled": true},
{"port": 8083, "remoteSocket": "/tmp/custom-$GUID.sock", "description": "Socket", "enabled": true}
],
"hosts": {
"myserver": {
"reversePortForward": [{"port": 9090, "description": "Host", "enabled": true}]
}
}
}`,
			expected: AppConfig{
				ReversePortForward: []ReversePortForward{
					{Port: 8081, Description: "Top", Enabled: true},
					{LocalPort: 8082, RemotePort: 18082, Description: "Alternate", Enabled: true},
					{Port: 8083, RemoteSocket: "/tmp/custom-$GUID.sock", Description: "Socket", Enabled: true},
				},
				Hosts: map[string]HostConfig{
					"myserver": {
						ReversePortForward: []ReversePortForward{{Port: 9090, Description: "Host", Enabled: true}},
					},
				},
			},
		},
		{
			name:        "invalid json",
			configPath:  filepath.Join(tempDir, "invalid.json"),
			configData:  `{"invalid": json}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalEnv := os.Getenv(configEnvVar)
			defer os.Setenv(configEnvVar, originalEnv)

			if tt.configData != "" || tt.createFile {
				if err := os.WriteFile(tt.configPath, []byte(tt.configData), 0o644); err != nil {
					t.Fatalf("Failed to create test config file: %v", err)
				}
			}

			os.Setenv(configEnvVar, tt.configPath)

			result, err := LoadAppConfig()
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected an error, but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			resultJSON, _ := json.Marshal(result)
			expectedJSON, _ := json.Marshal(tt.expected)
			if string(resultJSON) != string(expectedJSON) {
				t.Errorf("Config mismatch\nGot:  %s\nWant: %s", resultJSON, expectedJSON)
			}
		})
	}
}

func TestSaveAppConfig_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	originalEnv := os.Getenv(configEnvVar)
	defer os.Setenv(configEnvVar, originalEnv)
	os.Setenv(configEnvVar, configPath)

	tval := true
	config := AppConfig{
		ReversePortForward: []ReversePortForward{
			{Port: 8081, Description: "Top", Enabled: true},
			{LocalPort: 8082, RemotePort: 18082, Description: "Alternate", Enabled: true},
			{Port: 8083, RemoteSocket: "/tmp/custom-$GUID.sock", Description: "Socket", Enabled: true},
		},
		InstallXdgOpen: &tval,
		Hosts: map[string]HostConfig{
			"myserver": {
				ReversePortForward: []ReversePortForward{
					{Port: 9090, Description: "Host", Enabled: true},
				},
			},
		},
	}

	if err := SaveAppConfig(config); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	got, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(config)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("Round-trip mismatch\nGot:  %s\nWant: %s", gotJSON, wantJSON)
	}
}
