package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewNotificationService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewNotificationService(ctx)
	if err != nil {
		t.Fatalf("Failed to create notification service: %v", err)
	}
	defer service.Stop()

	if service.Port == 0 {
		t.Error("Notification service port should not be 0")
	}

	if service.SocketPath == "" {
		t.Error("Notification service socket path should not be empty")
	}

	if !strings.HasPrefix(service.SocketPath, "/tmp/devssh-notification-") || !strings.HasSuffix(service.SocketPath, ".sock") {
		t.Errorf("Socket path has unexpected format: %s", service.SocketPath)
	}

	if service.listener == nil {
		t.Error("Notification service listener should not be nil")
	}

	if service.server == nil {
		t.Error("Notification service HTTP server should not be nil")
	}
}

func TestNotificationServiceHandlesHTTPRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewNotificationService(ctx)
	if err != nil {
		t.Fatalf("Failed to create notification service: %v", err)
	}
	defer service.Stop()

	time.Sleep(100 * time.Millisecond)

	testReq := NotificationRequest{
		Title:   "Test Title",
		Message: "Test Message",
	}
	jsonData, err := json.Marshal(testReq)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/notify", service.Port),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		t.Fatalf("Failed to send HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Unexpected status code: %d", resp.StatusCode)
	}
}

func TestNotificationServiceStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewNotificationService(ctx)
	if err != nil {
		t.Fatalf("Failed to create notification service: %v", err)
	}

	service.Stop()

	time.Sleep(100 * time.Millisecond)
	_, err = net.Dial("tcp", fmt.Sprintf("localhost:%d", service.Port))
	if err == nil {
		t.Error("Expected connection to fail after service stop, but it succeeded")
	}
}

func TestNotificationHTTPEndpointMethodValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewNotificationService(ctx)
	if err != nil {
		t.Fatalf("Failed to create notification service: %v", err)
	}
	defer service.Stop()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/notify", service.Port))
	if err != nil {
		t.Fatalf("Failed to send GET request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d for GET request, got %d", http.StatusMethodNotAllowed, resp.StatusCode)
	}
}

func TestNotificationHTTPEndpointMissingTitle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewNotificationService(ctx)
	if err != nil {
		t.Fatalf("Failed to create notification service: %v", err)
	}
	defer service.Stop()

	time.Sleep(100 * time.Millisecond)

	testReq := NotificationRequest{Message: "Test Message"}
	jsonData, err := json.Marshal(testReq)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/notify", service.Port),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		t.Fatalf("Failed to send POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status %d for request without title, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestNotificationHTTPEndpointMissingMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewNotificationService(ctx)
	if err != nil {
		t.Fatalf("Failed to create notification service: %v", err)
	}
	defer service.Stop()

	time.Sleep(100 * time.Millisecond)

	testReq := NotificationRequest{Title: "Test Title"}
	jsonData, err := json.Marshal(testReq)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/notify", service.Port),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		t.Fatalf("Failed to send POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status %d for request without message, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestNotificationHTTPEndpointInvalidJSON(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewNotificationService(ctx)
	if err != nil {
		t.Fatalf("Failed to create notification service: %v", err)
	}
	defer service.Stop()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/notify", service.Port),
		"application/json",
		bytes.NewBufferString("not valid json"),
	)
	if err != nil {
		t.Fatalf("Failed to send POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid JSON, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestTruncateWithEllipsis(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"short", "hi", 100, "hi"},
		{"exact", "abcd", 4, "abcd"},
		{"truncate", strings.Repeat("a", 10), 5, "aa..."},
		{"max<=3", "abcdef", 3, "abc"},
		{"zero", "abc", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateWithEllipsis(tt.in, tt.max); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
