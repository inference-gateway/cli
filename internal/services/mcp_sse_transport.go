package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	transport "github.com/metoro-io/mcp-golang/transport"
)

// SSEHTTPClientTransport implements an SSE-aware HTTP client transport for MCP
// This handles servers that respond with Server-Sent Events format
type SSEHTTPClientTransport struct {
	endpoint       string
	messageHandler func(ctx context.Context, message *transport.BaseJsonRpcMessage)
	errorHandler   func(error)
	closeHandler   func()
	mu             sync.RWMutex
	client         *http.Client
	headers        map[string]string
}

// NewSSEHTTPClientTransport creates a new SSE-aware HTTP client transport
func NewSSEHTTPClientTransport(endpoint string) *SSEHTTPClientTransport {
	return &SSEHTTPClientTransport{
		endpoint: endpoint,
		client:   &http.Client{},
		headers:  make(map[string]string),
	}
}

// WithHeader adds a header to the request
func (t *SSEHTTPClientTransport) WithHeader(key, value string) *SSEHTTPClientTransport {
	t.headers[key] = value
	return t
}

// Start implements Transport.Start
func (t *SSEHTTPClientTransport) Start(ctx context.Context) error {
	return nil
}

// Send implements Transport.Send
func (t *SSEHTTPClientTransport) Send(ctx context.Context, message *transport.BaseJsonRpcMessage) error {
	cleanedMessage := t.cleanNullParams(message)

	jsonData, err := json.Marshal(cleanedMessage)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	url := t.endpoint
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned error: %s (status: %d)", string(body), resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		return t.handleSSEResponse(ctx, resp.Body)
	}

	return t.handleJSONResponse(ctx, resp.Body)
}

// cleanNullParams removes null values from params to satisfy strict JSON-RPC servers
func (t *SSEHTTPClientTransport) cleanNullParams(message *transport.BaseJsonRpcMessage) *transport.BaseJsonRpcMessage {
	if message.JsonRpcRequest == nil {
		return message
	}

	cleaned := &transport.BaseJsonRpcMessage{
		Type: message.Type,
		JsonRpcRequest: &transport.BaseJSONRPCRequest{
			Jsonrpc: message.JsonRpcRequest.Jsonrpc,
			Method:  message.JsonRpcRequest.Method,
			Id:      message.JsonRpcRequest.Id,
		},
	}

	if len(message.JsonRpcRequest.Params) > 0 {
		cleaned.JsonRpcRequest.Params = t.removeNullParams(message.JsonRpcRequest.Params)
	}

	return cleaned
}

// removeNullParams removes null values from JSON params
func (t *SSEHTTPClientTransport) removeNullParams(params json.RawMessage) json.RawMessage {
	var paramsMap map[string]any
	if err := json.Unmarshal(params, &paramsMap); err != nil {
		return params
	}

	cleanedParams := make(map[string]any)
	for k, v := range paramsMap {
		if v != nil {
			cleanedParams[k] = v
		}
	}

	if len(cleanedParams) == 0 {
		return nil
	}

	cleanedBytes, err := json.Marshal(cleanedParams)
	if err != nil {
		return params
	}

	return cleanedBytes
}

// handleSSEResponse parses SSE format responses
func (t *SSEHTTPClientTransport) handleSSEResponse(ctx context.Context, body io.Reader) error {
	scanner := bufio.NewScanner(body)
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if len(dataLines) > 0 {
				jsonData := strings.Join(dataLines, "\n")
				if err := t.parseAndDispatchJSON(ctx, []byte(jsonData)); err != nil {
					return err
				}
				dataLines = nil
			}
			continue
		}

		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(line[5:])
			dataLines = append(dataLines, data)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading SSE stream: %w", err)
	}

	return nil
}

// handleJSONResponse parses plain JSON responses
func (t *SSEHTTPClientTransport) handleJSONResponse(ctx context.Context, body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	return t.parseAndDispatchJSON(ctx, data)
}

// parseAndDispatchJSON tries to parse JSON as different message types and dispatches to handler
func (t *SSEHTTPClientTransport) parseAndDispatchJSON(ctx context.Context, data []byte) error {
	t.mu.RLock()
	handler := t.messageHandler
	t.mu.RUnlock()

	if handler == nil {
		return nil
	}

	var response transport.BaseJSONRPCResponse
	if err := json.Unmarshal(data, &response); err == nil {
		handler(ctx, transport.NewBaseMessageResponse(&response))
		return nil
	}

	var errorResponse transport.BaseJSONRPCError
	if err := json.Unmarshal(data, &errorResponse); err == nil {
		handler(ctx, transport.NewBaseMessageError(&errorResponse))
		return nil
	}

	var notification transport.BaseJSONRPCNotification
	if err := json.Unmarshal(data, &notification); err == nil {
		handler(ctx, transport.NewBaseMessageNotification(&notification))
		return nil
	}

	var request transport.BaseJSONRPCRequest
	if err := json.Unmarshal(data, &request); err == nil {
		handler(ctx, transport.NewBaseMessageRequest(&request))
		return nil
	}

	return fmt.Errorf("received invalid JSON: %s", string(data))
}

// Close implements Transport.Close
func (t *SSEHTTPClientTransport) Close() error {
	if t.closeHandler != nil {
		t.closeHandler()
	}
	return nil
}

// SetCloseHandler implements Transport.SetCloseHandler
func (t *SSEHTTPClientTransport) SetCloseHandler(handler func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closeHandler = handler
}

// SetErrorHandler implements Transport.SetErrorHandler
func (t *SSEHTTPClientTransport) SetErrorHandler(handler func(error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.errorHandler = handler
}

// SetMessageHandler implements Transport.SetMessageHandler
func (t *SSEHTTPClientTransport) SetMessageHandler(handler func(ctx context.Context, message *transport.BaseJsonRpcMessage)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messageHandler = handler
}
