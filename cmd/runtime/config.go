package runtime

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
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// resolveViperEnvironmentVariables applies INFER_* overrides to cfg after
// unmarshalling, including pointer options Viper cannot set directly.
func resolveViperEnvironmentVariables(v *viper.Viper, cfg any, keyPrefix string) {
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
		tag = strings.SplitN(tag, ",", 2)[0]

		var key string
		if keyPrefix == "" {
			key = tag
		} else {
			key = keyPrefix + "." + tag
		}

		switch field.Kind() {
		case reflect.Slice:
			if v.IsSet(key) && field.Type().Elem().Kind() == reflect.String {
				field.Set(reflect.ValueOf(stringSliceFromViper(v, key)))
			}
		case reflect.Pointer:
			if field.Type().Elem().Kind() == reflect.Struct {
				if !field.IsNil() {
					resolveViperEnvironmentVariables(v, field.Interface(), key)
				}
				break
			}
			setPointerOption(v, key, field)
		case reflect.Struct:
			resolveViperEnvironmentVariables(v, field.Addr().Interface(), key)
		default:
			if v.IsSet(key) {
				setScalarFromViper(v, key, field)
			}
		}
	}
}

// setPointerOption applies a Viper key to a pointer-to-scalar field while
// preserving nil for unset tri-state options.
func setPointerOption(v *viper.Viper, key string, field reflect.Value) {
	if !v.IsSet(key) {
		return
	}
	ptr := reflect.New(field.Type().Elem())
	if !setScalarFromViper(v, key, ptr.Elem()) {
		return
	}
	field.Set(ptr)
}

// setScalarFromViper writes v's value for key into a settable scalar, reporting
// whether it wrote one. Bool and number values are trimmed and parsed strictly
// so a typo like INFER_X_REQUIRE_APPROVAL=maybe is ignored rather than cast to
// false and silently flipping a safety default.
func setScalarFromViper(v *viper.Viper, key string, field reflect.Value) bool {
	switch field.Kind() {
	case reflect.String:
		field.SetString(v.GetString(key))
		return true
	}

	raw := strings.TrimSpace(v.GetString(key))
	switch field.Kind() {
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return false
		}
		field.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return false
		}
		field.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return false
		}
		field.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return false
		}
		field.SetFloat(f)
	default:
		return false
	}
	return true
}

// stringSliceFromViper reads a []string key, comma-splitting a raw string
// value: viper's GetStringSlice whitespace-splits env strings, which shreds
// entries containing spaces (e.g. sandbox directory paths).
func stringSliceFromViper(v *viper.Viper, key string) []string {
	if raw, ok := v.Get(key).(string); ok {
		return parseDelimitedList(raw)
	}
	return v.GetStringSlice(key)
}

// sidecarPath returns the effective path of a sidecar config file:
// project .infer/<fileName> if it exists, else ~/.infer/<fileName> if it
// exists, else the project path (the loader then falls back to defaults).
func sidecarPath(fileName string) string {
	projectPath := config.ConfigDirName + "/" + fileName
	candidates := []string{projectPath}
	if homeDir, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(homeDir, config.ConfigDirName, fileName))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return projectPath
}

// applySidecarEnv applies INFER_<PREFIX>_* env vars onto a sidecar config
// through the same walker the main config uses. Sidecars are excluded from the
// main viper (mapstructure:"-") so legacy blocks in config.yaml cannot leak in;
// an env-only viper keeps that guarantee while still honouring env overrides.
// AllowEmptyEnv matches the previous os.LookupEnv semantics (set-but-empty wins).
func applySidecarEnv(cfg any, prefix string) {
	v := viper.New()
	v.SetEnvPrefix("INFER")
	v.AutomaticEnv()
	v.AllowEmptyEnv(true)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	resolveViperEnvironmentVariables(v, cfg, prefix)
}

// resolveRemindersConfig resolves the reminders configuration, layering the
// content sources embedded consumers need (issue #733) on top of the on-disk
// files. Precedence, highest first:
//  1. INFER_REMINDERS_CONFIG - inline YAML, so a consumer (e.g. infer-action)
//     never has to write ~/.infer/reminders.yaml.
//  2. --reminders-file - an arbitrary path, not constrained to ~/.infer/.
//  3. project .infer/reminders.yaml, then ~/.infer/reminders.yaml
//     (sidecarPath).
//  4. built-in defaults (LoadReminders returns them when the file is missing).
//
// Env wins over the flag, matching the documented flags < env layering.
//
// When the resolved config has Merge=true, its entries are merged onto the
// built-in defaults by name instead of replacing them (see MergeWithDefaults).
// This lets consumers add reminders without re-declaring the built-in set.
func resolveRemindersConfig(root *cobra.Command) (*config.RemindersConfig, error) {
	var cfg *config.RemindersConfig
	var err error

	if inline := strings.TrimSpace(os.Getenv("INFER_REMINDERS_CONFIG")); inline != "" {
		cfg, err = config.ParseReminders([]byte(inline))
	} else if path := remindersFileOverride(root); path != "" {
		cfg, err = config.LoadReminders(path)
	} else {
		cfg, err = config.LoadReminders(sidecarPath(config.RemindersFileName))
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
func remindersFileOverride(root *cobra.Command) string {
	if !root.PersistentFlags().Changed("reminders-file") {
		return ""
	}
	path, err := root.PersistentFlags().GetString("reminders-file")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(path)
}

// getPluginsConfigPath returns the path of the plugins registry. Plugins are
// userspace-only, so unlike other sidecars there is no project-first search.
func PluginsConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(config.ConfigDirName, config.PluginsFileName)
	}
	return filepath.Join(homeDir, config.ConfigDirName, config.PluginsFileName)
}

