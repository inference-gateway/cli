package shortcuts

import (
	"context"
	"fmt"
	"sort"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	models "github.com/inference-gateway/cli/internal/models"
)

// ClearShortcut clears the conversation history
type ClearShortcut struct {
	repo        domain.ConversationRepository
	taskTracker domain.TaskTracker
}

func NewClearShortcut(repo domain.ConversationRepository, taskTracker domain.TaskTracker) *ClearShortcut {
	return &ClearShortcut{
		repo:        repo,
		taskTracker: taskTracker,
	}
}

func (c *ClearShortcut) GetName() string               { return "clear" }
func (c *ClearShortcut) GetDescription() string        { return "Clear conversation history" }
func (c *ClearShortcut) GetUsage() string              { return "/clear" }
func (c *ClearShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (c *ClearShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if err := c.repo.Clear(); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to clear conversation: %v", err),
			Success: false,
		}, nil
	}

	if c.taskTracker != nil {
		c.taskTracker.ClearAllAgents()
	}

	return ShortcutResult{
		Output:     "🧹 Conversation history cleared!",
		Success:    true,
		SideEffect: SideEffectClearConversation,
	}, nil
}

// CompactShortcut runs conversation optimization to reduce token usage
type CompactShortcut struct {
	repo domain.ConversationRepository
}

func NewCompactShortcut(repo domain.ConversationRepository) *CompactShortcut {
	return &CompactShortcut{
		repo: repo,
	}
}

