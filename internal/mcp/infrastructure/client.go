package infrastructure

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"

	mcpdomain "github.com/inference-gateway/cli/internal/mcp/domain"
)

// infer speaks one MCP revision. 2026-07-28 is stateless: there is no
// initialize handshake and no session, and every request carries its version
// in params._meta, mirrored into headers. Servers on older revisions are not
// supported; there is no fallback.
const (
	ProtocolVersion = "2026-07-28"

	headerProtocolVersion = "MCP-Protocol-Version"
	headerMethod          = "Mcp-Method"
	headerName            = "Mcp-Name"

	contentTypeJSON = "application/json"
	contentTypeSSE  = "text/event-stream"

	// codeHeaderMismatch and codeUnsupportedProtocolVersion are the JSON-RPC
	// codes 2026-07-28 reserves for request metadata failures.
	codeHeaderMismatch             = -32020
	codeUnsupportedProtocolVersion = -32022

	resultTypeInputRequired = "input_required"

	// requestID is the id of every request. Each one travels alone in its own
	// HTTP exchange, so there is nothing to tell apart.
	requestID = 1
)

var _ mcpdomain.Client = (*Client)(nil)

// errUnsupportedProtocol marks a server that does not speak ProtocolVersion.
var errUnsupportedProtocol = errors.New("MCP server must speak protocol version " + ProtocolVersion)

var clientInfo = map[string]string{"name": "inference-gateway-cli", "version": "1.0.0"}

// Client speaks MCP 2026-07-28 to one server.
type Client struct {
	url  string
	http *http.Client
}

// NewClient returns a client for the server at url. Callers bound each call
// with the context's deadline.
func NewClient(url string) *Client {
	return &Client{url: url, http: &http.Client{}}
}

// Discover asks the server which protocol versions it speaks. Anything but a
// transport failure, auth refusal or a list naming ProtocolVersion means the
// server is on another revision.
func (c *Client) Discover(ctx context.Context) error {
	var result struct {
		SupportedVersions []string `json:"supportedVersions"`
	}
	err := c.rpc(ctx, "server/discover", "", nil, &result)

	var rpcErr *rpcError
	var statusErr *statusError
	switch {
	case err == nil && !slices.Contains(result.SupportedVersions, ProtocolVersion):
		return unsupportedProtocol(errors.New("server/discover does not list it"), result.SupportedVersions)
	case err == nil, errors.Is(err, errUnsupportedProtocol):
		return err
	case errors.As(err, &rpcErr):
		return unsupportedProtocol(err, nil)
	case errors.As(err, &statusErr) && statusErr.status != http.StatusUnauthorized && statusErr.status != http.StatusForbidden:
		return unsupportedProtocol(err, nil)
	}
	return err
}

// ListTools fetches every page of the server's tools.
func (c *Client) ListTools(ctx context.Context) ([]mcpdomain.Tool, error) {
	var tools []mcpdomain.Tool
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Tools []struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := c.rpc(ctx, "tools/list", "", params, &page); err != nil {
			return nil, err
		}
		for _, tool := range page.Tools {
			tools = append(tools, mcpdomain.Tool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			return tools, nil
		}
		cursor = page.NextCursor
	}
}

// CallTool runs a tool and flattens its content blocks to text. arguments is
// always sent, as {} when there are none, since some servers fail to decode a
// call without it.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (mcpdomain.CallResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	var result struct {
		Content    []contentBlock `json:"content"`
		IsError    bool           `json:"isError"`
		ResultType string         `json:"resultType"`
	}
	if err := c.rpc(ctx, "tools/call", name, map[string]any{"name": name, "arguments": args}, &result); err != nil {
		return mcpdomain.CallResult{}, err
	}
	if result.ResultType == resultTypeInputRequired {
		return mcpdomain.CallResult{}, fmt.Errorf("tool %q asked for client input (elicitation or sampling), which infer does not provide", name)
	}

	parts := make([]string, 0, len(result.Content))
	for _, block := range result.Content {
		if text := block.text(); text != "" {
			parts = append(parts, text)
		}
	}
	return mcpdomain.CallResult{Content: strings.Join(parts, "\n"), IsError: result.IsError}, nil
}

// contentBlock is the union of the content types a tool result carries.
type contentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	URI      string `json:"uri"`
	Resource struct {
		Text string `json:"text"`
		Blob string `json:"blob"`
	} `json:"resource"`
}

func (b contentBlock) text() string {
	switch b.Type {
	case "text":
		return b.Text
	case "image":
		return "[Image content]"
	case "audio":
		return "[Audio content]"
	case "resource_link":
		return b.URI
	case "resource":
		if b.Resource.Text != "" {
			return b.Resource.Text
		}
		if b.Resource.Blob != "" {
			return "[Binary resource content]"
		}
	}
	return ""
}

