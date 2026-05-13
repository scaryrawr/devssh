package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/google/uuid"
)

//go:embed notification-sender.sh
var notificationSenderScript string

// NotificationRequest is the JSON envelope accepted by the /notify endpoint.
type NotificationRequest struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

// NotificationService runs a local HTTP server that surfaces remote desktop
// notifications via beeep. The remote side reaches it through a streamlocal
// SSH forward pointed at SocketPath.
type NotificationService struct {
	Port       int
	SocketPath string
	server     *http.Server
	listener   net.Listener
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewNotificationService starts the local HTTP notification service.
func NewNotificationService(ctx context.Context) (*NotificationService, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to create local listener: %w", err)
	}

	notificationPort := listener.Addr().(*net.TCPAddr).Port

	socketId := uuid.New()
	socketPath := "/tmp/devssh-notification-" + socketId.String() + ".sock"

	logDebug("Local notification HTTP service created on port: %d, socket path: %s", notificationPort, socketPath)

	serviceCtx, cancel := context.WithCancel(ctx)

	service := &NotificationService{
		Port:       notificationPort,
		SocketPath: socketPath,
		listener:   listener,
		ctx:        serviceCtx,
		cancel:     cancel,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/notify", service.handleNotification)

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

func (ns *NotificationService) serve() {
	defer ns.wg.Done()
	defer func() {
		if err := ns.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logDebug("Notification listener close: %v", err)
		}
	}()

	logDebug("Notification HTTP service starting on port %d", ns.Port)

	err := ns.server.Serve(ns.listener)
	if err != nil && err != http.ErrServerClosed {
		logDebug("Notification HTTP service error: %v", err)
	}

	logDebug("Notification HTTP service stopped")
}

func (ns *NotificationService) handleNotification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req NotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		http.Error(w, "Missing title", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "Missing message", http.StatusBadRequest)
		return
	}

	const maxTitleLen = 100
	const maxMessageLen = 500

	req.Title = truncateWithEllipsis(req.Title, maxTitleLen)
	req.Message = truncateWithEllipsis(req.Message, maxMessageLen)

	logDebug("Sending notification: title=%s, message=%s", req.Title, req.Message)

	if err := beeep.Notify(req.Title, req.Message, ""); err != nil {
		logDebug("Error sending notification: %v", err)
		http.Error(w, "Failed to send notification", http.StatusInternalServerError)
		return
	}

	logDebug("Successfully sent notification")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("OK")); err != nil {
		logDebug("write notification response: %v", err)
	}
}

func truncateWithEllipsis(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

// Stop terminates the HTTP server.
func (ns *NotificationService) Stop() {
	if ns.cancel != nil {
		logDebug("NotificationService: Stop() called")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := ns.server.Shutdown(shutdownCtx); err != nil {
			logDebug("NotificationService shutdown: %v", err)
		}
		ns.cancel()
		ns.wg.Wait()

		cleanupSocketFile(ns.SocketPath)

		logDebug("NotificationService: stopped")
	}
}
