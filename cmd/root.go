package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
	cobra "github.com/spf13/cobra"
	viper "github.com/spf13/viper"
)

// Global Viper instance for commands to use
var V *viper.Viper

var rootCmd = &cobra.Command{
	Use:   "infer",
	Short: "The CLI for the Inference Gateway",
	Long: `A powerful command-line interface for managing and interacting with
the Inference Gateway. This CLI provides tools for configuration,
deployment, monitoring, and management of inference services.`,
	Run: func(cmd *cobra.Command, args []string) {
		if cmd.Flags().Changed("version") {
			versionCmd.Run(cmd, args)
			return
		}
		if len(args) == 0 && !cmd.Flags().Changed("help") {
			fmt.Println("Welcome to the Inference Gateway CLI!")
			fmt.Println("Use 'infer chat' to start interactive chat or --help to see available commands.")
		} else {
			fmt.Println("Welcome to the Inference Gateway CLI!")
			fmt.Println("Use --help to see available commands or 'infer chat' for interactive mode.")
		}
	},
}

func Execute() {
	defer logger.Close()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.Flags().BoolP("version", "", false, "print version information")

	cobra.OnInitialize(initConfig)
}

func initConfig() { // nolint:funlen
	V = viper.New()
	v := V

	defaults := config.DefaultConfig()
	v.SetDefault("container_runtime", defaults.ContainerRuntime)
	v.SetDefault("container_runtime.type", defaults.ContainerRuntime.Type)
	v.SetDefault("gateway", defaults.Gateway)
	v.SetDefault("gateway.url", defaults.Gateway.URL)
	v.SetDefault("gateway.api_key", defaults.Gateway.APIKey)
	v.SetDefault("gateway.timeout", defaults.Gateway.Timeout)
	v.SetDefault("gateway.oci", defaults.Gateway.OCI)
	v.SetDefault("gateway.run", defaults.Gateway.Run)
	v.SetDefault("gateway.standalone_binary", defaults.Gateway.StandaloneBinary)
	v.SetDefault("gateway.debug", defaults.Gateway.Debug)
	v.SetDefault("gateway.include_models", defaults.Gateway.IncludeModels)
	v.SetDefault("gateway.exclude_models", defaults.Gateway.ExcludeModels)
	v.SetDefault("gateway.vision_enabled", defaults.Gateway.VisionEnabled)
	v.SetDefault("logging", defaults.Logging)
	v.SetDefault("logging.debug", defaults.Logging.Debug)
	v.SetDefault("logging.dir", defaults.Logging.Dir)
	v.SetDefault("logging.console_output", defaults.Logging.ConsoleOutput)
	v.SetDefault("client", defaults.Client)
	v.SetDefault("tools", defaults.Tools)
	v.SetDefault("agent", defaults.Agent)
	v.SetDefault("export", defaults.Export)
	v.SetDefault("compact", defaults.Compact)
	v.SetDefault("compact.enabled", defaults.Compact.Enabled)
	v.SetDefault("compact.auto_at", defaults.Compact.AutoAt)
	v.SetDefault("compact.keep_first_messages", defaults.Compact.KeepFirstMessages)
	v.SetDefault("web", defaults.Web)
	v.SetDefault("web.enabled", defaults.Web.Enabled)
	v.SetDefault("web.port", defaults.Web.Port)
	v.SetDefault("web.host", defaults.Web.Host)
	v.SetDefault("web.session_inactivity_mins", defaults.Web.SessionInactivityMins)
	v.SetDefault("web.ssh", defaults.Web.SSH)
	v.SetDefault("web.ssh.enabled", defaults.Web.SSH.Enabled)
	v.SetDefault("web.ssh.use_ssh_config", defaults.Web.SSH.UseSSHConfig)
	v.SetDefault("web.ssh.known_hosts_path", defaults.Web.SSH.KnownHostsPath)
	v.SetDefault("web.ssh.auto_install", defaults.Web.SSH.AutoInstall)
	v.SetDefault("web.ssh.install_version", defaults.Web.SSH.InstallVersion)
	v.SetDefault("web.ssh.install_dir", defaults.Web.SSH.InstallDir)
	v.SetDefault("web.servers", defaults.Web.Servers)
	v.SetDefault("computer_use", defaults.ComputerUse)
	v.SetDefault("computer_use.enabled", defaults.ComputerUse.Enabled)
	v.SetDefault("computer_use.floating_window.enabled", defaults.ComputerUse.FloatingWindow.Enabled)
	v.SetDefault("computer_use.floating_window.respawn_on_close", defaults.ComputerUse.FloatingWindow.RespawnOnClose)
	v.SetDefault("computer_use.floating_window.position", defaults.ComputerUse.FloatingWindow.Position)
	v.SetDefault("computer_use.floating_window.always_on_top", defaults.ComputerUse.FloatingWindow.AlwaysOnTop)
	v.SetDefault("computer_use.screenshot.enabled", defaults.ComputerUse.Screenshot.Enabled)
	v.SetDefault("computer_use.screenshot.max_width", defaults.ComputerUse.Screenshot.MaxWidth)
	v.SetDefault("computer_use.screenshot.max_height", defaults.ComputerUse.Screenshot.MaxHeight)
	v.SetDefault("computer_use.screenshot.target_width", defaults.ComputerUse.Screenshot.TargetWidth)
	v.SetDefault("computer_use.screenshot.target_height", defaults.ComputerUse.Screenshot.TargetHeight)
	v.SetDefault("computer_use.screenshot.format", defaults.ComputerUse.Screenshot.Format)
	v.SetDefault("computer_use.screenshot.quality", defaults.ComputerUse.Screenshot.Quality)
	v.SetDefault("computer_use.screenshot.streaming_enabled", defaults.ComputerUse.Screenshot.StreamingEnabled)
	v.SetDefault("computer_use.screenshot.capture_interval", defaults.ComputerUse.Screenshot.CaptureInterval)
	v.SetDefault("computer_use.screenshot.buffer_size", defaults.ComputerUse.Screenshot.BufferSize)
	v.SetDefault("computer_use.screenshot.temp_dir", defaults.ComputerUse.Screenshot.TempDir)
	v.SetDefault("computer_use.screenshot.log_captures", defaults.ComputerUse.Screenshot.LogCaptures)
	v.SetDefault("computer_use.rate_limit.enabled", defaults.ComputerUse.RateLimit.Enabled)
	v.SetDefault("computer_use.rate_limit.max_actions_per_minute", defaults.ComputerUse.RateLimit.MaxActionsPerMinute)
	v.SetDefault("computer_use.rate_limit.window_seconds", defaults.ComputerUse.RateLimit.WindowSeconds)
	v.SetDefault("computer_use.tools.mouse_move.enabled", defaults.ComputerUse.Tools.MouseMove.Enabled)
	v.SetDefault("computer_use.tools.mouse_click.enabled", defaults.ComputerUse.Tools.MouseClick.Enabled)
	v.SetDefault("computer_use.tools.mouse_scroll.enabled", defaults.ComputerUse.Tools.MouseScroll.Enabled)
	v.SetDefault("computer_use.tools.keyboard_type.enabled", defaults.ComputerUse.Tools.KeyboardType.Enabled)
	v.SetDefault("computer_use.tools.keyboard_type.max_text_length", defaults.ComputerUse.Tools.KeyboardType.MaxTextLength)
	v.SetDefault("computer_use.tools.get_focused_app.enabled", defaults.ComputerUse.Tools.GetFocusedApp.Enabled)
	v.SetDefault("computer_use.tools.activate_app.enabled", defaults.ComputerUse.Tools.ActivateApp.Enabled)
	v.SetDefault("git", defaults.Git)
	v.SetDefault("storage", defaults.Storage)
	v.SetDefault("conversation", defaults.Conversation)
	v.SetDefault("chat", defaults.Chat)
	v.SetDefault("chat.theme", defaults.Chat.Theme)
	v.SetDefault("chat.keybindings.enabled", defaults.Chat.Keybindings.Enabled)
	v.SetDefault("chat.status_bar.enabled", defaults.Chat.StatusBar.Enabled)
	v.SetDefault("chat.status_bar.indicators.model", defaults.Chat.StatusBar.Indicators.Model)
	v.SetDefault("chat.status_bar.indicators.theme", defaults.Chat.StatusBar.Indicators.Theme)
	v.SetDefault("chat.status_bar.indicators.max_output", defaults.Chat.StatusBar.Indicators.MaxOutput)
	v.SetDefault("chat.status_bar.indicators.a2a_agents", defaults.Chat.StatusBar.Indicators.A2AAgents)
	v.SetDefault("chat.status_bar.indicators.tools", defaults.Chat.StatusBar.Indicators.Tools)
	v.SetDefault("chat.status_bar.indicators.background_shells", defaults.Chat.StatusBar.Indicators.BackgroundShells)
	v.SetDefault("chat.status_bar.indicators.a2a_tasks", defaults.Chat.StatusBar.Indicators.A2ATasks)
	v.SetDefault("chat.status_bar.indicators.mcp", defaults.Chat.StatusBar.Indicators.MCP)
	v.SetDefault("chat.status_bar.indicators.context_usage", defaults.Chat.StatusBar.Indicators.ContextUsage)
	v.SetDefault("chat.status_bar.indicators.session_tokens", defaults.Chat.StatusBar.Indicators.SessionTokens)
	v.SetDefault("chat.status_bar.indicators.cost", defaults.Chat.StatusBar.Indicators.Cost)
	v.SetDefault("chat.status_bar.indicators.git_branch", defaults.Chat.StatusBar.Indicators.GitBranch)
	v.SetDefault("a2a.enabled", defaults.A2A.Enabled)
	v.SetDefault("a2a.cache.enabled", defaults.A2A.Cache.Enabled)
	v.SetDefault("a2a.cache.ttl", defaults.A2A.Cache.TTL)
	v.SetDefault("a2a.task.status_poll_seconds", defaults.A2A.Task.StatusPollSeconds)
	v.SetDefault("a2a.task.polling_strategy", defaults.A2A.Task.PollingStrategy)
	v.SetDefault("a2a.task.initial_poll_interval_sec", defaults.A2A.Task.InitialPollIntervalSec)
	v.SetDefault("a2a.task.max_poll_interval_sec", defaults.A2A.Task.MaxPollIntervalSec)
	v.SetDefault("a2a.task.backoff_multiplier", defaults.A2A.Task.BackoffMultiplier)
	v.SetDefault("a2a.task.background_monitoring", defaults.A2A.Task.BackgroundMonitoring)
	v.SetDefault("a2a.task.completed_task_retention", defaults.A2A.Task.CompletedTaskRetention)
	v.SetDefault("a2a.tools.query_agent.enabled", defaults.A2A.Tools.QueryAgent.Enabled)
	v.SetDefault("a2a.tools.query_agent.require_approval", defaults.A2A.Tools.QueryAgent.RequireApproval)
	v.SetDefault("a2a.tools.query_task.enabled", defaults.A2A.Tools.QueryTask.Enabled)
	v.SetDefault("a2a.tools.query_task.require_approval", defaults.A2A.Tools.QueryTask.RequireApproval)
	v.SetDefault("a2a.tools.submit_task.enabled", defaults.A2A.Tools.SubmitTask.Enabled)
	v.SetDefault("a2a.tools.submit_task.require_approval", defaults.A2A.Tools.SubmitTask.RequireApproval)
	v.SetDefault("tools.web_fetch.enabled", defaults.Tools.WebFetch.Enabled)
	v.SetDefault("tools.web_fetch.whitelisted_domains", defaults.Tools.WebFetch.WhitelistedDomains)
	v.SetDefault("tools.web_fetch.safety.max_size", defaults.Tools.WebFetch.Safety.MaxSize)
	v.SetDefault("tools.web_fetch.safety.timeout", defaults.Tools.WebFetch.Safety.Timeout)
	v.SetDefault("tools.web_fetch.safety.allow_redirect", defaults.Tools.WebFetch.Safety.AllowRedirect)
	v.SetDefault("tools.web_fetch.require_approval", defaults.Tools.WebFetch.RequireApproval)
	v.SetDefault("pricing", defaults.Pricing)
	v.SetDefault("pricing.enabled", defaults.Pricing.Enabled)
	v.SetDefault("pricing.currency", defaults.Pricing.Currency)

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./.infer")
	v.AddConfigPath("$HOME/.infer")
	v.SetEnvPrefix("INFER")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if a2aAgents := os.Getenv("INFER_A2A_AGENTS"); a2aAgents != "" {
		var agents []string
		for _, agent := range strings.FieldsFunc(a2aAgents, func(c rune) bool {
			return c == ',' || c == '\n'
		}) {
			if trimmed := strings.TrimSpace(agent); trimmed != "" {
				agents = append(agents, trimmed)
			}
		}

		v.Set("a2a.agents", agents)
	}

	if whitelistCommands := os.Getenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS"); whitelistCommands != "" {
		var commands []string
		for _, cmd := range strings.FieldsFunc(whitelistCommands, func(c rune) bool {
			return c == ',' || c == '\n'
		}) {
			if trimmed := strings.TrimSpace(cmd); trimmed != "" {
				commands = append(commands, trimmed)
			}
		}

		v.Set("tools.bash.whitelist.commands", commands)
	}

	if whitelistPatterns := os.Getenv("INFER_TOOLS_BASH_WHITELIST_PATTERNS"); whitelistPatterns != "" {
		var patterns []string
		for _, pattern := range strings.FieldsFunc(whitelistPatterns, func(c rune) bool {
			return c == ',' || c == '\n'
		}) {
			if trimmed := strings.TrimSpace(pattern); trimmed != "" {
				patterns = append(patterns, trimmed)
			}
		}

		v.Set("tools.bash.whitelist.patterns", patterns)
	}

	if err := v.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose")); err != nil {
		fmt.Fprintf(os.Stderr, "Error binding verbose flag: %v\n", err)
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
			os.Exit(1)
		}
	}

	processKeybindingEnvVars(v)

	verbose := v.GetBool("verbose")
	debug := v.GetBool("logging.debug")
	logDir := v.GetString("logging.dir")

	if logDir == "" {
		configFile := v.ConfigFileUsed()
		if configFile != "" {
			configDir := filepath.Dir(configFile)
			logDir = filepath.Join(configDir, "logs")
		}
	}

	consoleOutput := v.GetString("logging.console_output")

	if consoleOutput != "" && consoleOutput != "stderr" {
		fmt.Fprintf(os.Stderr, "Warning: invalid logging.console_output value '%s', must be 'stderr' or empty\n", consoleOutput)
		consoleOutput = ""
	}

	logger.Init(verbose, debug, logDir, consoleOutput)
}

