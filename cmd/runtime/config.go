package runtime

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	mapstructure "github.com/go-viper/mapstructure/v2"
	cobra "github.com/spf13/cobra"
	viper "github.com/spf13/viper"
	yaml "gopkg.in/yaml.v3"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	sandbox "github.com/inference-gateway/cli/internal/sandbox"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

// sidecar is one split config file. Its contents are merged into the main
// Viper under key, so a block with the same key in config.yaml never reaches
// the section. homeOnly files have no project copy, fatal ones fail the load
// when broken, and fixup reshapes the parsed file before the merge.
type sidecar struct {
	key      string
	file     string
	homeOnly bool
	fatal    bool
	fixup    func(map[string]any) error
}

var sidecars = []sidecar{
	{key: "mcp", file: config.MCPFileName, fixup: keepMCPEnvCase},
	{key: "chat.keybindings", file: config.KeybindingsFileName},
	{key: "prompts", file: config.PromptsFileName, fixup: dropBlankPrompts},
	{key: "reminders", file: config.RemindersFileName},
	{key: "hooks", file: config.HooksFileName},
	{key: "judge", file: config.JudgeFileName},
	{key: "tools", file: config.ToolsFileName, homeOnly: true, fatal: true, fixup: flattenToolsLayout},
	{key: "tools.sandbox", file: config.SandboxFileName, homeOnly: true, fatal: true, fixup: typeSandboxEntries},
	{key: "channels", file: config.ChannelsFileName},
	{key: "heartbeat", file: config.HeartbeatFileName},
	{key: "computer_use", file: config.ComputerUseFileName},
	{key: "browser_use", file: config.BrowserUseFileName},
	{key: "daemon", file: config.DaemonFileName, fixup: keepWorkspaceEnvCase},
	{key: "memory", file: config.MemoryConfigFileName},
	{key: "plugins", file: config.PluginsFileName, homeOnly: true},
}

// SidecarFile returns the split file a dotted config key lives in, and false
// for a key that belongs to config.yaml.
func SidecarFile(key string) (string, bool) {
	for _, sc := range sidecars {
		if key == sc.key || strings.HasPrefix(key, sc.key+".") {
			return sc.file, true
		}
	}
	return "", false
}

func (sc sidecar) paths() []string {
	paths := []string{userConfigPath(sc.file)}
	if !sc.homeOnly {
		paths = append(paths, filepath.Join(config.ConfigDirName, sc.file))
	}
	return paths
}

func userConfigPath(file string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, config.ConfigDirName, file)
}

// mergeConfigLayers merges ~/.infer/config.yaml then ./.infer/config.yaml
// into v with every sidecar key stripped, so legacy blocks cannot leak into
// the split sections. Viper deep-merges maps and replaces lists wholesale.
func mergeConfigLayers(v *viper.Viper) error {
	home := userConfigPath(config.ConfigFileName)
	project := resolveProjectConfigPath()
	if sameConfigFile(project, home) {
		project = ""
	}
	for _, path := range []string{home, project} {
		if path == "" {
			continue
		}
		settings, err := readYAMLMap(path, false)
		if err != nil {
			return fmt.Errorf("reading config %s: %w", path, err)
		}
		for _, sc := range sidecars {
			deletePath(settings, sc.key)
		}
		if err := v.MergeConfigMap(settings); err != nil {
			return fmt.Errorf("merging config %s: %w", path, err)
		}
	}
	return nil
}

// mergeSidecar merges each existing layer of sc under its key. A broken
// layer is skipped with a warning unless the sidecar is fatal.
func mergeSidecar(v *viper.Viper, sc sidecar, paths []string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}
		settings, err := readYAMLMap(path, true)
		if err == nil && sc.fixup != nil && settings != nil {
			err = sc.fixup(settings)
		}
		if err != nil {
			if sc.fatal {
				return fmt.Errorf("loading %s: %w", path, err)
			}
			logger.Warn("failed to load config file, using defaults", "error", err, "path", path)
			continue
		}
		if err := mergeUnderKey(v, sc.key, settings); err != nil {
			return fmt.Errorf("merging %s: %w", path, err)
		}
	}
	return nil
}

