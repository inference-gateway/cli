package shortcuts

import (
	"context"
	"fmt"
	"strings"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// Outcome is what a surface without a TUI - headless, a channel bot - should do
// with a slash command: print Text, or run Prompt through the agent as if the
// user had typed it, optionally on Model. The chat TUI splits the same two cases
// across status messages and SetInputEvent; everything else it does with a
// ShortcutResult is view state, which has no meaning here.
type Outcome struct {
	Text   string
	Prompt string
	Model  string
}

// Deps supply the work the chat TUI does inside its side-effect handlers, for
// surfaces that have no handlers. A nil func means that command is unavailable
// and reports itself as such.
type Deps struct {
	// SessionID is the conversation the command runs against; /stats and
	// /traces read it from the context, exactly as the TUI passes it.
	SessionID string
	// Compact summarises the conversation into a fresh session and returns what
	// happened - the TUI's /compact. Headless wires the rollover manager.
	Compact func(ctx context.Context) (string, error)
}

// Run executes input as a slash command from reg. handled is false when input
// is not a registered shortcut - a skill invocation, a bare file path, plain
// prose - and the caller must then use input unchanged.
func Run(ctx context.Context, reg *Registry, input string, deps Deps) (Outcome, bool, error) {
	name, args, ok := resolve(reg, input)
	if !ok {
		return Outcome{}, false, nil
	}

	ctx = context.WithValue(ctx, agentdomain.SessionIDKey, deps.SessionID)
	res, err := reg.Execute(ctx, name, args)
	if err != nil {
		return Outcome{}, true, fmt.Errorf("shortcut /%s failed: %w", name, err)
	}
	if !res.Success {
		return Outcome{}, true, fmt.Errorf("shortcut /%s failed: %s", name, strings.TrimSpace(res.Output))
	}

	switch res.SideEffect {
	case SideEffectSetInput, SideEffectSendMessage:
		if prompt, _ := res.Data.(string); strings.TrimSpace(prompt) != "" {
			return Outcome{Prompt: prompt}, true, nil
		}
	case SideEffectSendMessageWithModel:
		if data, ok := res.Data.(ModelSwitchData); ok && strings.TrimSpace(data.Prompt) != "" {
			return Outcome{Prompt: data.Prompt, Model: data.TargetModel}, true, nil
		}
	case SideEffectGenerateSnippet:
		return snippetPrompt(res.Data)
	case SideEffectCompactConversation:
		if deps.Compact == nil {
			return Outcome{Text: "/compact needs a persistent conversation; nothing to compact here."}, true, nil
		}
		text, cErr := deps.Compact(ctx)
		if cErr != nil {
			return Outcome{}, true, fmt.Errorf("shortcut /compact failed: %w", cErr)
		}
		return Outcome{Text: text}, true, nil
	case SideEffectShowHelp:
		return Outcome{Text: HelpText(reg)}, true, nil
	}

	if text := strings.TrimSpace(res.Output); text != "" {
		return Outcome{Text: text}, true, nil
	}

	sc, _ := reg.Get(name)
	return Outcome{Text: fmt.Sprintf("/%s opens a panel in the chat TUI (infer chat) and has nothing to render here: %s", name, sc.GetDescription())}, true, nil
}

// HelpText renders the registry as one "/name - description" line per shortcut.
func HelpText(reg *Registry) string {
	var sb strings.Builder
	sb.WriteString("Available commands:\n")
	for _, sc := range reg.GetAll() {
		fmt.Fprintf(&sb, "/%s - %s\n", sc.GetName(), sc.GetDescription())
	}
	return strings.TrimRight(sb.String(), "\n")
}

// resolve reports the registered shortcut input names, if any.
func resolve(reg *Registry, input string) (string, []string, bool) {
	if reg == nil || !strings.HasPrefix(strings.TrimSpace(input), "/") {
		return "", nil, false
	}

	name, args, err := reg.ParseShortcut(strings.TrimSpace(input))
	if err != nil {
		return "", nil, false
	}
	name, _, _ = strings.Cut(strings.ToLower(name), "@") // channel group form: /help@MyBot

	// ponytail: /voice would sit recording from a microphone nobody is at.
	if _, exists := reg.Get(name); !exists || name == "voice" {
		return "", nil, false
	}
	return name, args, true
}

// snippetPrompt finishes a custom shortcut's AI-generated snippet, which the TUI
// generates asynchronously and drops into the input box.
func snippetPrompt(data any) (Outcome, bool, error) {
	dataMap, ok := data.(map[string]any)
	if !ok {
		return Outcome{}, true, fmt.Errorf("shortcut failed: malformed snippet data")
	}

	ctx, ok1 := dataMap["context"].(context.Context)
	values, ok2 := dataMap["dataMap"].(map[string]string)
	shortcut, ok3 := dataMap["customShortcut"].(*CustomShortcut)
	snippet, ok4 := dataMap["snippet"].(*SnippetConfig)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return Outcome{}, true, fmt.Errorf("shortcut failed: incomplete snippet data")
	}

	prompt, err := shortcut.GenerateSnippet(ctx, values, snippet)
	if err != nil {
		return Outcome{}, true, fmt.Errorf("shortcut /%s failed to generate its snippet: %w", shortcut.GetName(), err)
	}
	return Outcome{Prompt: prompt}, true, nil
}