// getKeybindingsConfigWritePath returns the path to write keybindings to.
// Keybindings are a userspace-only concern, so writes target ~/.infer/ by
// default; --project (toProject) opts into a project-level override instead.
func KeybindingsConfigWritePath(toProject bool) (string, error) {
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
// startup; commands afterwards read the cached typed config from State.
//
//nolint:funlen
func loadConfigFromViper(v *viper.Viper, root *cobra.Command) (*config.Config, error) {
	cfg := &config.Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config from Viper: %w", err)
	}

	resolveViperEnvironmentVariables(v, cfg, "")

	mcpConfigPath := sidecarPath(config.MCPFileName)
	mcpConfig, err := config.LoadMCP(mcpConfigPath)
	if err != nil {
		logger.Warn("failed to load MCP config, using defaults", "error", err, "path", mcpConfigPath)
		mcpConfig = config.DefaultMCPConfig()
	}
	cfg.MCP = *mcpConfig

	kbPath := sidecarPath(config.KeybindingsFileName)
	kbConfig, err := config.LoadKeybindings(kbPath)
	if err != nil {
		logger.Warn("failed to load keybindings config, using defaults", "error", err, "path", kbPath)
		kbConfig = config.DefaultKeybindingsConfig()
	}
	cfg.Chat.Keybindings = *kbConfig

	applyKeybindingEnvOverrides(cfg)

	if v, ok := os.LookupEnv("INFER_CHAT_INPUT_MAX_LINES"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			cfg.Chat.InputMaxLines = n
		}
	}

	promptsPath := sidecarPath(config.PromptsFileName)
	prompts, err := config.LoadPrompts(promptsPath)
	if err != nil {
		logger.Warn("failed to load prompts config, using defaults", "error", err, "path", promptsPath)
		prompts = config.DefaultPromptsConfig()
	}
	cfg.Prompts = *prompts
	applyPromptsEnvOverrides(cfg)

	remindersCfg, err := resolveRemindersConfig(root)
	if err != nil {
		logger.Warn("failed to load reminders config, using defaults", "error", err)
		remindersCfg = config.DefaultRemindersConfig()
	}
	cfg.Reminders = *remindersCfg
	applySidecarEnv(&cfg.Reminders, "reminders")

	hooksPath := sidecarPath(config.HooksFileName)
	hooksCfg, err := config.LoadHooks(hooksPath)
	if err != nil {
		logger.Warn("failed to load hooks config, using defaults", "error", err, "path", hooksPath)
		hooksCfg = config.DefaultHooksConfig()
	}
	cfg.Hooks = *hooksCfg
	applySidecarEnv(&cfg.Hooks, "hooks")

	judgePath := sidecarPath(config.JudgeFileName)
	judgeCfg, err := config.LoadJudge(judgePath)
	if err != nil {
		logger.Warn("failed to load judge config, using defaults", "error", err, "path", judgePath)
		judgeCfg = config.DefaultJudgeConfig()
	}
	cfg.Judge = *judgeCfg
	applySidecarEnv(&cfg.Judge, "judge")

	channelsPath := sidecarPath(config.ChannelsFileName)
	channelsCfg, err := config.LoadChannels(channelsPath)
	if err != nil {
		logger.Warn("failed to load channels config, using defaults", "error", err, "path", channelsPath)
		channelsCfg = config.DefaultChannelsConfig()
	}
	cfg.Channels = *channelsCfg
	applySidecarEnv(&cfg.Channels, "channels")

	heartbeatPath := sidecarPath(config.HeartbeatFileName)
	heartbeatCfg, err := config.LoadHeartbeat(heartbeatPath)
	if err != nil {
		logger.Warn("failed to load heartbeat config, using defaults", "error", err, "path", heartbeatPath)
		heartbeatCfg = config.DefaultHeartbeatConfig()
	}
	cfg.Heartbeat = *heartbeatCfg
	applySidecarEnv(&cfg.Heartbeat, "heartbeat")

	cuPath := sidecarPath(config.ComputerUseFileName)
	cuCfg, err := config.LoadComputerUse(cuPath)
	if err != nil {
		logger.Warn("failed to load computer_use config, using defaults", "error", err, "path", cuPath)
		cuCfg = config.DefaultComputerUseConfig()
	}
	cfg.ComputerUse = *cuCfg
	applySidecarEnv(&cfg.ComputerUse, "computer_use")

	buPath := sidecarPath(config.BrowserUseFileName)
	buCfg, err := config.LoadBrowserUse(buPath)
	if err != nil {
		logger.Warn("failed to load browser_use config, using defaults", "error", err, "path", buPath)
		buCfg = config.DefaultBrowserUseConfig()
	}
	cfg.BrowserUse = *buCfg
	applySidecarEnv(&cfg.BrowserUse, "browser_use")
	cfg.BrowserUse.Extension.Port = cfg.BrowserUse.Extension.EffectivePort()

	memoryPath := sidecarPath(config.MemoryConfigFileName)
	memoryCfg, err := config.LoadMemory(memoryPath)
	if err != nil {
		logger.Warn("failed to load memory config, using defaults", "error", err, "path", memoryPath)
		memoryCfg = config.DefaultMemoryConfig()
	}
	cfg.Memory = *memoryCfg
	applySidecarEnv(&cfg.Memory, "memory")
	pruneMemoryRemindersIfDisabled(cfg)

	pluginsPath := PluginsConfigPath()
	pluginsCfg, err := config.LoadPlugins(pluginsPath)
	if err != nil {
		logger.Warn("failed to load plugins config, using defaults", "error", err, "path", pluginsPath)
		pluginsCfg = config.DefaultPluginsConfig()
	}
	cfg.Plugins = *pluginsCfg
	applySidecarEnv(&cfg.Plugins, "plugins")

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
		"INFER_PROMPTS_AGENT_SYSTEM_PROMPT_REMOTE":                  &cfg.Prompts.Agent.SystemPromptRemote,
		"INFER_PROMPTS_AGENT_SYSTEM_PROMPT_HEARTBEAT":               &cfg.Prompts.Agent.SystemPromptHeartbeat,
		"INFER_PROMPTS_AGENT_MODE_ADJUSTMENT_PLAN":                  &cfg.Prompts.Agent.ModeAdjustmentPlan,
		"INFER_PROMPTS_AGENT_MODE_ADJUSTMENT_AUTO":                  &cfg.Prompts.Agent.ModeAdjustmentAuto,
		"INFER_PROMPTS_AGENT_CUSTOM_INSTRUCTIONS":                   &cfg.Prompts.Agent.CustomInstructions,
		"INFER_PROMPTS_GIT_COMMIT_MESSAGE_SYSTEM_PROMPT":            &cfg.Prompts.Git.CommitMessage.SystemPrompt,
		"INFER_PROMPTS_CONVERSATION_TITLE_GENERATION_SYSTEM_PROMPT": &cfg.Prompts.Conversation.TitleGeneration.SystemPrompt,
		"INFER_PROMPTS_VISION_ANNOTATOR_SCREEN_SYSTEM_PROMPT":       &cfg.Prompts.Vision.Annotator.ScreenSystemPrompt,
		"INFER_PROMPTS_VISION_ANNOTATOR_SCENE_SYSTEM_PROMPT":        &cfg.Prompts.Vision.Annotator.SceneSystemPrompt,
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
		"INFER_PROMPTS_TOOLS_COMPUTER_DESCRIPTION":              &cfg.Prompts.Tools.Computer.Description,
		"INFER_PROMPTS_TOOLS_BROWSER_NAVIGATE_DESCRIPTION":      &cfg.Prompts.Tools.BrowserNavigate.Description,
		"INFER_PROMPTS_TOOLS_BROWSER_CLICK_DESCRIPTION":         &cfg.Prompts.Tools.BrowserClick.Description,
		"INFER_PROMPTS_TOOLS_BROWSER_TYPE_DESCRIPTION":          &cfg.Prompts.Tools.BrowserType.Description,
		"INFER_PROMPTS_TOOLS_BROWSER_READ_DESCRIPTION":          &cfg.Prompts.Tools.BrowserRead.Description,
		"INFER_PROMPTS_TOOLS_GET_LATEST_FRAME_DESCRIPTION":      &cfg.Prompts.Tools.GetLatestFrame.Description,
		"INFER_PROMPTS_TOOLS_IMAGE_DECODE_DESCRIPTION":          &cfg.Prompts.Tools.ImageDecode.Description,
	}

	for envKey, target := range envOverrides {
		if val, ok := os.LookupEnv(envKey); ok {
			*target = val
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

// GetProjectFlag checks for the --project flag on the current command or any
// parent command. Userspace-first model (issue #680): config writes target the
// home ~/.infer/ baseline by default; --project opts into a project override.
func ProjectFlag(cmd *cobra.Command) bool {
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
