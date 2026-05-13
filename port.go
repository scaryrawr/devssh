package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
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

const portProbeTimeout = 150 * time.Millisecond

// isPortBound checks if a port is bound on the local machine.
func isPortBound(port int) bool {
	return isPortBoundWithTimeout(port, portProbeTimeout)
}

func isPortBoundWithTimeout(port int, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("localhost", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	if err := conn.Close(); err != nil {
		logDebug("close port probe connection for %d: %v", port, err)
	}
	return true
}

// GetBoundReverseForwards returns the subset of WellKnownPorts that should be
// reverse-forwarded based on local availability or the AlwaysForward flag.
func GetBoundReverseForwards() []ReversePortForward {
	forwards := append([]ReversePortForward(nil), WellKnownPorts...)
	bound := make([]bool, len(forwards))

	const maxConcurrentProbes = 16
	sem := make(chan struct{}, maxConcurrentProbes)
	var wg sync.WaitGroup

	for i, forward := range forwards {
		if !forward.Enabled {
			continue
		}
		if forward.AlwaysForward {
			bound[i] = true
			continue
		}

		wg.Add(1)
		go func(i int, port int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			bound[i] = isPortBound(port)
		}(i, forward.Port)
	}
	wg.Wait()

	boundPorts := make([]ReversePortForward, 0, len(forwards))
	for i, forward := range forwards {
		if bound[i] {
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

func reverseForwardedPortSet() map[int]struct{} {
	ports := make(map[int]struct{}, len(WellKnownPorts))
	for _, forward := range WellKnownPorts {
		if forward.Enabled {
			ports[forward.Port] = struct{}{}
		}
	}
	return ports
}
