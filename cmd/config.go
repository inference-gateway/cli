package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	cobra "github.com/spf13/cobra"

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
	Long: `Initialize a new config.yaml with default settings.

By default this writes the userspace baseline at ~/.infer/config.yaml. Pass
--project to write a project-level ./.infer/config.yaml that overrides the
baseline key-by-key instead.

For complete project initialization, use 'infer init' instead.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		project := GetProjectFlag(cmd)
		overwrite, _ := cmd.Flags().GetBool("overwrite")

		var configPath string
		if project {
			configPath = config.DefaultConfigPath
		} else {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get user home directory: %w", err)
			}
			configPath = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
		}

		if _, err := os.Stat(configPath); err == nil {
			if !overwrite {
				return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
			}
		}

		if err := configutils.SaveYAML(configPath, "config", config.DefaultConfig()); err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}

		scopeDesc := "userspace "
		if project {
			scopeDesc = "project "
		}

		fmt.Printf("Successfully created %sconfiguration: %s\n", scopeDesc, configPath)
		if project {
			fmt.Println("This project configuration overrides your userspace baseline (~/.infer/) key-by-key.")
		} else {
			fmt.Println("This userspace configuration is the shared baseline for all your projects.")
			fmt.Println("Project-level configurations are merged on top when present.")
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

// getEffectiveMemoryConfigPath returns the path to the memory config file
// Searches in this order: 1) project .infer/memory.yaml, 2) user home ~/.infer/memory.yaml
func getEffectiveMemoryConfigPath() string {
	searchPaths := []string{
		config.DefaultMemoryConfigPath,
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(homeDir, config.ConfigDirName, config.MemoryConfigFileName)
		searchPaths = append(searchPaths, homePath)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return config.DefaultMemoryConfigPath
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

// getEffectiveRemindersConfigPath returns the path to the reminders config file
// Searches in this order: 1) project .infer/reminders.yaml, 2) user home ~/.infer/reminders.yaml
func getEffectiveRemindersConfigPath() string {
	searchPaths := []string{
		config.DefaultRemindersPath,
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(homeDir, config.ConfigDirName, config.RemindersFileName)
		searchPaths = append(searchPaths, homePath)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return config.DefaultRemindersPath
}

// resolveRemindersConfig resolves the reminders configuration, layering the
// content sources embedded consumers need (issue #733) on top of the on-disk
// files. Precedence, highest first:
//  1. INFER_REMINDERS_CONFIG - inline YAML, so a consumer (e.g. infer-action)
//     never has to write ~/.infer/reminders.yaml.
//  2. --reminders-file - an arbitrary path, not constrained to ~/.infer/.
//  3. project .infer/reminders.yaml, then ~/.infer/reminders.yaml
//     (getEffectiveRemindersConfigPath).
//  4. built-in defaults (LoadReminders returns them when the file is missing).
//
// Env wins over the flag, matching the documented flags < env layering.
//
// When the resolved config has Merge=true, its entries are merged onto the
// built-in defaults by name instead of replacing them (see MergeWithDefaults).
// This lets consumers add reminders without re-declaring the built-in set.
func resolveRemindersConfig() (*config.RemindersConfig, error) {
	var cfg *config.RemindersConfig
	var err error

	if inline := strings.TrimSpace(os.Getenv("INFER_REMINDERS_CONFIG")); inline != "" {
		cfg, err = config.ParseReminders([]byte(inline))
	} else if path := remindersFileOverride(); path != "" {
		cfg, err = config.LoadReminders(path)
	} else {
		cfg, err = config.LoadReminders(getEffectiveRemindersConfigPath())
	}
	if err != nil {
		return nil, err
	}

	if cfg.Merge {
		cfg = cfg.MergeWithDefaults()
	}

	return cfg, nil
}

// remindersFileOverride returns the --reminders-file persistent flag value when
// the user set it, mirroring resolveBashAllowOverride's flag lookup. Empty means
// the flag was not provided.
func remindersFileOverride() string {
	if !rootCmd.PersistentFlags().Changed("reminders-file") {
		return ""
	}
	path, err := rootCmd.PersistentFlags().GetString("reminders-file")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(path)
}

// getEffectiveHooksConfigPath returns the path to the hooks config file.
// Searches in this order: 1) project .infer/hooks.yaml, 2) user home ~/.infer/hooks.yaml
func getEffectiveHooksConfigPath() string {
	searchPaths := []string{
		config.DefaultHooksPath,
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(homeDir, config.ConfigDirName, config.HooksFileName)
		searchPaths = append(searchPaths, homePath)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return config.DefaultHooksPath
}

// getKeybindingsConfigWritePath returns the path to write keybindings to.
// Keybindings are a userspace-only concern, so writes target ~/.infer/ by
// default; --project (toProject) opts into a project-level override instead.
func getKeybindingsConfigWritePath(toProject bool) (string, error) {
	if toProject {
		return config.DefaultKeybindingsPath, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(homeDir, config.ConfigDirName, config.KeybindingsFileName), nil
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

	mcpConfigPath := getEffectiveMCPConfigPath()
	mcpConfig, err := config.LoadMCP(mcpConfigPath)
	if err != nil {
		logger.Warn("failed to load MCP config, using defaults", "error", err, "path", mcpConfigPath)
		mcpConfig = config.DefaultMCPConfig()
	}
	cfg.MCP = *mcpConfig

	kbPath := getEffectiveKeybindingsConfigPath()
	kbConfig, err := config.LoadKeybindings(kbPath)
	if err != nil {
		logger.Warn("failed to load keybindings config, using defaults", "error", err, "path", kbPath)
		kbConfig = config.DefaultKeybindingsConfig()
	}
	cfg.Chat.Keybindings = *kbConfig

	applyKeybindingEnvOverrides(cfg)

	promptsPath := getEffectivePromptsConfigPath()
	prompts, err := config.LoadPrompts(promptsPath)
	if err != nil {
		logger.Warn("failed to load prompts config, using defaults", "error", err, "path", promptsPath)
		prompts = config.DefaultPromptsConfig()
	}
	cfg.Prompts = *prompts
	applyPromptsEnvOverrides(cfg)
	warnDeadPromptEnvVars()

	remindersCfg, err := resolveRemindersConfig()
	if err != nil {
		logger.Warn("failed to load reminders config, using defaults", "error", err)
		remindersCfg = config.DefaultRemindersConfig()
	}
	cfg.Reminders = *remindersCfg
	applyRemindersEnvOverrides(cfg)

	hooksPath := getEffectiveHooksConfigPath()
	hooksCfg, err := config.LoadHooks(hooksPath)
	if err != nil {
		logger.Warn("failed to load hooks config, using defaults", "error", err, "path", hooksPath)
		hooksCfg = config.DefaultHooksConfig()
	}
	cfg.Hooks = *hooksCfg
	applyHooksEnvOverrides(cfg)

	channelsPath := getEffectiveChannelsConfigPath()
	channelsCfg, err := config.LoadChannels(channelsPath)
	if err != nil {
		logger.Warn("failed to load channels config, using defaults", "error", err, "path", channelsPath)
		channelsCfg = config.DefaultChannelsConfig()
	}
	cfg.Channels = *channelsCfg
	applyChannelsEnvOverrides(cfg)

	heartbeatPath := getEffectiveHeartbeatConfigPath()
	heartbeatCfg, err := config.LoadHeartbeat(heartbeatPath)
	if err != nil {
		logger.Warn("failed to load heartbeat config, using defaults", "error", err, "path", heartbeatPath)
		heartbeatCfg = config.DefaultHeartbeatConfig()
	}
	cfg.Heartbeat = *heartbeatCfg
	applyHeartbeatEnvOverrides(cfg)

	cuPath := getEffectiveComputerUseConfigPath()
	cuCfg, err := config.LoadComputerUse(cuPath)
	if err != nil {
		logger.Warn("failed to load computer_use config, using defaults", "error", err, "path", cuPath)
		cuCfg = config.DefaultComputerUseConfig()
	}
	cfg.ComputerUse = *cuCfg
	applyComputerUseEnvOverrides(cfg)

	memoryPath := getEffectiveMemoryConfigPath()
	memoryCfg, err := config.LoadMemory(memoryPath)
	if err != nil {
		logger.Warn("failed to load memory config, using defaults", "error", err, "path", memoryPath)
		memoryCfg = config.DefaultMemoryConfig()
	}
	cfg.Memory = *memoryCfg
	applyMemoryEnvOverrides(cfg)
	pruneMemoryRemindersIfDisabled(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

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
		"INFER_PROMPTS_AGENT_SYSTEM_PROMPT_CLAUDE_CODE":             &cfg.Prompts.Agent.SystemPromptClaudeCode,
		"INFER_PROMPTS_AGENT_CUSTOM_INSTRUCTIONS":                   &cfg.Prompts.Agent.CustomInstructions,
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
}

// warnDeadPromptEnvVars logs a warning when a known-dead env var (renamed
// in v0.105.0) is set, so consumers following stale docs get a visible
// signal instead of silent ignore.
func warnDeadPromptEnvVars() {
	deadVars := []string{
		"INFER_AGENT_SYSTEM_PROMPT",
		"INFER_AGENT_SYSTEM_PROMPT_PLAN",
	}
	for _, name := range deadVars {
		if _, ok := os.LookupEnv(name); ok {
			logger.Warn("environment variable %s is no longer supported (renamed in v0.105.0); see INFER_PROMPTS_AGENT_SYSTEM_PROMPT / INFER_PROMPTS_AGENT_SYSTEM_PROMPT_PLAN in docs/configuration-reference.md", "var", name)
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

// applyRemindersEnvOverrides applies INFER_REMINDERS_* env vars onto the
// in-memory reminders config. Run AFTER LoadReminders so envs win over
// reminders.yaml. The reminders list itself is file-driven (like other complex
// lists); only the master switch takes a scalar env override.
func applyRemindersEnvOverrides(cfg *config.Config) {
	if v, ok := os.LookupEnv("INFER_REMINDERS_ENABLED"); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			cfg.Reminders.Enabled = b
		}
	}
}

// applyMemoryEnvOverrides applies INFER_MEMORY_* env vars onto the in-memory
// memory config. Run AFTER LoadMemory so envs win over memory.yaml. The memory
// config lives in its own file (yaml:"-" mapstructure:"-" on Config.Memory), so
// viper does not bind these env vars itself. Mirrors applyHeartbeatEnvOverrides.
func applyMemoryEnvOverrides(cfg *config.Config) {
	if v, ok := os.LookupEnv("INFER_MEMORY_ENABLED"); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			cfg.Memory.Enabled = b
		}
	}
	if v, ok := os.LookupEnv("INFER_MEMORY_DIR"); ok {
		cfg.Memory.Dir = v
	}
	if v, ok := os.LookupEnv("INFER_MEMORY_MAX_CHARS"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cfg.Memory.MaxChars = n
		}
	}
	if v, ok := os.LookupEnv("INFER_MEMORY_MAX_ENTRY_CHARS"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cfg.Memory.MaxEntryChars = n
		}
	}
	if v, ok := os.LookupEnv("INFER_MEMORY_BACKEND_TYPE"); ok {
		cfg.Memory.Backend.Type = v
	}
	if v, ok := os.LookupEnv("INFER_MEMORY_BACKEND_GIT_REPO"); ok {
		cfg.Memory.Backend.Git.Repo = v
	}
	if v, ok := os.LookupEnv("INFER_MEMORY_BACKEND_GIT_BRANCH"); ok {
		cfg.Memory.Backend.Git.Branch = v
	}
	if v, ok := os.LookupEnv("INFER_MEMORY_BACKEND_GIT_COMMIT_MESSAGE"); ok {
		cfg.Memory.Backend.Git.CommitMessage = v
	}
	if v, ok := os.LookupEnv("INFER_MEMORY_BACKEND_GIT_TIMEOUT"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cfg.Memory.Backend.Git.Timeout = n
		}
	}
	if v, ok := os.LookupEnv("INFER_MEMORY_BACKEND_GIT_SYNC_ON_START"); ok {
		cfg.Memory.Backend.Git.Sync.OnStart = v
	}
	if v, ok := os.LookupEnv("INFER_MEMORY_BACKEND_GIT_SYNC_ON_FINISH"); ok {
		cfg.Memory.Backend.Git.Sync.OnFinish = v
	}
}

// pruneMemoryRemindersIfDisabled drops the built-in memory reminders (see
// config.MemoryReminders) when memory is disabled, so the enabled-by-default
// reminder set does not tell the agent to consult or record memory that isn't
// active. When memory is enabled the built-ins are delivered through the config
// file (fresh init, or `init --overwrite`), keeping reminders.yaml the single
// source of truth. Run AFTER both reminders and memory config are loaded.
func pruneMemoryRemindersIfDisabled(cfg *config.Config) {
	if cfg.Memory.Enabled {
		return
	}
	builtin := make(map[string]bool)
	for _, r := range config.MemoryReminders() {
		builtin[r.Name] = true
	}
	kept := make([]config.ReminderConfig, 0, len(cfg.Reminders.Reminders))
	for _, r := range cfg.Reminders.Reminders {
		if builtin[r.Name] {
			continue
		}
		kept = append(kept, r)
	}
	cfg.Reminders.Reminders = kept
}

// applyHooksEnvOverrides applies INFER_HOOKS_* env vars onto the in-memory hooks
// config. Run AFTER LoadHooks so envs win over hooks.yaml. The hooks list itself
// is file-driven; only the master switch takes a scalar env override. Mirrors
// applyRemindersEnvOverrides.
func applyHooksEnvOverrides(cfg *config.Config) {
	if v, ok := os.LookupEnv("INFER_HOOKS_ENABLED"); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			cfg.Hooks.Enabled = b
		}
	}
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

// GetProjectFlag checks for the --project flag on the current command or any
// parent command. Userspace-first model (issue #680): config writes target the
// home ~/.infer/ baseline by default; --project opts into a project override.
func GetProjectFlag(cmd *cobra.Command) bool {
	if project, err := cmd.Flags().GetBool("project"); err == nil && project {
		return true
	}

	parent := cmd.Parent()
	for parent != nil {
		if project, err := parent.Flags().GetBool("project"); err == nil && project {
			return true
		}
		parent = parent.Parent()
	}

	return false
}

func init() {
	configInitCmd.Flags().Bool("overwrite", false, "Overwrite existing configuration file")
	configCmd.PersistentFlags().Bool("project", false, "Apply to the project configuration (./.infer/) instead of the userspace baseline (~/.infer/)")

	configCmd.AddCommand(configInitCmd)

	rootCmd.AddCommand(configCmd)
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
