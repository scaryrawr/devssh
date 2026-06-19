package devssh

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewBrowserService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewBrowserService(ctx)
	if err != nil {
		t.Fatalf("Failed to create browser service: %v", err)
	}
	defer service.Stop()

	if service.Port == 0 {
		t.Error("Browser service port should not be 0")
	}

	if service.SocketPath == "" {
		t.Error("Browser service socket path should not be empty")
	}

	if !strings.HasPrefix(service.SocketPath, "/tmp/devssh-browser-") || !strings.HasSuffix(service.SocketPath, ".sock") {
		t.Errorf("Socket path has unexpected format: %s", service.SocketPath)
	}

	if service.listener == nil {
		t.Error("Browser service listener should not be nil")
	}

	if service.server == nil {
		t.Error("Browser service HTTP server should not be nil")
	}
}

func TestBrowserServiceHandlesHTTPRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewBrowserService(ctx)
	if err != nil {
		t.Fatalf("Failed to create browser service: %v", err)
	}
	defer service.Stop()

	time.Sleep(100 * time.Millisecond)

	testURL := "https://example.com"
	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/open?url=%s", service.Port, url.QueryEscape(testURL)),
		"application/x-www-form-urlencoded",
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to send HTTP request: %v", err)
	}
	defer resp.Body.Close()

	// Browser opening may fail in CI; any 200 or 500 is acceptable.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Unexpected status code: %d", resp.StatusCode)
	}
}

func TestBrowserServiceStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewBrowserService(ctx)
	if err != nil {
		t.Fatalf("Failed to create browser service: %v", err)
	}

	service.Stop()

	time.Sleep(100 * time.Millisecond)
	_, err = net.Dial("tcp", fmt.Sprintf("localhost:%d", service.Port))
	if err == nil {
		t.Error("Expected connection to fail after service stop, but it succeeded")
	}
}

func TestBrowserHTTPEndpointMethodValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewBrowserService(ctx)
	if err != nil {
		t.Fatalf("Failed to create browser service: %v", err)
	}
	defer service.Stop()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/open?url=https://example.com", service.Port))
	if err != nil {
		t.Fatalf("Failed to send GET request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d for GET request, got %d", http.StatusMethodNotAllowed, resp.StatusCode)
	}
}

func TestBrowserHTTPEndpointMissingURL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service, err := NewBrowserService(ctx)
	if err != nil {
		t.Fatalf("Failed to create browser service: %v", err)
	}
	defer service.Stop()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/open", service.Port),
		"application/x-www-form-urlencoded",
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to send POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status %d for request without URL, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}
