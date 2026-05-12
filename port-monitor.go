package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed port-monitor.sh
var portMonitorScript string

// PortMessage is a JSON message emitted by port-monitor.sh for each
// bound/unbound event.
type PortMessage struct {
	Type      string `json:"type"`
	Action    string `json:"action"`
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
	Timestamp string `json:"timestamp"`
}

// LogMessage is a JSON log line emitted by port-monitor.sh.
type LogMessage struct {
	Type      string `json:"type"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// PortMonitorController manages the lifecycle of the remote port monitor.
type PortMonitorController struct {
	cancel    context.CancelFunc
	waitGroup *sync.WaitGroup
}

// Stop signals the port monitor to begin shutdown.
func (pmc *PortMonitorController) Stop() {
	if pmc.cancel != nil {
		logDebug("PortMonitorController: Stop() called")
		pmc.cancel()
	}
}

// Wait blocks until the port monitor goroutine completes its cleanup.
func (pmc *PortMonitorController) Wait() {
	if pmc.waitGroup != nil {
		logDebug("PortMonitorController: Wait() called")
		pmc.waitGroup.Wait()
		logDebug("PortMonitorController: cleanup complete")
	}
}

// StartPortMonitor launches the remote port monitor over the existing Mux
// and starts forwarding any detected listening ports back to the local
// machine via mux.AddLocalForward.
func StartPortMonitor(ctx context.Context, mux *Mux) (*PortMonitorController, error) {
	logDebug("Starting port monitor over Mux for host %s", mux.Host)

	monitorCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		logDebug("Port monitor goroutine started")
		if err := runPortMonitor(monitorCtx, mux); err != nil &&
			err != context.Canceled &&
			!strings.Contains(err.Error(), "context canceled") {
			logDebug("Port monitor exited with error: %v", err)
		} else {
			logDebug("Port monitor exited cleanly")
		}
	}()

	return &PortMonitorController{cancel: cancel, waitGroup: &wg}, nil
}

// activeForwards tracks the local→remote port forwards we've established.
type activeForwards struct {
	mu    sync.Mutex
	ports map[int]struct{}
}

func newActiveForwards() *activeForwards {
	return &activeForwards{ports: make(map[int]struct{})}
}

func (a *activeForwards) add(port int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ports[port] = struct{}{}
}

func (a *activeForwards) remove(port int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.ports[port]; ok {
		delete(a.ports, port)
		return true
	}
	return false
}

func (a *activeForwards) snapshot() []int {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]int, 0, len(a.ports))
	for p := range a.ports {
		out = append(out, p)
	}
	return out
}

// runPortMonitor invokes ~/port-monitor.sh remotely and processes its
// stdout, opening / closing local forwards in response.
func runPortMonitor(ctx context.Context, mux *Mux) error {
	cmd := mux.Command(ctx, "~/port-monitor.sh")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start remote port-monitor: %w", err)
	}

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			logDebug("port-monitor stderr: %s", scanner.Text())
		}
	}()

	forwards := newActiveForwards()
	defer cleanupForwards(mux, forwards)

	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			var typeCheck struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(line), &typeCheck); err != nil {
				logDebug("port-monitor non-JSON line: %s", line)
				continue
			}

			switch typeCheck.Type {
			case "port":
				var pm PortMessage
				if err := json.Unmarshal([]byte(line), &pm); err != nil {
					continue
				}
				handlePortMessage(mux, pm, forwards)
			case "log":
				var lm LogMessage
				if err := json.Unmarshal([]byte(line), &lm); err != nil {
					continue
				}
				logDebug("port-monitor: %s", lm.Message)
			}
		}
	}()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
		return ctx.Err()
	case err := <-waitErr:
		<-done
		return err
	}
}

func handlePortMessage(mux *Mux, msg PortMessage, forwards *activeForwards) {
	switch msg.Action {
	case "bound":
		if IsReverseForwardedPort(msg.Port) {
			logDebug("Port %d is reverse-forwarded, skipping local forward", msg.Port)
			return
		}
		if err := mux.AddLocalForward(msg.Port, msg.Port); err != nil {
			logDebug("Failed to add local forward for port %d: %v", msg.Port, err)
			return
		}
		forwards.add(msg.Port)
		logDebug("Added local forward for port %d", msg.Port)

	case "unbound":
		if !forwards.remove(msg.Port) {
			return
		}
		if err := mux.CancelLocalForward(msg.Port, msg.Port); err != nil {
			logDebug("Failed to cancel local forward for port %d: %v", msg.Port, err)
			return
		}
		logDebug("Cancelled local forward for port %d", msg.Port)
	}
}

func cleanupForwards(mux *Mux, forwards *activeForwards) {
	ports := forwards.snapshot()
	if len(ports) == 0 {
		return
	}
	logDebug("Cleaning up %d active local forwards", len(ports))
	for _, p := range ports {
		if err := mux.CancelLocalForward(p, p); err != nil {
			logDebug("Cleanup: failed to cancel forward for port %d: %v", p, err)
		}
	}
}
