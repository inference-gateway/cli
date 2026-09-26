package mcp

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	containermocks "github.com/inference-gateway/cli/tests/mocks/container"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	mcpdomain "github.com/inference-gateway/cli/internal/mcp/domain"
	containerruntime "github.com/inference-gateway/cli/internal/platform/container"
)

func TestNewSupervisor(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled:           true,
		ConnectionTimeout: 30,
		DiscoveryTimeout:  30,
		Servers: []config.MCPServerEntry{
			{
				Name:    "test-server",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8080:8080"},
				Path:    "/mcp",
				Enabled: true,
			},
		},
	}

	sessionID := convdomain.GenerateSessionID()
	supervisor := NewSupervisor(sessionID, cfg, nil, nil)

	if supervisor == nil {
		t.Fatal("Expected non-nil supervisor")
	}

	if supervisor.config != cfg {
		t.Error("Expected config to be set correctly")
	}

	clients := supervisor.clients
	if len(clients) != 1 {
		t.Errorf("Expected 1 client, got %d", len(clients))
	}
}

func TestSupervisor_Close(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{},
	}

	sessionID := convdomain.GenerateSessionID()
	supervisor := NewSupervisor(sessionID, cfg, nil, nil)

	err := supervisor.Close()
	if err != nil {
		t.Errorf("Close() returned unexpected error: %v", err)
	}
}

func TestSupervisor_GetClients_NoServers(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{},
	}

	sessionID := convdomain.GenerateSessionID()
	supervisor := NewSupervisor(sessionID, cfg, nil, nil)

	clients := supervisor.clients

	if len(clients) != 0 {
		t.Errorf("Expected 0 clients, got %d", len(clients))
	}
}

func TestSupervisor_GetClients_DisabledServer(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{
			{
				Name:    "disabled-server",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8080:8080"},
				Path:    "/mcp",
				Enabled: false,
			},
		},
	}

	sessionID := convdomain.GenerateSessionID()
	supervisor := NewSupervisor(sessionID, cfg, nil, nil)

	clients := supervisor.clients

	if len(clients) != 0 {
		t.Errorf("Expected 0 clients for disabled server, got %d", len(clients))
	}
}

func TestSupervisor_GetClients_MultipleServers(t *testing.T) {
	cfg := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{
			{
				Name:    "server1",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8080:8080"},
				Path:    "/mcp",
				Enabled: true,
			},
			{
				Name:    "server2",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8081:8080"},
				Path:    "/mcp",
				Enabled: true,
			},
			{
				Name:    "disabled-server",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8082:8080"},
				Path:    "/mcp",
				Enabled: false,
			},
		},
	}

	sessionID := convdomain.GenerateSessionID()
	supervisor := NewSupervisor(sessionID, cfg, nil, nil)

	clients := supervisor.clients

	if len(clients) != 2 {
		t.Errorf("Expected 2 clients, got %d", len(clients))
	}
}

// recordingNotifier is a thread-safe agentdomain.UINotifier that collects every
// pushed event so tests can assert what a producer emitted.
type recordingNotifier struct {
	mu     sync.Mutex
	events []any
}