// processKeybindingEnvVars processes environment variables for keybinding configuration
// Supports: INFER_CHAT_KEYBINDINGS_BINDINGS_<ACTION_ID>_KEYS="key1,key2,key3"
// Supports: INFER_CHAT_KEYBINDINGS_BINDINGS_<ACTION_ID>_ENABLED="true/false"
func processKeybindingEnvVars(v *viper.Viper) {
	const prefix = "INFER_CHAT_KEYBINDINGS_BINDINGS_"

	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		if len(pair) != 2 {
			continue
		}

		envKey := pair[0]
		envValue := pair[1]

		if !strings.HasPrefix(envKey, prefix) {
			continue
		}

		suffix := strings.TrimPrefix(envKey, prefix)
		parts := strings.Split(suffix, "_")

		if len(parts) < 2 {
			continue
		}

		field := parts[len(parts)-1]
		actionIDParts := parts[:len(parts)-1]
		actionID := strings.ToLower(strings.Join(actionIDParts, "_"))

		switch field {
		case "KEYS":
			processKeybindingKeys(v, actionID, envValue)
		case "ENABLED":
			processKeybindingEnabled(v, actionID, envValue)
		}
	}
}

// processKeybindingKeys parses comma-separated keys and sets them in viper
func processKeybindingKeys(v *viper.Viper, actionID, envValue string) {
	var keys []string
	for _, key := range strings.FieldsFunc(envValue, func(c rune) bool {
		return c == ',' || c == '\n'
	}) {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}

	if len(keys) > 0 {
		configPath := fmt.Sprintf("chat.keybindings.bindings.%s.keys", actionID)
		v.Set(configPath, keys)
	}
}

// processKeybindingEnabled parses boolean enabled value and sets it in viper
func processKeybindingEnabled(v *viper.Viper, actionID, envValue string) {
	enabled := strings.ToLower(strings.TrimSpace(envValue))
	if enabled == "true" || enabled == "false" {
		configPath := fmt.Sprintf("chat.keybindings.bindings.%s.enabled", actionID)
		v.Set(configPath, enabled == "true")
	}
}
