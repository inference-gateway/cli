package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	mcp "github.com/metoro-io/mcp-golang"
	mcphttp "github.com/metoro-io/mcp-golang/transport/http"
)

// MCPClientManager manages MCP client connections and tool execution
// It implements domain.MCPClient interface
type MCPClientManager struct {
	config           *config.MCPConfig
	mu               sync.RWMutex
	connectedServers map[string]bool
}

// NewMCPClientManager creates a new MCP client manager
func NewMCPClientManager(cfg *config.MCPConfig) *MCPClientManager {
	return &MCPClientManager{
		config:           cfg,
		connectedServers: make(map[string]bool),
	}
}

// DiscoverTools discovers tools from all enabled MCP servers
func (m *MCPClientManager) DiscoverTools(ctx context.Context) (map[string][]domain.MCPDiscoveredTool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.connectedServers = make(map[string]bool)

	result := make(map[string][]domain.MCPDiscoveredTool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, server := range m.config.Servers {
		if !server.Enabled {
			logger.Debug("Skipping disabled MCP server", "server", server.Name)
			continue
		}

		wg.Add(1)
		go func(srv config.MCPServerEntry) {
			defer wg.Done()

			timeout := time.Duration(srv.GetTimeout(m.config.ConnectionTimeout)) * time.Second
			serverCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			tools, err := m.discoverServerTools(serverCtx, srv)
			if err != nil {
				logger.Warn("Failed to discover tools from MCP server",
					"server", srv.Name,
					"url", srv.URL,
					"error", err)
				return
			}

			mu.Lock()
			result[srv.Name] = tools
			m.connectedServers[srv.Name] = true
			mu.Unlock()

			logger.Info("Discovered tools from MCP server",
				"server", srv.Name,
				"tool_count", len(tools))
		}(server)
	}

	wg.Wait()

	return result, nil
}

// discoverServerTools discovers tools from a single MCP server
func (m *MCPClientManager) discoverServerTools(ctx context.Context, server config.MCPServerEntry) ([]domain.MCPDiscoveredTool, error) {
	transport := mcphttp.NewHTTPClientTransport(server.URL)

	client := mcp.NewClientWithInfo(transport, mcp.ClientInfo{
		Name:    "inference-gateway-cli",
		Version: "1.0.0",
	})

	_, err := client.Initialize(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
	}

	toolsResp, err := client.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	tools := make([]domain.MCPDiscoveredTool, 0, len(toolsResp.Tools))
	for _, tool := range toolsResp.Tools {
		description := ""
		if tool.Description != nil {
			description = *tool.Description
		}

		tools = append(tools, domain.MCPDiscoveredTool{
			ServerName:  server.Name,
			Name:        tool.Name,
			Description: description,
			InputSchema: tool.InputSchema,
		})
	}

	return tools, nil
}

// CallTool executes a tool on the specified MCP server
func (m *MCPClientManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Find server config
	var server *config.MCPServerEntry
	for _, srv := range m.config.Servers {
		if srv.Name == serverName {
			server = &srv
			break
		}
	}

	if server == nil {
		return nil, fmt.Errorf("MCP server %q not found in configuration", serverName)
	}

	if !server.Enabled {
		return nil, fmt.Errorf("MCP server %q is disabled", serverName)
	}

	// Create timeout context
	timeout := time.Duration(server.GetTimeout(m.config.ConnectionTimeout)) * time.Second
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create HTTP transport (stateless - new connection per request)
	transport := mcphttp.NewHTTPClientTransport(server.URL)

	// Create MCP client
	client := mcp.NewClientWithInfo(transport, mcp.ClientInfo{
		Name:    "inference-gateway-cli",
		Version: "1.0.0",
	})

	// Initialize connection
	_, err := client.Initialize(execCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
	}

	// Call the tool
	result, err := client.CallTool(execCtx, toolName, args)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool %q: %w", toolName, err)
	}

	return result, nil
}

// GetMCPServerStatus returns the current MCP server connection status
func (m *MCPClientManager) GetMCPServerStatus() *domain.MCPServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalServers := 0
	for _, server := range m.config.Servers {
		if server.Enabled {
			totalServers++
		}
	}

	connectedServers := len(m.connectedServers)

	return &domain.MCPServerStatus{
		TotalServers:     totalServers,
		ConnectedServers: connectedServers,
	}
}

// Close cleans up resources (minimal for stateless design)
func (m *MCPClientManager) Close() error {
	// No persistent connections to close in stateless HTTP design
	return nil
}
