package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cobra "github.com/spf13/cobra"
	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// State owns configuration and logging initialized for one Cobra command tree.
// Cobra executes initialization before RunE, so no synchronization is needed.
type State struct {
	v         *viper.Viper
	cfg       *config.Config
	loggerCfg logger.Config
}

func NewState() *State { return &State{} }

func (s *State) Config() *config.Config { return s.cfg }

func (s *State) Viper() *viper.Viper { return s.v }

func (s *State) Initialize(root *cobra.Command) error {
	v := viper.New()
	s.v = v

	registerConfigDefaults(v, config.DefaultConfig())
	v.SetConfigType("yaml")
	v.SetEnvPrefix("INFER")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if agents := os.Getenv("INFER_A2A_AGENTS"); agents != "" {
		v.Set("a2a.agents", parseDelimitedList(agents))
	}
	if domains := os.Getenv("INFER_TOOLS_WEB_FETCH_ALLOWED_DOMAINS"); domains != "" {
		v.Set("tools.web_fetch.allowed_domains", parseDelimitedList(domains))
	}
	if err := v.BindPFlag("verbose", root.PersistentFlags().Lookup("verbose")); err != nil {
		return fmt.Errorf("binding verbose flag: %w", err)
	}
	if err := loadLayeredConfig(v); err != nil {
		return err
	}
	applyBashAllowAppends(v, root)

	cfg, err := loadConfigFromViper(v, root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	s.cfg = cfg
	config.UserContextWindows = cfg.ContextWindows

	if prompt := os.Getenv("INFER_SUBAGENT_SYSTEM_PROMPT"); prompt != "" {
		cfg.Prompts.Agent.SystemPrompt = prompt
	}

	logDir := v.GetString("logging.dir")
	if logDir == "" {
		logDir = config.DefaultLogsPath
	}
	s.loggerCfg = logger.Config{
		Verbose:          v.GetBool("verbose"),
		Debug:            v.GetBool("logging.debug"),
		LogDir:           logDir,
		Stdout:           v.GetBool("logging.stdout"),
		ArchiveEnabled:   v.GetBool("logging.archive.enabled"),
		ArchiveMaxSizeMB: v.GetInt("logging.archive.max_size_mb"),
	}
	logger.Init(s.loggerCfg)
	return nil
}

func (s *State) DisableStdoutLogging() {
	if !s.loggerCfg.Stdout {
		return
	}
	s.loggerCfg.Stdout = false
	s.loggerCfg.ArchiveEnabled = false
	logger.Init(s.loggerCfg)
}

func (s *State) ConfigureDaemonLogging() {
	if s.v.GetString("logging.dir") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			s.loggerCfg.LogDir = filepath.Join(home, config.ConfigDirName, config.LogsDirName)
		}
	}
	s.loggerCfg.FilePrefix = "daemon"
	s.loggerCfg.Stdout = true
	logger.Init(s.loggerCfg)
}

func parseDelimitedList(value string) []string {
	var out []string
	for _, item := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n'
	}) {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func resolveFlagEnvOverride(root *cobra.Command, flagName, envName string) string {
	if env := os.Getenv(envName); env != "" {
		return env
	}
	value, err := root.PersistentFlags().GetString(flagName)
	if err == nil {
		return value
	}
	return ""
}

func applyBashAllowAppends(v *viper.Viper, root *cobra.Command) {
	if override := resolveFlagEnvOverride(root, "tools-bash-allow-append", "INFER_TOOLS_BASH_ALLOW_APPEND"); override != "" {
		const key = "tools.bash.mode.all.allow"
		v.Set(key, append(v.GetStringSlice(key), parseDelimitedList(override)...))
	}
}

func loadLayeredConfig(v *viper.Viper) error {
	homeConfigPath := ""
	if homeDir, err := os.UserHomeDir(); err == nil {
		homeConfigPath = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
	}

	readLayer := func(path string, merge bool) error {
		v.SetConfigFile(path)
		if merge {
			if err := v.MergeInConfig(); err != nil {
				return fmt.Errorf("reading config %s: %w", path, err)
			}
			return nil
		}
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("reading config %s: %w", path, err)
		}
		return nil
	}

	loaded := false
	if homeConfigPath != "" && FileExists(homeConfigPath) {
		if err := readLayer(homeConfigPath, false); err != nil {
			return err
		}
		loaded = true
	}
	projectPath := resolveProjectConfigPath()
	if projectPath != "" && !sameConfigFile(projectPath, homeConfigPath) {
		if err := readLayer(projectPath, loaded); err != nil {
			return err
		}
	}
	return nil
}

func resolveProjectConfigPath() string {
	for _, path := range []string{config.ConfigFileName, config.DefaultConfigPath} {
		if FileExists(path) {
			return path
		}
	}
	return ""
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ConfigWriteTarget returns a fresh Viper instance for a userspace baseline or
// sparse project override without writing the fully merged runtime config.
func ConfigWriteTarget(toProject bool) (*viper.Viper, string, error) {
	path := config.DefaultConfigPath
	if !toProject {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, "", fmt.Errorf("failed to resolve home directory: %w", err)
		}
		path = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
	}

	target := viper.New()
	target.SetConfigFile(path)
	if _, err := os.Stat(path); err == nil {
		if err := target.ReadInConfig(); err != nil {
			return nil, "", fmt.Errorf("failed to read %s: %w", path, err)
		}
	}
	return target, path, nil
}
