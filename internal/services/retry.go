package services

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/inference-gateway/cli/internal/logger"
)

// RetryConfig contains retry logic settings
type RetryConfig struct {
	Enabled           bool
	MaxAttempts       int
	InitialBackoffSec int
	MaxBackoffSec     int
	BackoffMultiplier int
}

// RetryableHTTPClient wraps http.Client with retry logic
type RetryableHTTPClient struct {
	client *http.Client
	config RetryConfig
}

// NewRetryableHTTPClient creates a new retryable HTTP client
func NewRetryableHTTPClient(timeout time.Duration, config RetryConfig) *RetryableHTTPClient {
	return &RetryableHTTPClient{
		client: &http.Client{Timeout: timeout},
		config: config,
	}
}

// Do executes an HTTP request with retry logic
func (r *RetryableHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if !r.config.Enabled {
		return r.client.Do(req)
	}

	var lastErr error
	for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
		reqClone := r.cloneRequest(req)

		logger.Debug("HTTP request attempt",
			"attempt", attempt,
			"max_attempts", r.config.MaxAttempts,
			"url", req.URL.String(),
			"method", req.Method)

		resp, err := r.client.Do(reqClone)

		if err == nil {
			if !r.isRetryableStatusCode(resp.StatusCode) || attempt >= r.config.MaxAttempts {
				logger.Debug("HTTP request succeeded",
					"attempt", attempt,
					"status_code", resp.StatusCode)
				return resp, nil
			}
			_ = resp.Body.Close()
			logger.Debug("Received retryable status code",
				"status_code", resp.StatusCode,
				"attempt", attempt)
		} else if !r.isRetryableError(err) {
			logger.Debug("Non-retryable error encountered",
				"error", err.Error(),
				"attempt", attempt)
			return nil, err
		} else {
			logger.Debug("Retryable error encountered",
				"error", err.Error(),
				"attempt", attempt)
		}

		lastErr = err

		if attempt < r.config.MaxAttempts {
			backoff := r.calculateBackoff(attempt)
			logger.Debug("Waiting before retry",
				"backoff_seconds", backoff,
				"attempt", attempt)

			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(time.Duration(backoff) * time.Second):
			}
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("max retry attempts (%d) exceeded, last error: %w", r.config.MaxAttempts, lastErr)
	}

	return nil, fmt.Errorf("max retry attempts (%d) exceeded", r.config.MaxAttempts)
}

// cloneRequest creates a deep copy of an HTTP request
func (r *RetryableHTTPClient) cloneRequest(req *http.Request) *http.Request {
	clone := req.Clone(req.Context())
	return clone
}

// isRetryableError determines if an error should trigger a retry
func (r *RetryableHTTPClient) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	if netErr, ok := err.(net.Error); ok {
		if netErr.Timeout() {
			return true
		}
	}

	if strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "connection reset") ||
		strings.Contains(err.Error(), "no such host") ||
		strings.Contains(err.Error(), "timeout awaiting response headers") ||
		strings.Contains(err.Error(), "i/o timeout") ||
		strings.Contains(err.Error(), "EOF") {
		return true
	}

	if opErr, ok := err.(*net.OpError); ok {
		if syscallErr, ok := opErr.Err.(*syscall.Errno); ok {
			switch *syscallErr {
			case syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ETIMEDOUT:
				return true
			}
		}
	}

	return false
}

// isRetryableStatusCode determines if an HTTP status code should trigger a retry
func (r *RetryableHTTPClient) isRetryableStatusCode(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, // 408
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	default:
		return false
	}
}

// calculateBackoff calculates the backoff delay for a given attempt
func (r *RetryableHTTPClient) calculateBackoff(attempt int) int {
	backoff := r.config.InitialBackoffSec
	for i := 1; i < attempt; i++ {
		backoff *= r.config.BackoffMultiplier
	}

	if backoff > r.config.MaxBackoffSec {
		backoff = r.config.MaxBackoffSec
	}

	return backoff
}
