package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chzyer/readline"
	"github.com/inference-gateway/cli/config"
	"github.com/spf13/cobra"
)

var interactiveCmd = &cobra.Command{
	Use:   "interactive",
	Short: "Start interactive mode",
	Long: `Start an interactive session with the inference gateway CLI.
This provides a persistent shell-like interface with command history,
auto-completion, and enhanced user experience similar to Claude Code.`,
	Aliases: []string{"i", "shell"},
	Run: func(cmd *cobra.Command, args []string) {
		startInteractive()
	},
}

func startInteractive() {
	fmt.Println("🚀 Inference Gateway Interactive Mode")
	fmt.Println("Type '/help' for commands, '/exit' or Ctrl+D to quit")
	fmt.Println()

	cfg, err := config.LoadConfig("")
	if err != nil {
		fmt.Printf("Warning: Could not load config: %v\n", err)
		cfg = config.DefaultConfig()
	}

	completer := createCompleter(cfg)

	rlConfig := &readline.Config{
		Prompt:          "infer> ",
		HistoryFile:     getHistoryFile(),
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	}

	rl, err := readline.NewEx(rlConfig)
	if err != nil {
		fmt.Printf("Error creating readline: %v\n", err)
		return
	}
	defer func() {
		_ = rl.Close()
	}()

	for {
		line, err := rl.Readline()
		if err == readline.ErrInterrupt {
			if len(line) == 0 {
				break
			} else {
				continue
			}
		} else if err == io.EOF {
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if handleSpecialCommands(line, rl) {
			continue
		}

		executeInteractiveCommand(line)
	}

	fmt.Println("\n👋 Goodbye!")
}

func handleSpecialCommands(line string, rl *readline.Instance) bool {
	switch {
	case line == "/exit" || line == "/quit":
		return false
	case line == "/help":
		showInteractiveHelp()
		return true
	case line == "/clear":
		fmt.Print("\033[H\033[2J")
		return true
	case line == "/history":
		showHistory(rl)
		return true
	case line == "/models":
		executeInteractiveCommand("models list")
		return true
	case line == "/config":
		showConfig()
		return true
	case line == "/chat":
		if err := startChatSession(); err != nil {
			fmt.Printf("❌ Error starting chat: %v\n", err)
		}
		return true
	case strings.HasPrefix(line, "/"):
		fmt.Printf("Unknown command: %s. Type '/help' for available commands.\n", line)
		return true
	}
	return false
}

func showInteractiveHelp() {
	fmt.Println("🔧 Interactive Mode Commands:")
	fmt.Println()
	fmt.Println("Regular Commands:")
	fmt.Println("  status                - Check gateway status")
	fmt.Println("  models list           - List deployed models")
	fmt.Println("  prompt <flags> <text> - Send prompt to inference gateway")
	fmt.Println("  chat                  - Start interactive chat with model selection")
	fmt.Println("  version               - Show version information")
	fmt.Println()
	fmt.Println("Special Commands:")
	fmt.Println("  /help            - Show this help")
	fmt.Println("  /clear           - Clear screen")
	fmt.Println("  /history         - Show command history")
	fmt.Println("  /models          - Quick list models")
	fmt.Println("  /config          - Show current configuration")
	fmt.Println("  /chat            - Start chat session with model selection")
	fmt.Println("  /exit or /quit   - Exit interactive mode")
	fmt.Println()
	fmt.Println("Shortcuts:")
	fmt.Println("  ↑/↓              - Navigate command history")
	fmt.Println("  Tab              - Auto-complete commands")
	fmt.Println("  Ctrl+C           - Cancel current input")
	fmt.Println("  Ctrl+D           - Exit")
	fmt.Println()
}

func showHistory(rl *readline.Instance) {
	fmt.Println("📝 Command History:")
	fmt.Println("(History is stored in ~/.infer_history)")
	fmt.Println()
}

func showConfig() {
	cfg, err := config.LoadConfig("")
	if err != nil {
		fmt.Printf("❌ Error loading config: %v\n", err)
		return
	}

	fmt.Println("⚙️  Current Configuration:")
	fmt.Println()
	fmt.Printf("Gateway URL:    %s\n", cfg.Gateway.URL)
	fmt.Printf("API Key:        %s\n", maskAPIKey(cfg.Gateway.APIKey))
	fmt.Printf("Timeout:        %ds\n", cfg.Gateway.Timeout)
	fmt.Printf("Output Format:  %s\n", cfg.Output.Format)
	fmt.Printf("Quiet Mode:     %t\n", cfg.Output.Quiet)
	fmt.Println()
}

func maskAPIKey(apiKey string) string {
	if apiKey == "" {
		return "(not set)"
	}
	if len(apiKey) <= 8 {
		return strings.Repeat("*", len(apiKey))
	}
	return apiKey[:4] + strings.Repeat("*", len(apiKey)-8) + apiKey[len(apiKey)-4:]
}

func executeInteractiveCommand(line string) {
	args := strings.Fields(line)
	if len(args) == 0 {
		return
	}

	fmt.Printf("⚡ Executing: %s\n", line)

	switch args[0] {
	case "status":
		statusCmd.SetArgs(args[1:])
		if err := statusCmd.Execute(); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}
	case "models":
		if len(args) > 1 && args[1] == "list" {
			listModelsCmd.SetArgs(args[2:])
			if err := listModelsCmd.Execute(); err != nil {
				fmt.Printf("❌ Error: %v\n", err)
			}
		} else {
			modelsCmd.SetArgs(args[1:])
			if err := modelsCmd.Execute(); err != nil {
				fmt.Printf("❌ Error: %v\n", err)
			}
		}
	case "prompt":
		promptCmd.SetArgs(args[1:])
		if err := promptCmd.Execute(); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}
	case "version":
		versionCmd.SetArgs(args[1:])
		if err := versionCmd.Execute(); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}
	case "chat":
		chatCmd.SetArgs(args[1:])
		if err := chatCmd.Execute(); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}
	default:
		fmt.Printf("❌ Unknown command: %s\n", args[0])
		fmt.Println("💡 Tip: Type '/help' to see available commands")
	}
	fmt.Println()
}

