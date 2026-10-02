package container

import (
	"errors"
	"fmt"
	"strings"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// EnableConfigReload turns on /reload for this chat. It loads the startup
// baseline once, so runtime tweaks such as the mock gateway URL never read as
// changed keys.
func (c *ServiceContainer) EnableConfigReload(load func() (*config.Config, error)) {
	startup, err := load()
	if err != nil {
		logger.Warn("config reload disabled, loading the baseline failed", "error", err)
		return
	}
	c.loadConfig = load
	c.startupConfig = startup
}

// ReloadConfig loads the configuration again and copies the hot keys into the
// running config, so services keep their pointers. Other changed keys are only
// reported for a restart, so tools, sandbox and approval policy stay fixed.
// ponytail: no lock. A turn started in the same instant or a repaint can race
// the write, an RWMutex around config is the upgrade if that ever matters.
func (c *ServiceContainer) ReloadConfig() (applied, restart []string, err error) {
	c.reloadMu.Lock()
	defer c.reloadMu.Unlock()

	if c.loadConfig == nil {
		return nil, nil, errors.New("config reload is only available in infer chat")
	}
	if c.stateManager != nil && c.stateManager.IsAgentBusy() {
		return nil, nil, errors.New("agent is busy, run /reload after this turn")
	}
	fresh, err := c.loadConfig()
	if err != nil {
		return nil, nil, err
	}

	hot := c.hotConfigKeys(fresh)
	attempted := map[string]bool{}
	var errs []error
	for _, key := range config.ChangedKeys(c.config, fresh) {
		hotKey, ok := hotConfigKey(hot, key)
		if !ok || attempted[hotKey] {
			continue
		}
		attempted[hotKey] = true
		if err := hot[hotKey](); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", hotKey, err))
			continue
		}
		applied = append(applied, hotKey)
	}
	for _, key := range config.ChangedKeys(c.startupConfig, fresh) {
		if _, ok := hotConfigKey(hot, key); !ok {
			restart = append(restart, key)
		}
	}
	return applied, restart, errors.Join(errs...)
}

// hotConfigKey returns the hot entry that covers key: the key itself or a
// section it lives in, such as chat.status_bar.
func hotConfigKey(hot map[string]func() error, key string) (string, bool) {
	for {
		if _, ok := hot[key]; ok {
			return key, true
		}
		i := strings.LastIndex(key, ".")
		if i < 0 {
			return "", false
		}
		key = key[:i]
	}
}

// hotConfigKeys maps each key or section a running chat can pick up onto the
// setter that applies it. The agent reads these per turn and the TUI reads
// chat.status_bar per repaint. Model and effort go through their services so
// the status bar follows.
func (c *ServiceContainer) hotConfigKeys(fresh *config.Config) map[string]func() error {
	return map[string]func() error{
		"chat.status_bar": func() error {
			c.config.Chat.StatusBar = fresh.Chat.StatusBar
			return nil
		},
		"gateway.timeout": func() error {
			c.config.Gateway.Timeout = fresh.Gateway.Timeout
			return nil
		},
		"agent.max_turns": func() error {
			c.config.Agent.MaxTurns = fresh.Agent.MaxTurns
			return nil
		},
		"agent.max_tokens": func() error {
			c.config.Agent.MaxTokens = fresh.Agent.MaxTokens
			return nil
		},
		"agent.model": func() error {
			if err := c.modelService.SelectModel(fresh.Agent.Model); err != nil {
				return err
			}
			c.config.Agent.Model = fresh.Agent.Model
			return nil
		},
		"agent.reasoning_effort": func() error {
			if err := c.agent.SetReasoningEffort(fresh.Agent.ReasoningEffort); err != nil {
				return err
			}
			c.config.Agent.ReasoningEffort = fresh.Agent.ReasoningEffort
			return nil
		},
	}
}