func mergeUnderKey(v *viper.Viper, key string, settings map[string]any) error {
	if len(settings) == 0 {
		return nil
	}
	nested := any(settings)
	parts := strings.Split(key, ".")
	for i := len(parts) - 1; i >= 0; i-- {
		nested = map[string]any{parts[i]: nested}
	}
	return v.MergeConfigMap(nested.(map[string]any))
}

// readYAMLMap parses a YAML file into a map, nil when the file is missing.
// Sidecar bodies go through os.ExpandEnv so ${VAR} references resolve, as
// documented for the split files. config.yaml is never expanded.
func readYAMLMap(path string, expandEnv bool) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if expandEnv {
		data = []byte(os.ExpandEnv(string(data)))
	}
	var settings map[string]any
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}

func deletePath(m map[string]any, key string) {
	parts := strings.Split(key, ".")
	for _, part := range parts[:len(parts)-1] {
		next, ok := m[part].(map[string]any)
		if !ok {
			return
		}
		m = next
	}
	delete(m, parts[len(parts)-1])
}

// flattenToolsLayout lifts the per-tool sections of tools.yaml out of their
// tools: block, the layout ToolsConfig.MarshalYAML writes, onto the flat
// ToolsConfig keys. A section outside the block is ignored, as the YAML
// decoder does.
func flattenToolsLayout(m map[string]any) error {
	sections, _ := m["tools"].(map[string]any)
	for key := range m {
		if !config.IsToolsPolicyKey(key) {
			delete(m, key)
		}
	}
	maps.Copy(m, sections)
	return nil
}

// keepMCPEnvCase retypes each server's env block to map[string]string.
// ponytail: Viper lowercases the keys of every untyped map it stores, which
// would turn API_KEY into api_key. It leaves typed maps alone.
func keepMCPEnvCase(m map[string]any) error {
	servers, _ := m["servers"].([]any)
	for _, server := range servers {
		if entry, ok := server.(map[string]any); ok {
			if err := retype[map[string]string](entry, "env"); err != nil {
				return err
			}
		}
	}
	return nil
}

// keepWorkspaceEnvCase retypes every workspace's env map so Viper keeps the
// variable names' case, as keepMCPEnvCase does for MCP servers.
func keepWorkspaceEnvCase(m map[string]any) error {
	workspaces, _ := m["workspaces"].([]any)
	for _, workspace := range workspaces {
		if entry, ok := workspace.(map[string]any); ok {
			if err := retype[map[string]string](entry, "env"); err != nil {
				return err
			}
		}
	}
	return nil
}

// typeSandboxEntries decodes the allowed and denied lists with the domain's
// YAML decoders, which accept a bare path or a mapping, so mapstructure
// receives already-typed entries.
func typeSandboxEntries(m map[string]any) error {
	filesystem, ok := m["filesystem"].(map[string]any)
	if !ok {
		return nil
	}
	if err := retype[[]sandboxdomain.Allowed](filesystem, "allowed"); err != nil {
		return err
	}
	return retype[[]sandboxdomain.Denied](filesystem, "denied")
}

// retype re-decodes m[key] through YAML into T and stores the typed value.
func retype[T any](m map[string]any, key string) error {
	raw, ok := m[key]
	if !ok || raw == nil {
		return nil
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	var typed T
	if err := yaml.Unmarshal(data, &typed); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	m[key] = typed
	return nil
}

// dropBlankPrompts removes prompts a file left empty so the built-in default
// applies: an empty system prompt would leave the model with no instructions.
// agent.custom_instructions is the one prompt where empty is a choice.
func dropBlankPrompts(m map[string]any) error {
	agent, _ := m["agent"].(map[string]any)
	custom, hasCustom := agent["custom_instructions"]
	dropBlankStrings(m)
	if hasCustom {
		agent["custom_instructions"] = custom
	}
	return nil
}

func dropBlankStrings(m map[string]any) {
	for key, value := range m {
		switch typed := value.(type) {
		case string:
			if typed == "" {
				delete(m, key)
			}
		case map[string]any:
			dropBlankStrings(typed)
		}
	}
}

// decodeHook is Viper's default hook with the comma split replaced by
// ParseDelimitedList, so list values from env may use commas or newlines and
// keep entries that contain spaces, and scalars trimmed so a padded env value
// still parses.
func decodeHook() mapstructure.DecodeHookFunc {
	return mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		func(from, to reflect.Type, data any) (any, error) {
			if from.Kind() != reflect.String {
				return data, nil
			}
			raw := reflect.ValueOf(data).String()
			switch to.Kind() {
			case reflect.Slice:
				if to.Elem().Kind() == reflect.String {
					return ParseDelimitedList(raw), nil
				}
			case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
				return strings.TrimSpace(raw), nil
			}
			return data, nil
		},
	)
}

