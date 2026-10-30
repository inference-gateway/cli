package runtime

import (
	"fmt"
	"os"
	"strings"

	cobra "github.com/spf13/cobra"
	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// State owns configuration and logging initialized for one Cobra command tree.
// Cobra executes initialization before RunE, so no synchronization is needed.
type State struct {
	root      *cobra.Command
	v         *viper.Viper
	cfg       *config.Config
	loggerCfg logger.Config
}

func NewState() *State { return &State{} }

func (s *State) Config() *config.Config { return s.cfg }

func (s *State) Viper() *viper.Viper { return s.v }

func (s *State) Initialize(root *cobra.Command) error {
	v, cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	s.root = root
	s.v = v
	s.cfg = cfg
	config.UserContextWindows = cfg.ContextWindows

	logDir := v.GetString("logging.dir")
	if logDir == "" {
		logDir = config.DefaultLogsDir()
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

// LoadConfig re-runs the startup load (defaults, config.yaml layers, sidecar
// files, INFER_* env) and returns a fresh validated config. It leaves the
// running config and logging alone, so the caller decides what to apply.
func (s *State) LoadConfig() (*config.Config, error) {
	_, cfg, err := loadConfig(s.root)
	return cfg, err
}

// loadConfig builds the one Viper every setting flows through: defaults for
// every leaf, config.yaml home then project, each sidecar under its key,
// INFER_* env and the persistent flags, then a single Unmarshal.
//
// Precedence, lowest to highest: defaults, ~/.infer/<file>, ./.infer/<file>,
// INFER_* env, flags. Lists replace wholesale between layers.
func loadConfig(root *cobra.Command) (*viper.Viper, *config.Config, error) {
	v := viper.New()
	registerConfigDefaults(v, config.DefaultConfig())
	v.SetEnvPrefix("INFER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.AllowEmptyEnv(true)
	if err := v.BindPFlags(root.PersistentFlags()); err != nil {
		return nil, nil, fmt.Errorf("binding flags: %w", err)
	}
	if err := mergeConfigLayers(v); err != nil {
		return nil, nil, err
	}
	for _, sc := range sidecars {
		var err error
		if sc.key == "reminders" {
			err = mergeReminders(v, root, sc)
		} else {
			err = mergeSidecar(v, sc, sc.paths())
		}
		if err != nil {
			return nil, nil, err
		}
	}
	cfg := &config.Config{}
	if err := v.Unmarshal(cfg, viper.DecodeHook(decodeHook())); err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}
	postProcess(cfg, v, root)
	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}
	return v, cfg, nil
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
	s.loggerCfg.FilePrefix = "daemon"
	s.loggerCfg.Stdout = true
	logger.Init(s.loggerCfg)
}

// ParseDelimitedList parses a comma/newline-separated value into a string
// slice, dropping empty items - the split rule shared by the INFER_* list
// environment overrides and `config set` list values.
func ParseDelimitedList(value string) []string {
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
	path, err := ConfigPath(toProject, config.ConfigFileName)
	if err != nil {
		return nil, "", err
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