func (r *recordingNotifier) Notify(event any) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *recordingNotifier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// waitForCount polls until the recorder reaches at least n events or the deadline
// passes, returning the final count. Pushes happen on a goroutine, so we poll.
func (r *recordingNotifier) waitForCount(n int, within time.Duration) int {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if r.count() >= n {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return r.count()
}

func monitoringTestConfig() *config.MCPConfig {
	return &config.MCPConfig{
		Enabled:               true,
		LivenessProbeEnabled:  false,
		LivenessProbeInterval: 10,
		Servers: []config.MCPServerEntry{
			{
				Name:    "test-server",
				Scheme:  "http",
				Host:    "localhost",
				Ports:   []string{"8080:8080"},
				Path:    "/mcp",
				Enabled: true,
			},
		},
	}
}

// connectAll marks every client connected so the initial-status push fires
// (initializeClient does not connect by itself - a live probe would).
func connectAll(s *Supervisor) {
	for _, c := range s.clients {
		c.mu.Lock()
		c.isConnected = true
		c.mu.Unlock()
	}
}

// sendStatusUpdateWithTools pushes a ServerStatusUpdateEvent through the
// injected notifier (no channel). This is the synchronous push primitive every
// probe path funnels through.
func TestSupervisor_PushesStatusThroughNotifier(t *testing.T) {
	rec := &recordingNotifier{}
	supervisor := NewSupervisor(convdomain.GenerateSessionID(), monitoringTestConfig(), nil, rec)

	supervisor.sendStatusUpdateWithTools("test-server", true, nil)

	if got := rec.count(); got != 1 {
		t.Fatalf("expected 1 push, got %d", got)
	}
	ev, ok := rec.events[0].(mcpdomain.ServerStatusUpdateEvent)
	if !ok {
		t.Fatalf("expected ServerStatusUpdateEvent, got %T", rec.events[0])
	}
	if ev.ServerName != "test-server" || !ev.Connected {
		t.Errorf("unexpected event payload: %+v", ev)
	}
}

// StartMonitoring pushes the initial status for connected clients through the
// notifier - there is no channel to drain. A second call is a no-op (idempotent),
// so the count stays at one connected client rather than doubling.
func TestSupervisor_StartMonitoring_Idempotent(t *testing.T) {
	rec := &recordingNotifier{}
	supervisor := NewSupervisor(convdomain.GenerateSessionID(), monitoringTestConfig(), nil, rec)
	connectAll(supervisor)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	supervisor.StartMonitoring(ctx)
	supervisor.StartMonitoring(ctx)

	rec.waitForCount(1, time.Second)
	time.Sleep(50 * time.Millisecond)
	if got := rec.count(); got != 1 {
		t.Errorf("expected exactly 1 initial status push (idempotent), got %d", got)
	}

	_ = supervisor.Close()
}

// With liveness probes disabled, StartMonitoring still pushes the initial status
// once through the notifier and starts no probe goroutines.
func TestSupervisor_StartMonitoring_DisabledProbes(t *testing.T) {
	rec := &recordingNotifier{}
	supervisor := NewSupervisor(convdomain.GenerateSessionID(), monitoringTestConfig(), nil, rec)
	connectAll(supervisor)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	supervisor.StartMonitoring(ctx)

	if got := rec.waitForCount(1, time.Second); got != 1 {
		t.Errorf("expected 1 initial status push with probes disabled, got %d", got)
	}

	_ = supervisor.Close()
}

func TestMCPServerEntry_ShouldIncludeTool(t *testing.T) {
	tests := []struct {
		name          string
		entry         config.MCPServerEntry
		toolName      string
		shouldInclude bool
	}{
		{
			name: "no filters - include all",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{},
				ExcludeTools: []string{},
			},
			toolName:      "anyTool",
			shouldInclude: true,
		},
		{
			name: "include filter - tool in list",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{"readFile", "writeFile"},
				ExcludeTools: []string{},
			},
			toolName:      "readFile",
			shouldInclude: true,
		},
		{
			name: "include filter - tool not in list",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{"readFile", "writeFile"},
				ExcludeTools: []string{},
			},
			toolName:      "deleteTool",
			shouldInclude: false,
		},
		{
			name: "exclude filter - tool in list",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{},
				ExcludeTools: []string{"dangerousTool", "deleteTool"},
			},
			toolName:      "dangerousTool",
			shouldInclude: false,
		},
		{
			name: "exclude filter - tool not in list",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{},
				ExcludeTools: []string{"dangerousTool", "deleteTool"},
			},
			toolName:      "safeTool",
			shouldInclude: true,
		},
		{
			name: "both filters - include takes precedence",
			entry: config.MCPServerEntry{
				Name:         "server1",
				IncludeTools: []string{"readFile", "writeFile"},
				ExcludeTools: []string{"writeFile"},
			},
			toolName:      "writeFile",
			shouldInclude: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.entry.ShouldIncludeTool(tt.toolName)
			if result != tt.shouldInclude {
				t.Errorf("ShouldIncludeTool(%s) = %v, expected %v", tt.toolName, result, tt.shouldInclude)
			}
		})
	}
}

