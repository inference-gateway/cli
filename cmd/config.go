package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	cobra "github.com/spf13/cobra"
	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
	configutils "github.com/inference-gateway/cli/config/utils"
	logger "github.com/inference-gateway/cli/internal/logger"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
	Long:  `Manage the Inference Gateway CLI configuration settings.`,
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new configuration file",
	Long: `Initialize a new .infer/config.yaml configuration file in the current directory.
This creates only the configuration file with default settings.

For complete project initialization, use 'infer init' instead.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		userspace := GetUserspaceFlag(cmd)
		overwrite, _ := cmd.Flags().GetBool("overwrite")

		var configPath string
		if userspace {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get user home directory: %w", err)
			}
			configPath = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
		} else {
			configPath = config.DefaultConfigPath
		}

		if _, err := os.Stat(configPath); err == nil {
			if !overwrite {
				return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
			}
		}

		if err := configutils.SaveYAML(configPath, "config", config.DefaultConfig()); err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}

		var scopeDesc string
		if userspace {
			scopeDesc = "userspace "
		}

		fmt.Printf("Successfully created %sconfiguration: %s\n", scopeDesc, configPath)
		if userspace {
			fmt.Println("This userspace configuration will be used as a fallback for all projects.")
			fmt.Println("Project-level configurations will take precedence when present.")
		} else {
			fmt.Println("You can now customize the configuration for this project.")
		}
		fmt.Println("Tip: Use 'infer init' for complete project initialization including additional setup files.")

		return nil
	},
}

// resolveViperEnvironmentVariables recursively resolves environment variables for all string fields using Viper
func resolveViperEnvironmentVariables(cfg any, keyPrefix string) {
	rv := reflect.ValueOf(cfg)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return
	}
	rv = rv.Elem()

	if rv.Kind() != reflect.Struct {
		return
	}

	rt := rv.Type()

	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		fieldType := rt.Field(i)

		if !field.CanSet() {
			continue
		}

		tag := fieldType.Tag.Get("mapstructure")
		if tag == "" {
			tag = strings.ToLower(fieldType.Name)
		}

		var key string
		if keyPrefix == "" {
			key = tag
		} else {
			key = keyPrefix + "." + tag
		}

		switch field.Kind() {
		case reflect.String:
			if V.IsSet(key) {
				field.SetString(V.GetString(key))
			}
		case reflect.Bool:
			if V.IsSet(key) {
				field.SetBool(V.GetBool(key))
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if V.IsSet(key) {
				field.SetInt(V.GetInt64(key))
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if V.IsSet(key) {
				field.SetUint(V.GetUint64(key))
			}
		case reflect.Float32, reflect.Float64:
			if V.IsSet(key) {
				field.SetFloat(V.GetFloat64(key))
			}
		case reflect.Slice:
			if V.IsSet(key) && field.Type().Elem().Kind() == reflect.String {
				field.Set(reflect.ValueOf(V.GetStringSlice(key)))
			}
		case reflect.Pointer:
			if !field.IsNil() && field.Elem().Kind() == reflect.Struct {
				resolveViperEnvironmentVariables(field.Interface(), key)
			}
		case reflect.Struct:
			resolveViperEnvironmentVariables(field.Addr().Interface(), key)
		}
	}
}

// getEffectiveMCPConfigPath returns the path to the MCP config file
// Searches in this order: 1) project .infer/mcp.yaml, 2) user home ~/.infer/mcp.yaml
func getEffectiveMCPConfigPath() string {
	searchPaths := []string{
		".infer/mcp.yaml",
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(homeDir, ".infer", "mcp.yaml")
		searchPaths = append(searchPaths, homePath)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ".infer/mcp.yaml"
}

// getEffectiveKeybindingsConfigPath returns the path to the keybindings config file
// Searches in this order: 1) project .infer/keybindings.yaml, 2) user home ~/.infer/keybindings.yaml
func getEffectiveKeybindingsConfigPath() string {
	searchPaths := []string{
		config.DefaultKeybindingsPath,
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(homeDir, config.ConfigDirName, config.KeybindingsFileName)
		searchPaths = append(searchPaths, homePath)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return config.DefaultKeybindingsPath
}

// getEffectiveChannelsConfigPath returns the path to the channels config file
// Searches in this order: 1) project .infer/channels.yaml, 2) user home ~/.infer/channels.yaml
func getEffectiveChannelsConfigPath() string {
	searchPaths := []string{
		config.DefaultChannelsPath,
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(homeDir, config.ConfigDirName, config.ChannelsFileName)
		searchPaths = append(searchPaths, homePath)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return config.DefaultChannelsPath
}

// getEffectiveHeartbeatConfigPath returns the path to the heartbeat config file
// Searches in this order: 1) project .infer/heartbeat.yaml, 2) user home ~/.infer/heartbeat.yaml
func getEffectiveHeartbeatConfigPath() string {
	searchPaths := []string{
		config.DefaultHeartbeatPath,
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(homeDir, config.ConfigDirName, config.HeartbeatFileName)
		searchPaths = append(searchPaths, homePath)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return config.DefaultHeartbeatPath
}

// getEffectiveComputerUseConfigPath returns the path to the computer_use config file
// Searches in this order: 1) project .infer/computer_use.yaml, 2) user home ~/.infer/computer_use.yaml
func getEffectiveComputerUseConfigPath() string {
	searchPaths := []string{
		config.DefaultComputerUsePath,
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(homeDir, config.ConfigDirName, config.ComputerUseFileName)
		searchPaths = append(searchPaths, homePath)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return config.DefaultComputerUsePath
}

// getEffectivePromptsConfigPath returns the path to the prompts config file
// Searches in this order: 1) project .infer/prompts.yaml, 2) user home ~/.infer/prompts.yaml
func getEffectivePromptsConfigPath() string {
	searchPaths := []string{
		config.DefaultPromptsPath,
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(homeDir, config.ConfigDirName, config.PromptsFileName)
		searchPaths = append(searchPaths, homePath)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return config.DefaultPromptsPath
}

// getKeybindingsConfigWritePath returns the path to write keybindings to,
// honouring the --userspace flag.
func getKeybindingsConfigWritePath(userspace bool) (string, error) {
	if userspace {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		return filepath.Join(homeDir, config.ConfigDirName, config.KeybindingsFileName), nil
	}
	return config.DefaultKeybindingsPath, nil
}

// loadConfigFromViper assembles the in-memory Config by unmarshalling
// viper, then layering on the per-file YAML overlays (mcp, keybindings,
// prompts) and finally honouring INFER_* env overrides. It runs once at
// startup (initConfig); commands afterwards read the cached cmd.Cfg.
func loadConfigFromViper() (*config.Config, error) {
	cfg := &config.Config{}
	if err := V.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config from Viper: %w", err)
	}

	resolveViperEnvironmentVariables(cfg, "")
	mergeGlobalSandboxDirs(cfg)

	mcpConfigPath := getEffectiveMCPConfigPath()
	mcpConfig, err := config.LoadMCP(mcpConfigPath)
	if err != nil {
		logger.Warn("Failed to load MCP config, using defaults", "error", err, "path", mcpConfigPath)
		mcpConfig = config.DefaultMCPConfig()
	}
	cfg.MCP = *mcpConfig

	kbPath := getEffectiveKeybindingsConfigPath()
	kbConfig, err := config.LoadKeybindings(kbPath)
	if err != nil {
		logger.Warn("Failed to load keybindings config, using defaults", "error", err, "path", kbPath)
		kbConfig = config.DefaultKeybindingsConfig()
	}
	cfg.Chat.Keybindings = *kbConfig

	applyKeybindingEnvOverrides(cfg)

	promptsPath := getEffectivePromptsConfigPath()
	prompts, err := config.LoadPrompts(promptsPath)
	if err != nil {
		logger.Warn("Failed to load prompts config, using defaults", "error", err, "path", promptsPath)
		prompts = config.DefaultPromptsConfig()
	}
	cfg.Prompts = *prompts
	applyPromptsEnvOverrides(cfg)

	channelsPath := getEffectiveChannelsConfigPath()
	channelsCfg, err := config.LoadChannels(channelsPath)
	if err != nil {
		logger.Warn("Failed to load channels config, using defaults", "error", err, "path", channelsPath)
		channelsCfg = config.DefaultChannelsConfig()
	}
	cfg.Channels = *channelsCfg
	applyChannelsEnvOverrides(cfg)

	heartbeatPath := getEffectiveHeartbeatConfigPath()
	heartbeatCfg, err := config.LoadHeartbeat(heartbeatPath)
	if err != nil {
		logger.Warn("Failed to load heartbeat config, using defaults", "error", err, "path", heartbeatPath)
		heartbeatCfg = config.DefaultHeartbeatConfig()
	}
	cfg.Heartbeat = *heartbeatCfg
	applyHeartbeatEnvOverrides(cfg)

	cuPath := getEffectiveComputerUseConfigPath()
	cuCfg, err := config.LoadComputerUse(cuPath)
	if err != nil {
		logger.Warn("Failed to load computer_use config, using defaults", "error", err, "path", cuPath)
		cuCfg = config.DefaultComputerUseConfig()
	}
	cfg.ComputerUse = *cuCfg
	applyComputerUseEnvOverrides(cfg)

	return cfg, nil
}

// applyPromptsEnvOverrides lets users force a prompt from the
// environment. Run AFTER cfg.Prompts has been populated from
// prompts.yaml so envs win over the file.
func applyPromptsEnvOverrides(cfg *config.Config) {
	envOverrides := map[string]*string{
		"INFER_PROMPTS_AGENT_SYSTEM_PROMPT":                         &cfg.Prompts.Agent.SystemPrompt,
		"INFER_PROMPTS_AGENT_SYSTEM_PROMPT_PLAN":                    &cfg.Prompts.Agent.SystemPromptPlan,
		"INFER_PROMPTS_AGENT_SYSTEM_PROMPT_REMOTE":                  &cfg.Prompts.Agent.SystemPromptRemote,
		"INFER_PROMPTS_AGENT_SYSTEM_PROMPT_HEARTBEAT":               &cfg.Prompts.Agent.SystemPromptHeartbeat,
		"INFER_PROMPTS_AGENT_CUSTOM_INSTRUCTIONS":                   &cfg.Prompts.Agent.CustomInstructions,
		"INFER_PROMPTS_AGENT_SYSTEM_REMINDERS_REMINDER_TEXT":        &cfg.Prompts.Agent.SystemReminders.ReminderText,
		"INFER_PROMPTS_GIT_COMMIT_MESSAGE_SYSTEM_PROMPT":            &cfg.Prompts.Git.CommitMessage.SystemPrompt,
		"INFER_PROMPTS_CONVERSATION_TITLE_GENERATION_SYSTEM_PROMPT": &cfg.Prompts.Conversation.TitleGeneration.SystemPrompt,
		"INFER_PROMPTS_INIT_PROMPT":                                 &cfg.Prompts.Init.Prompt,

		"INFER_PROMPTS_TOOLS_BASH_DESCRIPTION":                  &cfg.Prompts.Tools.Bash.Description,
		"INFER_PROMPTS_TOOLS_BASH_OUTPUT_DESCRIPTION":           &cfg.Prompts.Tools.BashOutput.Description,
		"INFER_PROMPTS_TOOLS_KILL_SHELL_DESCRIPTION":            &cfg.Prompts.Tools.KillShell.Description,
		"INFER_PROMPTS_TOOLS_LIST_SHELLS_DESCRIPTION":           &cfg.Prompts.Tools.ListShells.Description,
		"INFER_PROMPTS_TOOLS_READ_DESCRIPTION":                  &cfg.Prompts.Tools.Read.Description,
		"INFER_PROMPTS_TOOLS_WRITE_DESCRIPTION":                 &cfg.Prompts.Tools.Write.Description,
		"INFER_PROMPTS_TOOLS_EDIT_DESCRIPTION":                  &cfg.Prompts.Tools.Edit.Description,
		"INFER_PROMPTS_TOOLS_MULTI_EDIT_DESCRIPTION":            &cfg.Prompts.Tools.MultiEdit.Description,
		"INFER_PROMPTS_TOOLS_DELETE_DESCRIPTION":                &cfg.Prompts.Tools.Delete.Description,
		"INFER_PROMPTS_TOOLS_GREP_DESCRIPTION":                  &cfg.Prompts.Tools.Grep.Description,
		"INFER_PROMPTS_TOOLS_TREE_DESCRIPTION":                  &cfg.Prompts.Tools.Tree.Description,
		"INFER_PROMPTS_TOOLS_TODO_WRITE_DESCRIPTION":            &cfg.Prompts.Tools.TodoWrite.Description,
		"INFER_PROMPTS_TOOLS_REQUEST_PLAN_APPROVAL_DESCRIPTION": &cfg.Prompts.Tools.RequestPlanApproval.Description,
		"INFER_PROMPTS_TOOLS_WEB_FETCH_DESCRIPTION":             &cfg.Prompts.Tools.WebFetch.Description,
		"INFER_PROMPTS_TOOLS_WEB_SEARCH_DESCRIPTION":            &cfg.Prompts.Tools.WebSearch.Description,
		"INFER_PROMPTS_TOOLS_SCHEDULE_DESCRIPTION":              &cfg.Prompts.Tools.Schedule.Description,
		"INFER_PROMPTS_TOOLS_A2A_QUERY_AGENT_DESCRIPTION":       &cfg.Prompts.Tools.A2AQueryAgent.Description,
		"INFER_PROMPTS_TOOLS_A2A_QUERY_TASK_DESCRIPTION":        &cfg.Prompts.Tools.A2AQueryTask.Description,
		"INFER_PROMPTS_TOOLS_A2A_SUBMIT_TASK_DESCRIPTION":       &cfg.Prompts.Tools.A2ASubmitTask.Description,
		"INFER_PROMPTS_TOOLS_MOUSE_MOVE_DESCRIPTION":            &cfg.Prompts.Tools.MouseMove.Description,
		"INFER_PROMPTS_TOOLS_MOUSE_CLICK_DESCRIPTION":           &cfg.Prompts.Tools.MouseClick.Description,
		"INFER_PROMPTS_TOOLS_MOUSE_SCROLL_DESCRIPTION":          &cfg.Prompts.Tools.MouseScroll.Description,
		"INFER_PROMPTS_TOOLS_KEYBOARD_TYPE_DESCRIPTION":         &cfg.Prompts.Tools.KeyboardType.Description,
		"INFER_PROMPTS_TOOLS_GET_FOCUSED_APP_DESCRIPTION":       &cfg.Prompts.Tools.GetFocusedApp.Description,
		"INFER_PROMPTS_TOOLS_ACTIVATE_APP_DESCRIPTION":          &cfg.Prompts.Tools.ActivateApp.Description,
		"INFER_PROMPTS_TOOLS_GET_LATEST_SCREENSHOT_DESCRIPTION": &cfg.Prompts.Tools.GetLatestScreenshot.Description,
	}

	for envKey, target := range envOverrides {
		if val, ok := os.LookupEnv(envKey); ok {
			*target = val
		}
	}

	if v := os.Getenv("INFER_PROMPTS_AGENT_SYSTEM_REMINDERS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Prompts.Agent.SystemReminders.Enabled = b
		}
	}
	if v := os.Getenv("INFER_PROMPTS_AGENT_SYSTEM_REMINDERS_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Prompts.Agent.SystemReminders.Interval = n
		}
	}
}

// applyKeybindingEnvOverrides walks INFER_CHAT_KEYBINDINGS_BINDINGS_*
// environment variables and applies them directly to the in-memory
// keybindings config. Run AFTER loading keybindings.yaml so env vars win.
//
// Supported forms:
//
//	INFER_CHAT_KEYBINDINGS_BINDINGS_<ACTION_ID>_KEYS="key1,key2"
//	INFER_CHAT_KEYBINDINGS_BINDINGS_<ACTION_ID>_ENABLED="true|false"
func applyKeybindingEnvOverrides(cfg *config.Config) {
	const prefix = "INFER_CHAT_KEYBINDINGS_BINDINGS_"

	if cfg.Chat.Keybindings.Bindings == nil {
		cfg.Chat.Keybindings.Bindings = make(map[string]config.KeyBindingEntry)
	}

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
		actionID := strings.ToLower(strings.Join(parts[:len(parts)-1], "_"))

		entry := cfg.Chat.Keybindings.Bindings[actionID]

		switch field {
		case "KEYS":
			var keys []string
			for _, key := range strings.FieldsFunc(envValue, func(c rune) bool {
				return c == ',' || c == '\n'
			}) {
				if trimmed := strings.TrimSpace(key); trimmed != "" {
					keys = append(keys, trimmed)
				}
			}
			if len(keys) > 0 {
				entry.Keys = keys
				cfg.Chat.Keybindings.Bindings[actionID] = entry
			}
		case "ENABLED":
			val := strings.ToLower(strings.TrimSpace(envValue))
			if val == "true" || val == "false" {
				enabled := val == "true"
				entry.Enabled = &enabled
				cfg.Chat.Keybindings.Bindings[actionID] = entry
			}
		}
	}
}

// applyChannelsEnvOverrides applies INFER_CHANNELS_* env vars onto the
// in-memory channels config. Run AFTER LoadChannels so envs win over
// channels.yaml. The channels config now lives in its own file
// (yaml:"-" mapstructure:"-" on Config.Channels), so viper does not bind
// these env vars itself - this function is the single source of env-var
// support. Mirrors applyKeybindingEnvOverrides / applyPromptsEnvOverrides.
func applyChannelsEnvOverrides(cfg *config.Config) {
	setBool := func(env string, target *bool) {
		val, ok := os.LookupEnv(env)
		if !ok {
			return
		}
		if b, err := strconv.ParseBool(strings.TrimSpace(val)); err == nil {
			*target = b
		}
	}
	setInt := func(env string, target *int) {
		val, ok := os.LookupEnv(env)
		if !ok {
			return
		}
		if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			*target = n
		}
	}
	setString := func(env string, target *string) {
		if val, ok := os.LookupEnv(env); ok {
			*target = val
		}
	}
	setStringSlice := func(env string, target *[]string) {
		val, ok := os.LookupEnv(env)
		if !ok {
			return
		}
		var out []string
		for item := range strings.SplitSeq(val, ",") {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if out == nil {
			out = []string{}
		}
		*target = out
	}

	setBool("INFER_CHANNELS_ENABLED", &cfg.Channels.Enabled)
	setBool("INFER_CHANNELS_REQUIRE_APPROVAL", &cfg.Channels.RequireApproval)
	setInt("INFER_CHANNELS_MAX_WORKERS", &cfg.Channels.MaxWorkers)
	setInt("INFER_CHANNELS_IMAGE_RETENTION", &cfg.Channels.ImageRetention)

	setBool("INFER_CHANNELS_TELEGRAM_ENABLED", &cfg.Channels.Telegram.Enabled)
	setString("INFER_CHANNELS_TELEGRAM_BOT_TOKEN", &cfg.Channels.Telegram.BotToken)
	setStringSlice("INFER_CHANNELS_TELEGRAM_ALLOWED_USERS", &cfg.Channels.Telegram.AllowedUsers)
	setInt("INFER_CHANNELS_TELEGRAM_POLL_TIMEOUT", &cfg.Channels.Telegram.PollTimeout)

	setBool("INFER_CHANNELS_WHATSAPP_ENABLED", &cfg.Channels.WhatsApp.Enabled)
	setString("INFER_CHANNELS_WHATSAPP_PHONE_NUMBER_ID", &cfg.Channels.WhatsApp.PhoneNumberID)
	setString("INFER_CHANNELS_WHATSAPP_ACCESS_TOKEN", &cfg.Channels.WhatsApp.AccessToken)
	setString("INFER_CHANNELS_WHATSAPP_VERIFY_TOKEN", &cfg.Channels.WhatsApp.VerifyToken)
	setInt("INFER_CHANNELS_WHATSAPP_WEBHOOK_PORT", &cfg.Channels.WhatsApp.WebhookPort)
	setStringSlice("INFER_CHANNELS_WHATSAPP_ALLOWED_USERS", &cfg.Channels.WhatsApp.AllowedUsers)
}

// applyHeartbeatEnvOverrides applies INFER_HEARTBEAT_* env vars onto
// the in-memory heartbeat config. Run AFTER LoadHeartbeat so envs win
// over heartbeat.yaml. The heartbeat config lives in its own file
// (yaml:"-" mapstructure:"-" on Config.Heartbeat), so viper does not
// bind these env vars itself - this function is the single source of
// env-var support. Mirrors applyChannelsEnvOverrides.
func applyHeartbeatEnvOverrides(cfg *config.Config) {
	setBool := func(env string, target *bool) {
		val, ok := os.LookupEnv(env)
		if !ok {
			return
		}
		if b, err := strconv.ParseBool(strings.TrimSpace(val)); err == nil {
			*target = b
		}
	}
	setString := func(env string, target *string) {
		if val, ok := os.LookupEnv(env); ok {
			*target = val
		}
	}

	setBool("INFER_HEARTBEAT_ENABLED", &cfg.Heartbeat.Enabled)
	setString("INFER_HEARTBEAT_INTERVAL", &cfg.Heartbeat.Interval)
	setString("INFER_HEARTBEAT_INITIAL_DELAY", &cfg.Heartbeat.InitialDelay)
	setString("INFER_HEARTBEAT_MODEL", &cfg.Heartbeat.Model)
	setString("INFER_HEARTBEAT_PROMPT", &cfg.Heartbeat.Prompt)
}

// applyComputerUseEnvOverrides applies INFER_COMPUTER_USE_* env vars onto
// the in-memory computer_use config. Run AFTER LoadComputerUse so envs win
// over computer_use.yaml. The computer_use config now lives in its own
// file (yaml:"-" mapstructure:"-" on Config.ComputerUse), so viper does not
// bind these env vars itself - this function is the single source of
// env-var support. Mirrors applyChannelsEnvOverrides.
func applyComputerUseEnvOverrides(cfg *config.Config) {
	setBool := func(env string, target *bool) {
		val, ok := os.LookupEnv(env)
		if !ok {
			return
		}
		if b, err := strconv.ParseBool(strings.TrimSpace(val)); err == nil {
			*target = b
		}
	}
	setInt := func(env string, target *int) {
		val, ok := os.LookupEnv(env)
		if !ok {
			return
		}
		if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			*target = n
		}
	}
	setString := func(env string, target *string) {
		if val, ok := os.LookupEnv(env); ok {
			*target = val
		}
	}

	setBool("INFER_COMPUTER_USE_ENABLED", &cfg.ComputerUse.Enabled)

	setBool("INFER_COMPUTER_USE_FLOATING_WINDOW_ENABLED", &cfg.ComputerUse.FloatingWindow.Enabled)
	setBool("INFER_COMPUTER_USE_FLOATING_WINDOW_RESPAWN_ON_CLOSE", &cfg.ComputerUse.FloatingWindow.RespawnOnClose)
	setString("INFER_COMPUTER_USE_FLOATING_WINDOW_POSITION", &cfg.ComputerUse.FloatingWindow.Position)
	setBool("INFER_COMPUTER_USE_FLOATING_WINDOW_ALWAYS_ON_TOP", &cfg.ComputerUse.FloatingWindow.AlwaysOnTop)

	setBool("INFER_COMPUTER_USE_SCREENSHOT_ENABLED", &cfg.ComputerUse.Screenshot.Enabled)
	setInt("INFER_COMPUTER_USE_SCREENSHOT_MAX_WIDTH", &cfg.ComputerUse.Screenshot.MaxWidth)
	setInt("INFER_COMPUTER_USE_SCREENSHOT_MAX_HEIGHT", &cfg.ComputerUse.Screenshot.MaxHeight)
	setInt("INFER_COMPUTER_USE_SCREENSHOT_TARGET_WIDTH", &cfg.ComputerUse.Screenshot.TargetWidth)
	setInt("INFER_COMPUTER_USE_SCREENSHOT_TARGET_HEIGHT", &cfg.ComputerUse.Screenshot.TargetHeight)
	setString("INFER_COMPUTER_USE_SCREENSHOT_FORMAT", &cfg.ComputerUse.Screenshot.Format)
	setInt("INFER_COMPUTER_USE_SCREENSHOT_QUALITY", &cfg.ComputerUse.Screenshot.Quality)
	setBool("INFER_COMPUTER_USE_SCREENSHOT_STREAMING_ENABLED", &cfg.ComputerUse.Screenshot.StreamingEnabled)
	setInt("INFER_COMPUTER_USE_SCREENSHOT_CAPTURE_INTERVAL", &cfg.ComputerUse.Screenshot.CaptureInterval)
	setInt("INFER_COMPUTER_USE_SCREENSHOT_BUFFER_SIZE", &cfg.ComputerUse.Screenshot.BufferSize)
	setString("INFER_COMPUTER_USE_SCREENSHOT_TEMP_DIR", &cfg.ComputerUse.Screenshot.TempDir)
	setBool("INFER_COMPUTER_USE_SCREENSHOT_LOG_CAPTURES", &cfg.ComputerUse.Screenshot.LogCaptures)
	setBool("INFER_COMPUTER_USE_SCREENSHOT_SHOW_OVERLAY", &cfg.ComputerUse.Screenshot.ShowOverlay)

	setBool("INFER_COMPUTER_USE_RATE_LIMIT_ENABLED", &cfg.ComputerUse.RateLimit.Enabled)
	setInt("INFER_COMPUTER_USE_RATE_LIMIT_MAX_ACTIONS_PER_MINUTE", &cfg.ComputerUse.RateLimit.MaxActionsPerMinute)
	setInt("INFER_COMPUTER_USE_RATE_LIMIT_WINDOW_SECONDS", &cfg.ComputerUse.RateLimit.WindowSeconds)

	setBool("INFER_COMPUTER_USE_TOOLS_MOUSE_MOVE_ENABLED", &cfg.ComputerUse.Tools.MouseMove.Enabled)
	setBool("INFER_COMPUTER_USE_TOOLS_MOUSE_CLICK_ENABLED", &cfg.ComputerUse.Tools.MouseClick.Enabled)
	setBool("INFER_COMPUTER_USE_TOOLS_MOUSE_SCROLL_ENABLED", &cfg.ComputerUse.Tools.MouseScroll.Enabled)
	setBool("INFER_COMPUTER_USE_TOOLS_KEYBOARD_TYPE_ENABLED", &cfg.ComputerUse.Tools.KeyboardType.Enabled)
	setInt("INFER_COMPUTER_USE_TOOLS_KEYBOARD_TYPE_MAX_TEXT_LENGTH", &cfg.ComputerUse.Tools.KeyboardType.MaxTextLength)
	setInt("INFER_COMPUTER_USE_TOOLS_KEYBOARD_TYPE_TYPING_DELAY_MS", &cfg.ComputerUse.Tools.KeyboardType.TypingDelayMs)
	setBool("INFER_COMPUTER_USE_TOOLS_GET_FOCUSED_APP_ENABLED", &cfg.ComputerUse.Tools.GetFocusedApp.Enabled)
	setBool("INFER_COMPUTER_USE_TOOLS_ACTIVATE_APP_ENABLED", &cfg.ComputerUse.Tools.ActivateApp.Enabled)
}

// GetUserspaceFlag checks for --userspace flag on the current command or parent commands
func GetUserspaceFlag(cmd *cobra.Command) bool {
	if userspace, err := cmd.Flags().GetBool("userspace"); err == nil && userspace {
		return true
	}

	parent := cmd.Parent()
	for parent != nil {
		if userspace, err := parent.Flags().GetBool("userspace"); err == nil && userspace {
			return true
		}
		parent = parent.Parent()
	}

	return false
}

func init() {
	configInitCmd.Flags().Bool("overwrite", false, "Overwrite existing configuration file")
	configCmd.PersistentFlags().Bool("userspace", false, "Apply to userspace configuration (~/.infer/) instead of project configuration")

	configCmd.AddCommand(configInitCmd)

	rootCmd.AddCommand(configCmd)
}

// mergeGlobalSandboxDirs unions the userspace ~/.infer/config.yaml sandbox
// directories into cfg so a project config.yaml (which viper reads in
// isolation) does not fully shadow the user's global allowlist. Runs at load
// time only and is never written back, so config files stay clean. Skills
// directories remain reachable via the isWithinSkillsDir carve-out regardless.
func mergeGlobalSandboxDirs(cfg *config.Config) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	globalPath := filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
	if sameConfigFile(globalPath, V.ConfigFileUsed()) {
		return
	}
	if _, err := os.Stat(globalPath); err != nil {
		return
	}

	gv := viper.New()
	gv.SetConfigFile(globalPath)
	if err := gv.ReadInConfig(); err != nil {
		logger.Warn("failed to read global config for sandbox merge", "path", globalPath, "error", err)
		return
	}
	cfg.MergeSandboxDirectories(gv.GetStringSlice("tools.sandbox.directories"))
}

// sameConfigFile reports whether two config paths point at the same file,
// comparing absolute paths so a relative active path and an absolute global
// path do not look distinct.
func sameConfigFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aAbs, errA := filepath.Abs(a)
	bAbs, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return aAbs == bAbs
}
