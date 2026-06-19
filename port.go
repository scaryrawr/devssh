package devssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ReversePortForward represents a reverse port forward configuration.
type ReversePortForward struct {
	Port          int    `json:"port,omitempty"`       // Legacy shorthand for localPort and remotePort.
	LocalPort     int    `json:"localPort,omitempty"`  // Local TCP port to expose to the remote.
	RemotePort    int    `json:"remotePort,omitempty"` // Remote TCP port to listen on.
	LocalSocket   string `json:"localSocket,omitempty"`
	RemoteSocket  string `json:"remoteSocket,omitempty"`
	Description   string `json:"description,omitempty"`
	Enabled       bool   `json:"enabled"`
	AlwaysForward bool   `json:"alwaysForward"` // If true, forward even when the local endpoint is not bound
}

var defaultReversePortForwards = []ReversePortForward{
	{Port: 1234, Description: "LM Studio", Enabled: true},
	{Port: 9222, Description: "Chrome DevTools", Enabled: true},
	{Port: 11434, Description: "Ollama", Enabled: true},
}

// DefaultReversePortForwards returns the built-in local service forwards that
// devssh exposes to remote sessions by default.
func DefaultReversePortForwards() []ReversePortForward {
	return append([]ReversePortForward(nil), defaultReversePortForwards...)
}

// WellKnownPorts defines commonly used local service ports that should be
// reverse-forwarded into the remote session so the remote can reach them.
//
// Deprecated: use DefaultReversePortForwards and Options.ReversePortForwards
// instead of mutating this package-level variable.
var WellKnownPorts = DefaultReversePortForwards()

const reverseForwardGUIDPlaceholder = "$GUID"

// MergeReversePortForwards merges default and override port lists by local
// endpoint. Later lists override earlier entries for the same local endpoint.
// Invalid entries are skipped with a warning.
func MergeReversePortForwards(lists ...[]ReversePortForward) []ReversePortForward {
	mergedByKey := make(map[string]ReversePortForward)
	var order []string

	for _, forwards := range lists {
		for _, forward := range forwards {
			if err := validateReversePortForward(forward); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: skipping reverse port forward %q: %v\n", forward.Description, err)
				continue
			}
			key := forward.mergeKey()
			if _, exists := mergedByKey[key]; !exists {
				order = append(order, key)
			}
			mergedByKey[key] = normalizeReversePortForward(forward)
		}
	}

	merged := make([]ReversePortForward, 0, len(order))
	for _, key := range order {
		merged = append(merged, mergedByKey[key])
	}

	return merged
}

func normalizeReversePortForward(forward ReversePortForward) ReversePortForward {
	forward.LocalSocket = strings.TrimSpace(forward.LocalSocket)
	forward.RemoteSocket = strings.TrimSpace(forward.RemoteSocket)
	return forward
}

func validateReversePortForward(forward ReversePortForward) error {
	forward = normalizeReversePortForward(forward)

	if forward.Port != 0 && forward.LocalPort != 0 && forward.Port != forward.LocalPort {
		return fmt.Errorf("port and localPort cannot both be set to different values")
	}

	localPort := forward.effectiveLocalPort()
	hasLocalPort := localPort != 0
	hasLocalSocket := forward.LocalSocket != ""
	if hasLocalPort == hasLocalSocket {
		return fmt.Errorf("set exactly one local endpoint: port/localPort or localSocket")
	}
	if hasLocalPort && !isValidTCPPort(localPort) {
		return fmt.Errorf("invalid local port %d", localPort)
	}
	if hasLocalSocket {
		if !strings.HasPrefix(forward.LocalSocket, "/") {
			return fmt.Errorf("localSocket must be an absolute Unix socket path")
		}
		if strings.Contains(forward.LocalSocket, ":") {
			return fmt.Errorf("localSocket cannot contain ':'")
		}
	}

	remotePort := forward.effectiveRemotePort()
	hasRemotePort := remotePort != 0
	hasRemoteSocket := forward.RemoteSocket != ""
	if hasRemotePort == hasRemoteSocket {
		return fmt.Errorf("set exactly one remote endpoint: remotePort or remoteSocket")
	}
	if hasRemotePort && !isValidTCPPort(remotePort) {
		return fmt.Errorf("invalid remote port %d", remotePort)
	}
	if hasRemoteSocket {
		if !strings.HasPrefix(forward.RemoteSocket, "/") {
			return fmt.Errorf("remoteSocket must be an absolute Unix socket path")
		}
		if strings.Contains(forward.RemoteSocket, ":") {
			return fmt.Errorf("remoteSocket cannot contain ':'")
		}
	}

	return nil
}

func isValidTCPPort(port int) bool {
	return port > 0 && port <= 65535
}

func (f ReversePortForward) effectiveLocalPort() int {
	if f.LocalPort != 0 {
		return f.LocalPort
	}
	return f.Port
}

func (f ReversePortForward) effectiveRemotePort() int {
	if f.RemotePort != 0 {
		return f.RemotePort
	}
	if strings.TrimSpace(f.RemoteSocket) != "" {
		return 0
	}
	if f.Port != 0 {
		return f.Port
	}
	return f.LocalPort
}

func (f ReversePortForward) mergeKey() string {
	if localPort := f.effectiveLocalPort(); localPort != 0 {
		return fmt.Sprintf("tcp:%d", localPort)
	}
	return "unix:" + strings.TrimSpace(f.LocalSocket)
}