func TestMCPServerEntry_GetTimeout(t *testing.T) {
	tests := []struct {
		name           string
		serverTimeout  int
		defaultTimeout int
		expected       int
	}{
		{
			name:           "use server timeout when set",
			serverTimeout:  60,
			defaultTimeout: 30,
			expected:       60,
		},
		{
			name:           "use default timeout when server timeout is 0",
			serverTimeout:  0,
			defaultTimeout: 30,
			expected:       30,
		},
		{
			name:           "use server timeout even if smaller than default",
			serverTimeout:  10,
			defaultTimeout: 30,
			expected:       10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := config.MCPServerEntry{
				Timeout: tt.serverTimeout,
			}

			result := entry.GetTimeout(tt.defaultTimeout)
			if result != tt.expected {
				t.Errorf("GetTimeout() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

// StartServer drives containers only through the runtime port: a detached
// shared container is reused via its published port; otherwise the image is
// pulled, run detached and polled until its healthcheck passes.
func TestSupervisor_StartServer_UsesContainerRuntime(t *testing.T) {
	server := config.MCPServerEntry{
		Name: "fs", Enabled: true, Run: true, OCI: "example/fs:latest", Port: 3100,
		Env: map[string]string{"TOKEN": "x"},
	}
	cfg := &config.MCPConfig{Enabled: true, Servers: []config.MCPServerEntry{server}}

	t.Run("reuses a detached shared container", func(t *testing.T) {
		rt := &containermocks.FakeContainerRuntime{}
		rt.PublishedPortReturns(4200, nil)
		supervisor := NewSupervisor(convdomain.GenerateSessionID(), cfg, rt, nil)

		if err := supervisor.StartServer(context.Background(), server); err != nil {
			t.Fatalf("StartServer: %v", err)
		}
		if _, name := rt.PublishedPortArgsForCall(0); name != "inference-mcp-fs-shared" {
			t.Errorf("looked up %q, want the shared container", name)
		}
		if rt.RunContainerCallCount() != 0 {
			t.Error("started a container although a shared one is running")
		}
	})

	t.Run("runs a new container and waits until healthy", func(t *testing.T) {
		rt := &containermocks.FakeContainerRuntime{}
		rt.PublishedPortReturns(0, errors.New("no such container"))
		rt.RunContainerReturns("abc123", nil)
		rt.GetContainerHealthReturns(containerruntime.HealthStatusHealthy, nil)
		sessionID := convdomain.GenerateSessionID()
		supervisor := NewSupervisor(sessionID, cfg, rt, nil)

		if err := supervisor.StartServer(context.Background(), server); err != nil {
			t.Fatalf("StartServer: %v", err)
		}
		if rt.PullImageCallCount() != 1 {
			t.Errorf("PullImage called %d times, want 1", rt.PullImageCallCount())
		}
		_, opts := rt.RunContainerArgsForCall(0)
		name := "inference-mcp-fs-" + string(sessionID)
		if opts.Name != name || opts.Image != server.OCI || !opts.Detached || !opts.RemoveOnExit {
			t.Errorf("unexpected run options: %+v", opts)
		}
		if !slices.Equal(opts.Ports, []string{"3100:3000"}) || opts.Environment["TOKEN"] != "x" {
			t.Errorf("unexpected ports/env: %v %v", opts.Ports, opts.Environment)
		}
		if supervisor.containerIDs[name] != "abc123" {
			t.Errorf("container id not recorded: %v", supervisor.containerIDs)
		}
	})
}
