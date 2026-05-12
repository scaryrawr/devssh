package main

import (
	"net"
	"testing"
	"time"
)

func TestIsPortBound(t *testing.T) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create test listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	boundPort := addr.Port

	if !isPortBound(boundPort) {
		t.Errorf("isPortBound(%d) = false, expected true for bound port", boundPort)
	}

	unboundPort := 65432
	if isPortBound(unboundPort) {
		t.Logf("Warning: Port %d is bound, test may be unreliable", unboundPort)
	}
}

func TestGetBoundReverseForwards(t *testing.T) {
	forwards := GetBoundReverseForwards()

	for _, forward := range forwards {
		t.Logf("Found bound port: %d (%s)", forward.Port, forward.Description)
	}

	for _, forward := range forwards {
		if forward.Port <= 0 {
			t.Errorf("Invalid port number: %d", forward.Port)
		}
		if forward.Description == "" {
			t.Errorf("Missing description for port %d", forward.Port)
		}
		if !forward.AlwaysForward && !isPortBound(forward.Port) {
			t.Errorf("Port %d reported as bound but isPortBound() returns false", forward.Port)
		}
	}
}

func TestMergeReversePortForwards(t *testing.T) {
	base := []ReversePortForward{
		{Port: 1234, Description: "LM Studio", Enabled: true},
		{Port: 11434, Description: "Ollama", Enabled: true},
	}
	topLevel := []ReversePortForward{
		{Port: 1234, Description: "Override LM", Enabled: false},
		{Port: 8081, Description: "Top level", Enabled: true},
	}
	host := []ReversePortForward{
		{Port: 8081, Description: "Host override", Enabled: false},
		{Port: 9090, Description: "Host extra", Enabled: true},
	}

	merged := MergeReversePortForwards(base, topLevel, host)
	byPort := map[int]ReversePortForward{}
	for _, forward := range merged {
		byPort[forward.Port] = forward
	}

	if got, ok := byPort[1234]; !ok || got.Enabled {
		t.Fatalf("expected 1234 to be disabled by override, got %+v", got)
	}
	if got, ok := byPort[8081]; !ok || got.Enabled {
		t.Fatalf("expected host override for 8081, got %+v", got)
	}
	if _, ok := byPort[9090]; !ok {
		t.Fatal("expected host port 9090 to be present")
	}
}

func TestMergeReversePortForwards_SkipsInvalid(t *testing.T) {
	input := []ReversePortForward{
		{Port: 0, Description: "zero", Enabled: true},
		{Port: -1, Description: "negative", Enabled: true},
		{Port: 70000, Description: "too big", Enabled: true},
		{Port: 8080, Description: "ok", Enabled: true},
	}
	merged := MergeReversePortForwards(input)
	if len(merged) != 1 || merged[0].Port != 8080 {
		t.Fatalf("expected only 8080 to survive, got %+v", merged)
	}
}

func TestWellKnownPorts(t *testing.T) {
	if len(WellKnownPorts) == 0 {
		t.Error("WellKnownPorts should not be empty")
	}

	seenPorts := make(map[int]bool)
	for _, forward := range WellKnownPorts {
		if forward.Port <= 0 || forward.Port > 65535 {
			t.Errorf("Invalid port number: %d", forward.Port)
		}

		if seenPorts[forward.Port] {
			t.Errorf("Duplicate port in WellKnownPorts: %d", forward.Port)
		}
		seenPorts[forward.Port] = true

		if forward.Description == "" {
			t.Errorf("Missing description for port %d", forward.Port)
		}
	}

	expectedPorts := map[int]string{
		1234:  "LM Studio",
		11434: "Ollama",
	}

	for port, desc := range expectedPorts {
		found := false
		for _, forward := range WellKnownPorts {
			if forward.Port == port {
				found = true
				if forward.Description != desc {
					t.Logf("Port %d description: got %q, expected %q", port, forward.Description, desc)
				}
				break
			}
		}
		if !found {
			t.Errorf("Expected port %d (%s) not found in WellKnownPorts", port, desc)
		}
	}
}

func TestIsReverseForwardedPort(t *testing.T) {
	tests := []struct {
		name     string
		port     int
		expected bool
	}{
		{"LM Studio port", 1234, true},
		{"Chrome DevTools port", 9222, true},
		{"Ollama port", 11434, true},
		{"random high port", 8080, false},
		{"another random port", 3000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsReverseForwardedPort(tt.port)
			if result != tt.expected {
				t.Errorf("IsReverseForwardedPort(%d) = %v, want %v", tt.port, result, tt.expected)
			}
		})
	}

	t.Run("disabled port in WellKnownPorts", func(t *testing.T) {
		originalPorts := WellKnownPorts
		defer func() { WellKnownPorts = originalPorts }()

		WellKnownPorts = []ReversePortForward{
			{Port: 5555, Description: "Disabled Service", Enabled: false},
			{Port: 6666, Description: "Enabled Service", Enabled: true},
		}

		if IsReverseForwardedPort(5555) {
			t.Error("IsReverseForwardedPort(5555) = true, want false for disabled port")
		}
		if !IsReverseForwardedPort(6666) {
			t.Error("IsReverseForwardedPort(6666) = false, want true for enabled port")
		}
	})
}

func TestReversePortForwardIntegration(t *testing.T) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create test listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	testPort := addr.Port

	originalPorts := WellKnownPorts
	defer func() { WellKnownPorts = originalPorts }()

	WellKnownPorts = []ReversePortForward{
		{Port: testPort, Description: "Test Service", Enabled: true},
	}

	time.Sleep(10 * time.Millisecond)

	forwards := GetBoundReverseForwards()

	if len(forwards) != 1 {
		t.Errorf("Expected 1 forward, got %d", len(forwards))
	}

	if len(forwards) > 0 && forwards[0].Port != testPort {
		t.Errorf("Expected port %d, got %d", testPort, forwards[0].Port)
	}
}

func TestGetBoundReverseForwards_DisabledPorts(t *testing.T) {
	originalPorts := WellKnownPorts
	defer func() { WellKnownPorts = originalPorts }()

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create test listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	testPort := addr.Port

	WellKnownPorts = []ReversePortForward{
		{Port: testPort, Description: "Disabled Service", Enabled: false},
	}

	forwards := GetBoundReverseForwards()

	if len(forwards) != 0 {
		t.Errorf("Expected 0 forwards for disabled port, got %d", len(forwards))
	}
}
