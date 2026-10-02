package shortcuts

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ReloadShortcut re-reads the configuration and applies what can change in a
// running chat. Its summary names the applied keys and the keys that only take
// effect after a restart.
type ReloadShortcut struct {
	reload func() (applied, restart []string, err error)
}

func NewReloadShortcut(reload func() (applied, restart []string, err error)) *ReloadShortcut {
	return &ReloadShortcut{reload: reload}
}

func (c *ReloadShortcut) GetName() string               { return "reload" }
func (c *ReloadShortcut) GetDescription() string        { return "Reload configuration from disk and env" }
func (c *ReloadShortcut) GetUsage() string              { return "/reload" }
func (c *ReloadShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (c *ReloadShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if c.reload == nil {
		return ShortcutResult{}, errors.New("config reload is only available in infer chat")
	}
	applied, restart, err := c.reload()
	if err != nil {
		return ShortcutResult{}, fmt.Errorf("reloading config: %w", err)
	}
	return ShortcutResult{
		Success:    true,
		SideEffect: SideEffectReloadConfig,
		Data:       reloadSummary(applied, restart),
	}, nil
}

func reloadSummary(applied, restart []string) string {
	switch {
	case len(applied) == 0 && len(restart) == 0:
		return "No config changes"
	case len(restart) == 0:
		return "Reloaded " + strings.Join(applied, ", ")
	case len(applied) == 0:
		return "Restart to apply " + strings.Join(restart, ", ")
	default:
		return "Reloaded " + strings.Join(applied, ", ") + " · restart to apply " + strings.Join(restart, ", ")
	}
}
