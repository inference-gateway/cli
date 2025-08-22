package services

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
	"time"
)

func TestRetryableHTTPClient_Do_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	config := RetryConfig{
		Enabled:           true,
		MaxAttempts:       3,
		InitialBackoffSec: 1,
		MaxBackoffSec:     5,
		BackoffMultiplier: 2,
	}

	client := NewRetryableHTTPClient(10*time.Second, config)
	req, _ := http.NewRequest("GET", server.URL, nil)

	resp, err := client.Do(req)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestRetryableHTTPClient_Do_RetryOnServerError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		}
	}))
	defer server.Close()

	config := RetryConfig{
		Enabled:           true,
		MaxAttempts:       3,
		InitialBackoffSec: 1,
		MaxBackoffSec:     5,
		BackoffMultiplier: 2,
	}

	client := NewRetryableHTTPClient(10*time.Second, config)
	req, _ := http.NewRequest("GET", server.URL, nil)

	resp, err := client.Do(req)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Fatalf("Expected 3 attempts, got %d", attempts)
	}
	_ = resp.Body.Close()
}

func TestRetryableHTTPClient_Do_MaxRetriesExceeded(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := RetryConfig{
		Enabled:           true,
		MaxAttempts:       3,
		InitialBackoffSec: 1,
		MaxBackoffSec:     5,
		BackoffMultiplier: 2,
	}

	client := NewRetryableHTTPClient(10*time.Second, config)
	req, _ := http.NewRequest("GET", server.URL, nil)

	resp, err := client.Do(req)

	if err == nil && resp.StatusCode == http.StatusInternalServerError {
		_ = resp.Body.Close()
	} else if err != nil {
		t.Logf("Got expected error: %v", err)
	} else {
		t.Fatal("Expected either error or final 500 status code")
	}
	if attempts != 3 {
		t.Fatalf("Expected 3 attempts, got %d", attempts)
	}
}

func TestRetryableHTTPClient_Do_RetryDisabled(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := RetryConfig{
		Enabled:           false,
		MaxAttempts:       3,
		InitialBackoffSec: 1,
		MaxBackoffSec:     5,
		BackoffMultiplier: 2,
	}

	client := NewRetryableHTTPClient(10*time.Second, config)
	req, _ := http.NewRequest("GET", server.URL, nil)

	resp, err := client.Do(req)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("Expected status 500, got %d", resp.StatusCode)
	}
	if attempts != 1 {
		t.Fatalf("Expected 1 attempt, got %d", attempts)
	}
	_ = resp.Body.Close()
}

func TestRetryableHTTPClient_isRetryableError(t *testing.T) {
	client := &RetryableHTTPClient{}

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection refused",
			err:      errors.New("connection refused"),
			expected: true,
		},
		{
			name:     "timeout awaiting response headers",
			err:      errors.New("timeout awaiting response headers"),
			expected: true,
		},
		{
			name:     "i/o timeout",
			err:      errors.New("i/o timeout"),
			expected: true,
		},
		{
			name:     "EOF",
			err:      errors.New("EOF"),
			expected: true,
		},
		{
			name:     "non-retryable error",
			err:      errors.New("invalid request"),
			expected: false,
		},
		{
			name:     "syscall ECONNREFUSED",
			err:      &net.OpError{Err: syscall.ECONNREFUSED},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.isRetryableError(tt.err)
			if result != tt.expected {
				t.Errorf("isRetryableError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestRetryableHTTPClient_isRetryableStatusCode(t *testing.T) {
	client := &RetryableHTTPClient{}

	tests := []struct {
		name       string
		statusCode int
		expected   bool
	}{
		{name: "200 OK", statusCode: http.StatusOK, expected: false},
		{name: "400 Bad Request", statusCode: http.StatusBadRequest, expected: false},
		{name: "408 Request Timeout", statusCode: http.StatusRequestTimeout, expected: true},
		{name: "429 Too Many Requests", statusCode: http.StatusTooManyRequests, expected: true},
		{name: "500 Internal Server Error", statusCode: http.StatusInternalServerError, expected: true},
		{name: "502 Bad Gateway", statusCode: http.StatusBadGateway, expected: true},
		{name: "503 Service Unavailable", statusCode: http.StatusServiceUnavailable, expected: true},
		{name: "504 Gateway Timeout", statusCode: http.StatusGatewayTimeout, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.isRetryableStatusCode(tt.statusCode)
			if result != tt.expected {
				t.Errorf("isRetryableStatusCode(%d) = %v, expected %v", tt.statusCode, result, tt.expected)
			}
		})
	}
}

func TestRetryableHTTPClient_calculateBackoff(t *testing.T) {
	config := RetryConfig{
		InitialBackoffSec: 2,
		MaxBackoffSec:     16,
		BackoffMultiplier: 2,
	}

	client := &RetryableHTTPClient{config: config}

	tests := []struct {
		name     string
		attempt  int
		expected int
	}{
		{name: "attempt 1", attempt: 1, expected: 2},
		{name: "attempt 2", attempt: 2, expected: 4},
		{name: "attempt 3", attempt: 3, expected: 8},
		{name: "attempt 4", attempt: 4, expected: 16}, // capped at max
		{name: "attempt 5", attempt: 5, expected: 16}, // capped at max
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.calculateBackoff(tt.attempt)
			if result != tt.expected {
				t.Errorf("calculateBackoff(%d) = %d, expected %d", tt.attempt, result, tt.expected)
			}
		})
	}
}