func (c *CompactShortcut) GetName() string { return "compact" }
func (c *CompactShortcut) GetDescription() string {
	return "Optimize conversation to reduce token usage"
}
func (c *CompactShortcut) GetUsage() string              { return "/compact" }
func (c *CompactShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (c *CompactShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if c.repo.GetMessageCount() == 0 {
		return ShortcutResult{
			Output:  "No conversation to compact - conversation history is empty",
			Success: true,
		}, nil
	}

	return ShortcutResult{
		Output:     "Compacting conversation history...",
		Success:    true,
		SideEffect: SideEffectCompactConversation,
		Data:       ctx,
	}, nil
}

// ContextShortcut shows context window usage information
type ContextShortcut struct {
	repo         domain.ConversationRepository
	modelService domain.ModelService
}

func NewContextShortcut(repo domain.ConversationRepository, modelService domain.ModelService) *ContextShortcut {
	return &ContextShortcut{
		repo:         repo,
		modelService: modelService,
	}
}

func (c *ContextShortcut) GetName() string               { return "context" }
func (c *ContextShortcut) GetDescription() string        { return "Show context window usage" }
func (c *ContextShortcut) GetUsage() string              { return "/context" }
func (c *ContextShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (c *ContextShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	stats := c.repo.GetSessionTokens()
	messageCount := c.repo.GetMessageCount()
	currentModel := c.modelService.GetCurrentModel()

	contextWindowSize := c.estimateContextWindow(currentModel)

	var output strings.Builder
	output.WriteString("## Context Window Usage\n\n")

	if currentModel != "" {
		output.WriteString(fmt.Sprintf("**Model:** %s\n", currentModel))
	}
	output.WriteString(fmt.Sprintf("**Messages:** %d\n", messageCount))
	output.WriteString(fmt.Sprintf("**Current Context Size:** %d tokens\n", stats.LastInputTokens))
	output.WriteString(fmt.Sprintf("**API Requests:** %d\n", stats.RequestCount))
	output.WriteString(fmt.Sprintf("**Session Totals:** %d input, %d output\n", stats.TotalInputTokens, stats.TotalOutputTokens))

	if contextWindowSize > 0 && stats.LastInputTokens > 0 {
		output.WriteString(c.formatContextUsage(stats.LastInputTokens, contextWindowSize))
	}

	return ShortcutResult{
		Output:  output.String(),
		Success: true,
	}, nil
}

// estimateContextWindow returns an estimated context window size based on model name
func (c *ContextShortcut) estimateContextWindow(model string) int {
	return models.EstimateContextWindow(model)
}

// formatContextUsage formats the context window usage information
func (c *ContextShortcut) formatContextUsage(lastInputTokens, contextWindowSize int) string {
	var output strings.Builder

	usagePercent := float64(lastInputTokens) * 100 / float64(contextWindowSize)
	remaining := contextWindowSize - lastInputTokens
	if remaining < 0 {
		remaining = 0
	}

	displayPercent := usagePercent
	if displayPercent > 100 {
		displayPercent = 100
	}

	output.WriteString(fmt.Sprintf("\n**Context Window:** %d tokens\n", contextWindowSize))
	output.WriteString(fmt.Sprintf("**Usage:** %.1f%%\n", displayPercent))
	output.WriteString(fmt.Sprintf("**Remaining:** ~%d tokens\n", remaining))

	barWidth := 20
	filledWidth := int(displayPercent * float64(barWidth) / 100)
	if filledWidth > barWidth {
		filledWidth = barWidth
	}
	bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", barWidth-filledWidth)
	output.WriteString(fmt.Sprintf("\n`[%s]` %.1f%%\n", bar, displayPercent))

	if usagePercent > 80 {
		output.WriteString("\n**Warning:** Context window is getting full. Consider using `/compact` to optimize.")
	}

	return output.String()
}

// CostShortcut displays cost information for the current session
type CostShortcut struct {
	repo domain.ConversationRepository
}

func NewCostShortcut(repo domain.ConversationRepository) *CostShortcut {
	return &CostShortcut{repo: repo}
}

func (c *CostShortcut) GetName() string               { return "cost" }
func (c *CostShortcut) GetDescription() string        { return "Show session cost breakdown" }
func (c *CostShortcut) GetUsage() string              { return "/cost" }
func (c *CostShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (c *CostShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	costStats := c.repo.GetSessionCostStats()
	tokenStats := c.repo.GetSessionTokens()

	var output strings.Builder
	output.WriteString("## Session Cost Summary\n\n")

	output.WriteString("| Metric | Value |\n")
	output.WriteString("|--------|-------|\n")
	output.WriteString(fmt.Sprintf("| **Input Cost** | $%.4f (%s tokens) |\n",
		costStats.TotalInputCost,
		formatTokenCount(tokenStats.TotalInputTokens)))
	output.WriteString(fmt.Sprintf("| **Output Cost** | $%.4f (%s tokens) |\n",
		costStats.TotalOutputCost,
		formatTokenCount(tokenStats.TotalOutputTokens)))
	output.WriteString(fmt.Sprintf("| **API Requests** | %d |\n", tokenStats.RequestCount))
	output.WriteString(fmt.Sprintf("| **Total Cost** | $%.4f %s |\n\n",
		costStats.TotalCost, costStats.Currency))

	if len(costStats.PerModelStats) > 1 {
		output.WriteString("### Cost by Model\n\n")

		type modelCost struct {
			model string
			cost  float64
		}
		var models []modelCost
		for model, stats := range costStats.PerModelStats {
			models = append(models, modelCost{model, stats.TotalCost})
		}
		sort.Slice(models, func(i, j int) bool {
			return models[i].cost > models[j].cost
		})

		output.WriteString("| Model | Cost | Input | Output | Requests |\n")
		output.WriteString("|-------|------|-------|--------|----------|\n")
		for _, mc := range models {
			stats := costStats.PerModelStats[mc.model]
			output.WriteString(fmt.Sprintf("| %s | $%.4f (%.1f%%) | %s tokens ($%.4f) | %s tokens ($%.4f) | %d |\n",
				stats.Model,
				stats.TotalCost,
				(stats.TotalCost/costStats.TotalCost)*100,
				formatTokenCount(stats.InputTokens),
				stats.InputCost,
				formatTokenCount(stats.OutputTokens),
				stats.OutputCost,
				stats.RequestCount))
		}
		output.WriteString("\n")
	} else if len(costStats.PerModelStats) == 1 {
		for model := range costStats.PerModelStats {
			output.WriteString(fmt.Sprintf("**Model:** %s\n", model))
		}
	}

	hasFreeCost := costStats.TotalCost == 0.0 && tokenStats.TotalTokens > 0
	if hasFreeCost {
		output.WriteString("*Note: This session used models with no pricing data " +
			"(e.g., Ollama, free tier, or custom models). Cost shown may be incomplete.*\n")
	}

	return ShortcutResult{
		Output:  output.String(),
		Success: true,
	}, nil
}

// formatTokenCount formats token counts in a human-readable way
func formatTokenCount(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.2fM", float64(tokens)/1_000_000.0)
	} else if tokens >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1_000.0)
	}
	return fmt.Sprintf("%d", tokens)
}

// NewShortcut starts a new conversation
type NewShortcut struct {
	repo        PersistentConversationRepository
	taskTracker domain.TaskTracker
}

func NewNewShortcut(repo PersistentConversationRepository, taskTracker domain.TaskTracker) *NewShortcut {
	return &NewShortcut{
		repo:        repo,
		taskTracker: taskTracker,
	}
}