func (f ReversePortForward) withExpandedRemoteSocket() ReversePortForward {
	f = normalizeReversePortForward(f)
	if strings.Contains(f.RemoteSocket, reverseForwardGUIDPlaceholder) {
		id := uuid.NewString()
		f.RemoteSocket = strings.ReplaceAll(f.RemoteSocket, reverseForwardGUIDPlaceholder, id)
	}
	return f
}

// LocalSpec returns the OpenSSH local endpoint spec for this reverse forward.
func (f ReversePortForward) LocalSpec() string {
	if localSocket := strings.TrimSpace(f.LocalSocket); localSocket != "" {
		return localSocket
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(f.effectiveLocalPort()))
}

// RemoteSpec returns the OpenSSH remote endpoint spec for this reverse forward.
func (f ReversePortForward) RemoteSpec() string {
	if remoteSocket := strings.TrimSpace(f.RemoteSocket); remoteSocket != "" {
		return remoteSocket
	}
	return strconv.Itoa(f.effectiveRemotePort())
}

func (f ReversePortForward) localLabel() string {
	if localSocket := strings.TrimSpace(f.LocalSocket); localSocket != "" {
		return "socket " + localSocket
	}
	return "port " + strconv.Itoa(f.effectiveLocalPort())
}

func (f ReversePortForward) remoteLabel() string {
	if remoteSocket := strings.TrimSpace(f.RemoteSocket); remoteSocket != "" {
		return "remote socket " + remoteSocket
	}
	return "remote port " + strconv.Itoa(f.effectiveRemotePort())
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

func isUnixSocketBound(path string) bool {
	return isUnixSocketBoundWithTimeout(path, portProbeTimeout)
}

func isUnixSocketBoundWithTimeout(path string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return false
	}
	if err := conn.Close(); err != nil {
		logDebug("close socket probe connection for %s: %v", path, err)
	}
	return true
}

func isReverseForwardEndpointBound(forward ReversePortForward) bool {
	if localSocket := strings.TrimSpace(forward.LocalSocket); localSocket != "" {
		return isUnixSocketBound(localSocket)
	}
	return isPortBound(forward.effectiveLocalPort())
}

// GetBoundReverseForwards returns the subset of WellKnownPorts that should be
// reverse-forwarded based on local availability or the AlwaysForward flag.
//
// Deprecated: use GetBoundReverseForwardsFrom with an explicit forward list.
func GetBoundReverseForwards() []ReversePortForward {
	return GetBoundReverseForwardsFrom(WellKnownPorts)
}

// GetBoundReverseForwardsFrom returns the subset of forwards that should be
// reverse-forwarded based on local availability or the AlwaysForward flag.
func GetBoundReverseForwardsFrom(forwards []ReversePortForward) []ReversePortForward {
	forwards = append([]ReversePortForward(nil), forwards...)
	bound := make([]bool, len(forwards))

	const maxConcurrentProbes = 16
	sem := make(chan struct{}, maxConcurrentProbes)
	var wg sync.WaitGroup

	for i, forward := range forwards {
		if err := validateReversePortForward(forward); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping reverse port forward %q: %v\n", forward.Description, err)
			continue
		}
		forwards[i] = forward.withExpandedRemoteSocket()
		if !forward.Enabled {
			continue
		}
		if forward.AlwaysForward {
			bound[i] = true
			continue
		}

		wg.Add(1)
		go func(i int, forward ReversePortForward) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			bound[i] = isReverseForwardEndpointBound(forward)
		}(i, forwards[i])
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
	LogReverseForwardsTo(os.Stderr, forwards)
}

// LogReverseForwardsTo prints a user-facing summary of detected reverse
// forwards to w.
func LogReverseForwardsTo(w io.Writer, forwards []ReversePortForward) {
	if len(forwards) == 0 {
		return
	}

	fmt.Fprintf(w, "Reverse port forwarding:\n")
	for _, forward := range forwards {
		if forward.AlwaysForward {
			fmt.Fprintf(w, "  • %s (%s → %s) → always forwarded\n", forward.Description, forward.localLabel(), forward.remoteLabel())
		} else {
			fmt.Fprintf(w, "  • %s (%s → %s) → detected locally\n", forward.Description, forward.localLabel(), forward.remoteLabel())
		}
	}
}

// IsReverseForwardedPort reports whether a remote TCP port is part of the active
// reverse-forward set. Used by the port monitor to avoid double-forwarding.
func IsReverseForwardedPort(port int) bool {
	return IsReverseForwardedPortIn(WellKnownPorts, port)
}

// IsReverseForwardedPortIn reports whether a remote TCP port is part of the
// supplied reverse-forward set.
func IsReverseForwardedPortIn(forwards []ReversePortForward, port int) bool {
	for _, forward := range forwards {
		if err := validateReversePortForward(forward); err != nil {
			continue
		}
		if forward.effectiveRemotePort() == port && forward.Enabled {
			return true
		}
	}
	return false
}

func reverseForwardedPortSet(forwards []ReversePortForward) map[int]struct{} {
	ports := make(map[int]struct{}, len(forwards))
	for _, forward := range forwards {
		if err := validateReversePortForward(forward); err != nil {
			continue
		}
		if forward.Enabled {
			if port := forward.effectiveRemotePort(); port != 0 {
				ports[port] = struct{}{}
			}
		}
	}
	return ports
}
