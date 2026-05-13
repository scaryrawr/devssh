package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/browser"
)

//go:embed browser-opener.sh
var browserOpenerScript string

// BrowserService runs a local HTTP server that opens URLs in the user's
// default browser. The remote side reaches it through a streamlocal SSH
// forward pointed at SocketPath.
type BrowserService struct {
	Port       int
	SocketPath string
	server     *http.Server
	listener   net.Listener
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewBrowserService starts the local HTTP browser service.
func NewBrowserService(ctx context.Context) (*BrowserService, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to create local listener: %w", err)
	}

	browserPort := listener.Addr().(*net.TCPAddr).Port

	socketId := uuid.New()
	socketPath := "/tmp/devssh-browser-" + socketId.String() + ".sock"

	logDebug("Local browser HTTP service created on port: %d, socket path: %s", browserPort, socketPath)

	serviceCtx, cancel := context.WithCancel(ctx)

	service := &BrowserService{
		Port:       browserPort,
		SocketPath: socketPath,
		listener:   listener,
		ctx:        serviceCtx,
		cancel:     cancel,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/open", service.handleOpenURL)

	service.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	service.wg.Add(1)
	go service.serve()

	return service, nil
}

func (bs *BrowserService) serve() {
	defer bs.wg.Done()
	defer func() {
		if err := bs.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logDebug("Browser listener close: %v", err)
		}
	}()

	logDebug("Browser HTTP service starting on port %d", bs.Port)

	err := bs.server.Serve(bs.listener)
	if err != nil && err != http.ErrServerClosed {
		logDebug("Browser HTTP service error: %v", err)
	}

	logDebug("Browser HTTP service stopped")
}

func (bs *BrowserService) handleOpenURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	url := r.URL.Query().Get("url")
	if url == "" {
		http.Error(w, "Missing url parameter", http.StatusBadRequest)
		return
	}

	logDebug("Opening URL in browser: %s", url)

	if err := browser.OpenURL(url); err != nil {
		logDebug("Error opening browser: %v", err)
		fmt.Fprintf(os.Stderr, "Warning: failed to open browser for URL: %s (%v)\n", url, err)
		http.Error(w, "Failed to open browser", http.StatusInternalServerError)
		return
	}

	logDebug("Successfully opened URL in browser")
	fmt.Fprintf(os.Stderr, "Opened in browser: %s\n", url)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("OK")); err != nil {
		logDebug("write browser response: %v", err)
	}
}

// Stop terminates the HTTP server and cleans up any stale socket file.
func (bs *BrowserService) Stop() {
	if bs.cancel != nil {
		logDebug("BrowserService: Stop() called")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := bs.server.Shutdown(shutdownCtx); err != nil {
			logDebug("BrowserService shutdown: %v", err)
		}
		bs.cancel()
		bs.wg.Wait()

		cleanupSocketFile(bs.SocketPath)

		logDebug("BrowserService: stopped")
	}
}

// cleanupSocketFile removes the socket file at the specified path. This is a
// best-effort cleanup of the LOCAL representation only — the real socket
// lives on the remote.
func cleanupSocketFile(socketPath string) {
	if socketPath != "" {
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			logDebug("Failed to remove socket file %s: %v", socketPath, err)
		} else {
			logDebug("Cleaned up socket file: %s", socketPath)
		}
	}
}