// rpc POSTs one JSON-RPC request and decodes its result. name is the tool of a
// tools/call, mirrored into Mcp-Name.
func (c *Client) rpc(ctx context.Context, method, name string, params map[string]any, result any) error {
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    ProtocolVersion,
		"io.modelcontextprotocol/clientInfo":         clientInfo,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": requestID, "method": method, "params": params})
	if err != nil {
		return fmt.Errorf("encoding %s request: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", contentTypeJSON)
	req.Header.Set("Accept", contentTypeJSON+", "+contentTypeSSE)
	req.Header.Set(headerProtocolVersion, ProtocolVersion)
	req.Header.Set(headerMethod, method)
	if name != "" {
		req.Header.Set(headerName, headerValue(name))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var raw []byte
	if mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mediaType == contentTypeSSE {
		raw, err = sseResponse(resp.Body)
	} else {
		raw, err = io.ReadAll(resp.Body)
	}
	if err != nil {
		return fmt.Errorf("%s: reading http %d response: %w", method, resp.StatusCode, err)
	}

	// A JSON-RPC error can arrive with a 4xx (-32022 comes with 400), so the
	// body is decoded before the status is judged.
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return newStatusError(method, resp.StatusCode, raw)
	}
	if rpcErr := envelope.Error; rpcErr != nil {
		if rpcErr.Code == codeUnsupportedProtocolVersion || rpcErr.Code == codeHeaderMismatch {
			return unsupportedProtocol(rpcErr, rpcErr.supported())
		}
		return fmt.Errorf("%s: %w", method, rpcErr)
	}
	if resp.StatusCode != http.StatusOK || envelope.Result == nil {
		return newStatusError(method, resp.StatusCode, raw)
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("%s: decoding result: %w", method, err)
	}
	return nil
}

// rpcError is a JSON-RPC error object.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("json-rpc error %d: %s", e.Code, e.Message)
}

// supported is data.supported of an UnsupportedProtocolVersionError.
func (e *rpcError) supported() []string {
	var data struct {
		Supported []string `json:"supported"`
	}
	_ = json.Unmarshal(e.Data, &data)
	return data.Supported
}

// statusError is an HTTP response that carried no JSON-RPC result. body is the
// first line of what it carried instead, which from a server on an older
// revision usually names the versions it supports.
type statusError struct {
	method string
	status int
	body   string
}

func newStatusError(method string, status int, raw []byte) *statusError {
	line, _, _ := bytes.Cut(bytes.TrimSpace(raw), []byte("\n"))
	if len(line) > 200 {
		line = line[:200]
	}
	return &statusError{method: method, status: status, body: string(line)}
}

func (e *statusError) Error() string {
	msg := fmt.Sprintf("%s: http %d with no json-rpc result", e.method, e.status)
	if e.body != "" {
		msg += ": " + e.body
	}
	return msg
}

func unsupportedProtocol(cause error, supported []string) error {
	if len(supported) > 0 {
		return fmt.Errorf("%w (server supports %s): %w", errUnsupportedProtocol, strings.Join(supported, ", "), cause)
	}
	return fmt.Errorf("%w: %w", errUnsupportedProtocol, cause)
}

// headerValue passes printable ASCII through and wraps anything else as
// =?base64?<value>?=, the encoding MCP headers use for non-ASCII values.
func headerValue(value string) string {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(value)) + "?="
		}
	}
	return value
}

// sseResponse returns the data of the first event on an SSE stream that
// carries a JSON-RPC response, skipping any notification sent before it. It
// reads with bufio.Reader because a tool result can outgrow Scanner's line
// limit.
func sseResponse(r io.Reader) ([]byte, error) {
	reader := bufio.NewReader(r)
	var data []byte
	for {
		line, readErr := reader.ReadBytes('\n')
		line = bytes.TrimRight(line, "\r\n")

		if value, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, bytes.TrimPrefix(value, []byte(" "))...)
		} else if len(line) == 0 { // a blank line ends the event
			if isJSONRPCResponse(data) {
				return data, nil
			}
			data = nil
		}

		if readErr != nil {
			if isJSONRPCResponse(data) { // the stream closed without a final blank line
				return data, nil
			}
			if readErr == io.EOF {
				return nil, errors.New("sse stream ended without a json-rpc response")
			}
			return nil, readErr
		}
	}
}

// isJSONRPCResponse reports whether an SSE event's data is a response (it has
// an id and no method) rather than a notification or a server request.
func isJSONRPCResponse(data []byte) bool {
	var msg struct {
		ID     any    `json:"id"`
		Method string `json:"method"`
	}
	return len(data) > 0 && json.Unmarshal(data, &msg) == nil && msg.ID != nil && msg.Method == ""
}
