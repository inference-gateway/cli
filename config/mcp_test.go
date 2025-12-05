package config

import (
	"testing"
)

func TestDefaultMCPConfig(t *testing.T) {
	cfg := DefaultMCPConfig()

	if cfg == nil {
		t.Fatal("DefaultMCPConfig() returned nil")
	}

	if cfg.Enabled {
		t.Error("Expected Enabled to be false by default")
	}

	if cfg.ConnectionTimeout != 30 {
		t.Errorf("Expected ConnectionTimeout to be 30, got %d", cfg.ConnectionTimeout)
	}

	if cfg.DiscoveryTimeout != 30 {
		t.Errorf("Expected DiscoveryTimeout to be 30, got %d", cfg.DiscoveryTimeout)
	}

	if cfg.Servers == nil {
		t.Error("Expected Servers to be initialized")
	}

	if len(cfg.Servers) != 0 {
		t.Errorf("Expected Servers to be empty, got %d servers", len(cfg.Servers))
	}
}

func TestMCPServerEntry_ShouldIncludeTool(t *testing.T) {
	tests := []struct {
		name          string
		entry         MCPServerEntry
		toolName      string
		shouldInclude bool
	}{
		{
			name: "no include or exclude lists - include all",
			entry: MCPServerEntry{
				Name: "test-server",
			},
			toolName:      "any_tool",
			shouldInclude: true,
		},
		{
			name: "include list only - tool in list",
			entry: MCPServerEntry{
				Name:         "test-server",
				IncludeTools: []string{"tool1", "tool2", "tool3"},
			},
			toolName:      "tool2",
			shouldInclude: true,
		},
		{
			name: "include list only - tool not in list",
			entry: MCPServerEntry{
				Name:         "test-server",
				IncludeTools: []string{"tool1", "tool2", "tool3"},
			},
			toolName:      "tool4",
			shouldInclude: false,
		},
		{
			name: "exclude list only - tool in list",
			entry: MCPServerEntry{
				Name:         "test-server",
				ExcludeTools: []string{"dangerous_tool", "risky_tool"},
			},
			toolName:      "dangerous_tool",
			shouldInclude: false,
		},
		{
			name: "exclude list only - tool not in list",
			entry: MCPServerEntry{
				Name:         "test-server",
				ExcludeTools: []string{"dangerous_tool", "risky_tool"},
			},
			toolName:      "safe_tool",
			shouldInclude: true,
		},
		{
			name: "both lists - tool in include list",
			entry: MCPServerEntry{
				Name:         "test-server",
				IncludeTools: []string{"tool1", "tool2"},
				ExcludeTools: []string{"tool2", "tool3"}, // tool2 in both - include takes precedence
			},
			toolName:      "tool2",
			shouldInclude: true,
		},
		{
			name: "both lists - tool not in include list",
			entry: MCPServerEntry{
				Name:         "test-server",
				IncludeTools: []string{"tool1", "tool2"},
				ExcludeTools: []string{"tool3"},
			},
			toolName:      "tool3",
			shouldInclude: false,
		},
		{
			name: "empty lists - include all",
			entry: MCPServerEntry{
				Name:         "test-server",
				IncludeTools: []string{},
				ExcludeTools: []string{},
			},
			toolName:      "any_tool",
			shouldInclude: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.entry.ShouldIncludeTool(tt.toolName)
			if result != tt.shouldInclude {
				t.Errorf("ShouldIncludeTool(%q) = %v, want %v", tt.toolName, result, tt.shouldInclude)
			}
		})
	}
}

func TestMCPServerEntry_GetTimeout(t *testing.T) {
	tests := []struct {
		name           string
		serverTimeout  int
		globalTimeout  int
		expectedResult int
	}{
		{
			name:           "server timeout set - use server timeout",
			serverTimeout:  60,
			globalTimeout:  30,
			expectedResult: 60,
		},
		{
			name:           "server timeout not set - use global timeout",
			serverTimeout:  0,
			globalTimeout:  45,
			expectedResult: 45,
		},
		{
			name:           "neither set - use default",
			serverTimeout:  0,
			globalTimeout:  0,
			expectedResult: 30,
		},
		{
			name:           "server timeout zero but global set - use global",
			serverTimeout:  0,
			globalTimeout:  120,
			expectedResult: 120,
		},
		{
			name:           "both set - server takes precedence",
			serverTimeout:  90,
			globalTimeout:  30,
			expectedResult: 90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := MCPServerEntry{
				Timeout: tt.serverTimeout,
			}
			result := entry.GetTimeout(tt.globalTimeout)
			if result != tt.expectedResult {
				t.Errorf("GetTimeout(%d) = %d, want %d", tt.globalTimeout, result, tt.expectedResult)
			}
		})
	}
}

func TestMCPConstants(t *testing.T) {
	if MCPFileName != "mcp.yaml" {
		t.Errorf("Expected MCPFileName to be 'mcp.yaml', got %q", MCPFileName)
	}

	expectedPath := ConfigDirName + "/" + MCPFileName
	if DefaultMCPPath != expectedPath {
		t.Errorf("Expected DefaultMCPPath to be %q, got %q", expectedPath, DefaultMCPPath)
	}
}