// mergeReminders picks the single reminders source: INFER_REMINDERS_CONFIG
// inline YAML, then --reminders-file, then the reminders.yaml layers.
func mergeReminders(v *viper.Viper, root *cobra.Command, sc sidecar) error {
	if inline := strings.TrimSpace(v.GetString("reminders.config")); inline != "" {
		var settings map[string]any
		if err := yaml.Unmarshal([]byte(os.ExpandEnv(inline)), &settings); err != nil {
			return fmt.Errorf("parsing INFER_REMINDERS_CONFIG: %w", err)
		}
		return mergeUnderKey(v, sc.key, settings)
	}
	if path := remindersFileOverride(root); path != "" {
		return mergeSidecar(v, sidecar{key: sc.key, fatal: true}, []string{path})
	}
	return mergeSidecar(v, sc, sc.paths())
}

// remindersFileOverride returns the --reminders-file persistent flag value when
// the user set it. Empty means the flag was not provided.
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

// postProcess applies the few settings that are additions rather than key
// overrides, after the single Unmarshal.
func postProcess(cfg *config.Config, v *viper.Viper, root *cobra.Command) {
	if prompt := v.GetString("subagent.system_prompt"); prompt != "" {
		cfg.Prompts.Agent.SystemPrompt = prompt
	}
	if override := resolveFlagEnvOverride(v, root, "tools-bash-allow-append", "tools.bash.allow_append"); override != "" {
		cfg.Tools.Bash.Mode.All.Allow = append(cfg.Tools.Bash.Mode.All.Allow, ParseDelimitedList(override)...)
	}
	if extra := ParseDelimitedList(v.GetString("tools.sandbox.directories")); len(extra) > 0 {
		for i, dir := range extra {
			if !sandbox.IsAnchored(dir) {
				extra[i] = "./" + dir
			}
		}
		cfg.Tools.Sandbox.Filesystem.Allowed = append(cfg.Tools.Sandbox.Filesystem.Allowed, sandboxdomain.Allow(extra...)...)
	}
	if cfg.Reminders.Merge {
		cfg.Reminders = *cfg.Reminders.MergeWithDefaults()
	}
	pruneMemoryRemindersIfDisabled(cfg)
}

// resolveFlagEnvOverride returns the INFER_* value for key when set, else the
// persistent flag. These two settings are documented as env-over-flag.
func resolveFlagEnvOverride(v *viper.Viper, root *cobra.Command, flagName, key string) string {
	if env := v.GetString(key); env != "" {
		return env
	}
	value, err := root.PersistentFlags().GetString(flagName)
	if err == nil {
		return value
	}
	return ""
}

// ConfigPath resolves name under the project .infer/ override when toProject
// is set, and under the userspace baseline (~/.infer/) otherwise.
func ConfigPath(toProject bool, name string) (string, error) {
	if toProject {
		return filepath.Join(config.ConfigDirName, name), nil
	}
	return UserConfigPath(name)
}

// UserConfigPath resolves name under the userspace baseline (~/.infer/).
func UserConfigPath(name string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve user home directory: %w", err)
	}
	return filepath.Join(homeDir, config.ConfigDirName, name), nil
}

// AddProjectFlag registers the --project flag that flips a write command from
// the userspace baseline (~/.infer/) to the project .infer/ override layer.
func AddProjectFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().Bool("project", false,
		"Apply to the project configuration (./.infer/) instead of the userspace baseline (~/.infer/)")
}

// pruneMemoryRemindersIfDisabled drops the built-in memory reminders (see
// config.MemoryReminders) when memory is disabled, so the enabled-by-default
// set does not tell the agent to consult memory that isn't active.
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

// ProjectFlag checks for the --project flag on the current command or any
// parent command. Userspace-first model: config writes target the home
// ~/.infer/ baseline by default; --project opts into a project override.
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