func (c *NewShortcut) GetName() string               { return "new" }
func (c *NewShortcut) GetDescription() string        { return "Start a new conversation" }
func (c *NewShortcut) GetUsage() string              { return "/new [title]" }
func (c *NewShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *NewShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	title := "New Conversation"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		title = strings.TrimSpace(args[0])
	}

	if c.taskTracker != nil {
		c.taskTracker.ClearAllAgents()
	}

	return ShortcutResult{
		Output:     fmt.Sprintf("🆕 Starting new conversation: %s", title),
		Success:    true,
		SideEffect: SideEffectStartNewConversation,
		Data:       title,
	}, nil
}

// HelpShortcut shows available shortcuts
type HelpShortcut struct {
	registry *Registry
}

func NewHelpShortcut(registry *Registry) *HelpShortcut {
	return &HelpShortcut{registry: registry}
}

func (c *HelpShortcut) GetName() string               { return "help" }
func (c *HelpShortcut) GetDescription() string        { return "Show available shortcuts" }
func (c *HelpShortcut) GetUsage() string              { return "/help [shortcut]" }
func (c *HelpShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *HelpShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 1 {
		shortcutName := args[0]
		shortcut, exists := c.registry.Get(shortcutName)
		if !exists {
			return ShortcutResult{
				Output:  fmt.Sprintf("Unknown shortcut: %s", shortcutName),
				Success: false,
			}, nil
		}

		output := fmt.Sprintf("Shortcut: %s\nDescription: %s\nUsage: %s",
			shortcut.GetName(), shortcut.GetDescription(), shortcut.GetUsage())

		return ShortcutResult{
			Output:  output,
			Success: true,
		}, nil
	}

	var output strings.Builder
	output.WriteString("## Available Shortcuts\n\n")

	shortcuts := c.registry.GetAll()
	for _, shortcut := range shortcuts {
		output.WriteString(fmt.Sprintf("/%s\n", shortcut.GetName()))
		output.WriteString(fmt.Sprintf("  %s\n\n", shortcut.GetDescription()))
	}

	output.WriteString("Type `/help <shortcut>` for detailed usage information.")

	return ShortcutResult{
		Output:     output.String(),
		Success:    true,
		SideEffect: SideEffectShowHelp,
	}, nil
}

// ExitShortcut exits the application
type ExitShortcut struct{}

func NewExitShortcut() *ExitShortcut {
	return &ExitShortcut{}
}

func (c *ExitShortcut) GetName() string               { return "exit" }
func (c *ExitShortcut) GetDescription() string        { return "Exit the chat session" }
func (c *ExitShortcut) GetUsage() string              { return "/exit" }
func (c *ExitShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (c *ExitShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	return ShortcutResult{
		Output:     "👋 Chat session ended!",
		Success:    true,
		SideEffect: SideEffectExit,
	}, nil
}

// SwitchShortcut switches the active model
type SwitchShortcut struct {
	modelService domain.ModelService
}

func NewSwitchShortcut(modelService domain.ModelService) *SwitchShortcut {
	return &SwitchShortcut{modelService: modelService}
}

func (c *SwitchShortcut) GetName() string               { return "switch" }
func (c *SwitchShortcut) GetDescription() string        { return "Switch to a different model" }
func (c *SwitchShortcut) GetUsage() string              { return "/switch [model]" }
func (c *SwitchShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *SwitchShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 {
		return ShortcutResult{
			Output:     "Select a model from the dropdown",
			Success:    true,
			SideEffect: SideEffectSwitchModel,
		}, nil
	}

	modelID := args[0]
	if err := c.modelService.SelectModel(modelID); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to switch model: %v", err),
			Success: false,
		}, nil
	}

	return ShortcutResult{
		Output:     fmt.Sprintf("Switched to model: %s", modelID),
		Success:    true,
		SideEffect: SideEffectSwitchModel,
		Data:       modelID,
	}, nil
}

// ThemeShortcut switches the active theme
type ThemeShortcut struct {
	themeService domain.ThemeService
}

func NewThemeShortcut(themeService domain.ThemeService) *ThemeShortcut {
	return &ThemeShortcut{themeService: themeService}
}

func (c *ThemeShortcut) GetName() string               { return "theme" }
func (c *ThemeShortcut) GetDescription() string        { return "Switch to a different theme" }
func (c *ThemeShortcut) GetUsage() string              { return "/theme [theme-name]" }
func (c *ThemeShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *ThemeShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 {
		return ShortcutResult{
			Output:     "",
			Success:    true,
			SideEffect: SideEffectSwitchTheme,
		}, nil
	}

	themeName := args[0]
	if err := c.themeService.SetTheme(themeName); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("❌ Failed to switch theme: %v", err),
			Success: false,
		}, nil
	}

	return ShortcutResult{
		Output:     fmt.Sprintf("🎨 Switched to theme: **%s**", themeName),
		Success:    true,
		SideEffect: SideEffectSwitchTheme,
		Data:       themeName,
	}, nil
}
