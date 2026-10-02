package container

import (
	"errors"
	"slices"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	conversationmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	config "github.com/inference-gateway/cli/config"
)

func newReloadContainer(load func() (*config.Config, error)) *ServiceContainer {
	c := &ServiceContainer{
		config:        config.DefaultConfig(),
		startupConfig: config.DefaultConfig(),
		loadConfig:    load,
		modelService:  &conversationmocks.FakeModelService{},
		agent:         &agentdomainmocks.FakeAgentService{},
	}
	c.initializeStateManager()
	return c
}

func loadWith(change func(*config.Config)) func() (*config.Config, error) {
	return func() (*config.Config, error) {
		cfg := config.DefaultConfig()
		change(cfg)
		return cfg, nil
	}
}

// The approval policy holds the live config, so a restart-only key must be
// reported and never copied in, while hot keys apply to the running session.
func TestReloadConfigKeepsApprovalBehaviourUntilRestart(t *testing.T) {
	c := newReloadContainer(loadWith(func(cfg *config.Config) {
		cfg.Gateway.Timeout = 300
		cfg.Tools.Safety.ApprovalBehaviour = config.ApprovalBehaviourBlock
	}))
	before := c.config.Tools.Safety.ApprovalBehaviour

	applied, restart, err := c.ReloadConfig()
	if err != nil {
		t.Fatalf("ReloadConfig() error = %v", err)
	}
	if !slices.Equal(applied, []string{"gateway.timeout"}) {
		t.Errorf("applied = %v, want [gateway.timeout]", applied)
	}
	if !slices.Equal(restart, []string{"tools.safety.approval_behaviour"}) {
		t.Errorf("restart = %v, want [tools.safety.approval_behaviour]", restart)
	}
	if c.config.Gateway.Timeout != 300 {
		t.Errorf("gateway.timeout = %d, want 300", c.config.Gateway.Timeout)
	}
	if c.config.Tools.Safety.ApprovalBehaviour != before {
		t.Errorf("approval_behaviour changed mid-session to %q", c.config.Tools.Safety.ApprovalBehaviour)
	}

	_, restart, _ = c.ReloadConfig()
	if !slices.Equal(restart, []string{"tools.safety.approval_behaviour"}) {
		t.Errorf("second reload restart = %v, want the pending key again", restart)
	}
}

// The TUI reads chat.status_bar on every repaint, so hiding indicators must
// apply live and be reported once for the section, not once per leaf.
func TestReloadConfigAppliesStatusBarSection(t *testing.T) {
	c := newReloadContainer(loadWith(func(cfg *config.Config) {
		cfg.Chat.StatusBar.Indicators.Model = false
		cfg.Chat.StatusBar.Indicators.Cost = false
		cfg.Chat.StatusBar.Indicators.Tools = false
	}))

	applied, restart, err := c.ReloadConfig()
	if err != nil {
		t.Fatalf("ReloadConfig() error = %v", err)
	}
	if !slices.Equal(applied, []string{"chat.status_bar"}) {
		t.Errorf("applied = %v, want [chat.status_bar]", applied)
	}
	if len(restart) != 0 {
		t.Errorf("restart = %v, want none", restart)
	}
	indicators := c.config.Chat.StatusBar.Indicators
	if indicators.Model || indicators.Cost || indicators.Tools {
		t.Errorf("indicators not applied live: %+v", indicators)
	}
}

func TestReloadConfigSwitchesModelAndEffort(t *testing.T) {
	c := newReloadContainer(loadWith(func(cfg *config.Config) {
		cfg.Agent.Model = "openai/gpt-5"
		cfg.Agent.ReasoningEffort = "high"
	}))

	applied, _, err := c.ReloadConfig()
	if err != nil {
		t.Fatalf("ReloadConfig() error = %v", err)
	}
	if !slices.Equal(applied, []string{"agent.model", "agent.reasoning_effort"}) {
		t.Errorf("applied = %v", applied)
	}
	models := c.modelService.(*conversationmocks.FakeModelService)
	if models.SelectModelCallCount() != 1 || models.SelectModelArgsForCall(0) != "openai/gpt-5" {
		t.Errorf("SelectModel not called with the new model")
	}
	agent := c.agent.(*agentdomainmocks.FakeAgentService)
	if agent.SetReasoningEffortCallCount() != 1 || agent.SetReasoningEffortArgsForCall(0) != "high" {
		t.Errorf("SetReasoningEffort not called with the new effort")
	}
}

func TestReloadConfigRejectsAndKeepsConfig(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*ServiceContainer)
	}{
		{"invalid config", func(c *ServiceContainer) {
			c.loadConfig = func() (*config.Config, error) { return nil, errors.New("invalid reasoning effort") }
		}},
		{"agent busy", func(c *ServiceContainer) {
			_ = c.stateManager.StartChatSession("req", "model", nil)
		}},
		{"reload not enabled", func(c *ServiceContainer) { c.loadConfig = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newReloadContainer(loadWith(func(cfg *config.Config) { cfg.Gateway.Timeout = 300 }))
			tt.setup(c)
			want := c.config.Gateway.Timeout

			if _, _, err := c.ReloadConfig(); err == nil {
				t.Fatal("ReloadConfig() error = nil, want an error")
			}
			if c.config.Gateway.Timeout != want {
				t.Errorf("gateway.timeout = %d, want %d kept", c.config.Gateway.Timeout, want)
			}
		})
	}
}