func getHistoryFile() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".infer_history")
}

func createCompleter(cfg *config.Config) readline.AutoCompleter {
	items := []readline.PrefixCompleterInterface{
		readline.PcItem("status",
			readline.PcItem("--format"),
			readline.PcItem("-f"),
		),
		readline.PcItem("models",
			readline.PcItem("list"),
		),
		readline.PcItem("prompt"),
		readline.PcItem("chat"),
		readline.PcItem("version"),
		readline.PcItem("/help"),
		readline.PcItem("/exit"),
		readline.PcItem("/quit"),
		readline.PcItem("/clear"),
		readline.PcItem("/history"),
		readline.PcItem("/models"),
		readline.PcItem("/config"),
		readline.PcItem("/chat"),
	}

	if models := getAvailableModels(cfg); len(models) > 0 {
		modelItems := make([]readline.PrefixCompleterInterface, len(models))
		for i, model := range models {
			modelItems[i] = readline.PcItem(model)
		}

		items = append(items, readline.PcItem("prompt", modelItems...))
	}

	return readline.NewPrefixCompleter(items...)
}

func getAvailableModels(cfg *config.Config) []string {
	modelsResp, err := fetchModels(cfg)
	if err != nil {
		return []string{"gpt-4", "gpt-3.5-turbo", "claude-3", "claude-2"}
	}

	models := make([]string, len(modelsResp.Data))
	for i, model := range modelsResp.Data {
		models[i] = model.ID
	}
	return models
}

func init() {
	rootCmd.AddCommand(interactiveCmd)
}
