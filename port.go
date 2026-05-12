package main

import (
	"fmt"
	"net"
	"os"
)

// ReversePortForward represents a reverse port forward configuration.
type ReversePortForward struct {
	Port          int    `json:"port"`
	Description   string `json:"description"`
	Enabled       bool   `json:"enabled"`
	AlwaysForward bool   `json:"alwaysForward"` // If true, forward even when port is not bound locally
}

// WellKnownPorts defines commonly used local service ports that should be
// reverse-forwarded into the remote session so the remote can reach them.
var WellKnownPorts = []ReversePortForward{
	{Port: 1234, Description: "LM Studio", Enabled: true},
	{Port: 9222, Description: "Chrome DevTools", Enabled: true},
	{Port: 11434, Description: "Ollama", Enabled: true},
}

// MergeReversePortForwards merges default and override port lists by port number.
// Later lists override earlier entries for the same port. Entries with invalid
// port numbers (≤0 or >65535) are skipped with a warning.
func MergeReversePortForwards(lists ...[]ReversePortForward) []ReversePortForward {
	mergedByPort := make(map[int]ReversePortForward)
	var order []int

	for _, forwards := range lists {
		for _, forward := range forwards {
			if forward.Port <= 0 || forward.Port > 65535 {
				fmt.Fprintf(os.Stderr, "Warning: skipping reverse port forward with invalid port %d (%q)\n", forward.Port, forward.Description)
				continue
			}
			if _, exists := mergedByPort[forward.Port]; !exists {
				order = append(order, forward.Port)
			}
			mergedByPort[forward.Port] = forward
		}
	}

	merged := make([]ReversePortForward, 0, len(order))
	for _, port := range order {
		merged = append(merged, mergedByPort[port])
	}

	return merged
}

// isPortBound checks if a port is bound on the local machine.
func isPortBound(port int) bool {
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// GetBoundReverseForwards returns the subset of WellKnownPorts that should be
// reverse-forwarded based on local availability or the AlwaysForward flag.
func GetBoundReverseForwards() []ReversePortForward {
	var boundPorts []ReversePortForward

	for _, forward := range WellKnownPorts {
		if !forward.Enabled {
			continue
		}

		if forward.AlwaysForward || isPortBound(forward.Port) {
			boundPorts = append(boundPorts, forward)
		}
	}

	return boundPorts
}

// LogReverseForwards prints a user-facing summary of detected reverse forwards.
func LogReverseForwards(forwards []ReversePortForward) {
	if len(forwards) == 0 {
		return
	}

	fmt.Fprintf(os.Stderr, "Reverse port forwarding:\n")
	for _, forward := range forwards {
		if forward.AlwaysForward {
			fmt.Fprintf(os.Stderr, "  • %s (port %d) → always forwarded\n", forward.Description, forward.Port)
		} else {
			fmt.Fprintf(os.Stderr, "  • %s (port %d) → detected locally\n", forward.Description, forward.Port)
		}
	}
}

// IsReverseForwardedPort reports whether a port is part of the active
// reverse-forward set. Used by the port monitor to avoid double-forwarding.
func IsReverseForwardedPort(port int) bool {
	for _, forward := range WellKnownPorts {
		if forward.Port == port && forward.Enabled {
			return true
		}
	}
	return false
}
